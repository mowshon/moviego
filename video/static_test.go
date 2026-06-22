package video_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

func TestColorNodeFill(t *testing.T) {
	n := video.NewColor(clip.Size{W: 3, H: 2}, [3]byte{10, 20, 30})
	if n.Size() != (clip.Size{W: 3, H: 2}) {
		t.Fatalf("size = %v", n.Size())
	}
	if !n.ParallelSafe() || n.SourceAccess() != video.AccessStatic {
		t.Errorf("color node should be parallel-safe and static")
	}
	dst := clip.NewFrame(3, 2, clip.RGB24)
	if _, err := n.RenderInto(context.Background(), 0, dst, nil); err != nil {
		t.Fatalf("render: %v", err)
	}
	for i := 0; i < len(dst.Pix); i += 3 {
		if dst.Pix[i] != 10 || dst.Pix[i+1] != 20 || dst.Pix[i+2] != 30 {
			t.Fatalf("pixel %d = %v, want 10,20,30", i/3, dst.Pix[i:i+3])
		}
	}
}

// TestStaticRenderDoesNotAliasCache mutates a rendered destination and asserts a
// second render is unaffected: nodes copy their cache, never hand it out.
func TestStaticRenderDoesNotAliasCache(t *testing.T) {
	rgb := clip.NewFrame(2, 2, clip.RGB24)
	for i := range rgb.Pix {
		rgb.Pix[i] = byte(i + 1)
	}
	n := video.NewImage(rgb, nil)

	dst := clip.NewFrame(2, 2, clip.RGB24)
	if err := n.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame 1: %v", err)
	}
	golden := append([]byte(nil), dst.Pix...)

	// Corrupt the returned buffer.
	for i := range dst.Pix {
		dst.Pix[i] = 0xff
	}
	again := clip.NewFrame(2, 2, clip.RGB24)
	if err := n.FrameInto(context.Background(), time.Second, again); err != nil {
		t.Fatalf("frame 2: %v", err)
	}
	if !bytes.Equal(again.Pix, golden) {
		t.Error("mutating a rendered frame corrupted the node cache")
	}
}

func TestImageNodeMaskFromAlpha(t *testing.T) {
	rgb := clip.NewFrame(2, 1, clip.RGB24)
	alpha := clip.NewFrame(2, 1, clip.Gray8)
	alpha.Pix[0], alpha.Pix[1] = 0, 255
	n := video.NewImage(rgb, alpha)

	if !n.HasMask() {
		t.Fatal("image with alpha should report a mask")
	}
	maskDst := clip.NewFrame(2, 1, clip.Gray8)
	ok, err := n.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if maskDst.Pix[0] != 0 || maskDst.Pix[1] != 255 {
		t.Errorf("mask = %v, want 0,255", maskDst.Pix)
	}

	// RenderInto fills both RGB and alpha in one pass and reports hasAlpha.
	rgbDst := clip.NewFrame(2, 1, clip.RGB24)
	alphaDst := clip.NewFrame(2, 1, clip.Gray8)
	hasAlpha, err := n.RenderInto(context.Background(), 0, rgbDst, alphaDst)
	if err != nil || !hasAlpha {
		t.Fatalf("render: hasAlpha=%v err=%v", hasAlpha, err)
	}
	if !bytes.Equal(alphaDst.Pix, alpha.Pix) {
		t.Error("render alpha differs from source mask")
	}
}

func TestImageNodeMapStaticIsEager(t *testing.T) {
	rgb := clip.NewFrame(4, 4, clip.RGB24)
	n := video.NewImage(rgb, nil)
	out, err := n.MapStatic(clip.Size{W: 2, H: 2},
		func(dst, src *clip.Frame) error { return nil },
		nil)
	if err != nil {
		t.Fatalf("map static: %v", err)
	}
	if out.Size() != (clip.Size{W: 2, H: 2}) {
		t.Errorf("size = %v, want 2x2", out.Size())
	}
	// The eager result is itself a static source.
	if out.SourceAccess() != video.AccessStatic {
		t.Error("mapped image should remain AccessStatic")
	}
}

// TestOpenImageDecodesOnce proves the disk path reads the file exactly once at
// construction: after deleting the file, renders still succeed from the cached
// buffer (MoviePy-style re-reading would fail here).
func TestOpenImageDecodesOnce(t *testing.T) {
	src := clip.NewFrame(3, 2, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = byte(i + 1)
	}
	path := filepath.Join(t.TempDir(), "still.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := imagex.EncodePNG(f, src); err != nil {
		t.Fatalf("encode: %v", err)
	}
	f.Close()

	n, err := video.OpenImage(path)
	if err != nil {
		t.Fatalf("open image: %v", err)
	}
	// Remove the source; any later read would now fail.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	dst := clip.NewFrame(3, 2, clip.RGB24)
	for i := 0; i < 3; i++ {
		if err := n.FrameInto(context.Background(), clip.Time(i)*time.Second, dst); err != nil {
			t.Fatalf("render %d after file removal: %v", i, err)
		}
	}
	if !bytes.Equal(dst.Pix, src.Pix) {
		t.Error("rendered pixels differ from the decoded source")
	}
}

// TestStaticConcurrentRenderInto exercises the parallel-safe claim of a static
// source under concurrent render. Run under -race.
func TestStaticConcurrentRenderInto(t *testing.T) {
	src := clip.NewFrame(8, 8, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = 42
	}
	n := video.NewImage(src, nil)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dst := clip.NewFrame(8, 8, clip.RGB24)
			for i := 0; i < 50; i++ {
				if _, err := n.RenderInto(context.Background(), clip.Time(i), dst, nil); err != nil {
					t.Errorf("render: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestStaticTimelineWith(t *testing.T) {
	n := video.NewColor(clip.Size{W: 2, H: 2}, [3]byte{})
	d := n.WithDuration(2 * time.Second).(video.VideoClip)
	if dur := d.Duration(); !clip.Finite(dur) || dur != 2*time.Second {
		t.Errorf("duration = %v", dur)
	}
	// Original is unchanged (With* returns a new clip).
	if clip.Finite(n.Duration()) {
		t.Error("original color node duration mutated")
	}
	r := n.WithRate(clip.Rate{Num: 30, Den: 1})
	if rate, ok := r.Rate(); !ok || rate != (clip.Rate{Num: 30, Den: 1}) {
		t.Errorf("rate = %v,%v", rate, ok)
	}
}
