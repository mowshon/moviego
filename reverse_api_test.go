package mgo_test

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/render"
)

func TestReverseRunsSequential(t *testing.T) {
	// Reversing a static clip still flips it to random-access, so the planner
	// must route it to the sequential engine.
	v := sampleClip(time.Second).Reverse()
	if v.Err() != nil {
		t.Fatalf("unexpected error: %v", v.Err())
	}
	if d := v.Duration(); d != time.Second {
		t.Errorf("duration = %v, want 1s", d)
	}
	rep := describe(t, v)
	if rep.Class != render.ClassRandom {
		t.Errorf("class = %v, want Random", rep.Class)
	}
	if rep.Engine != mgo.EngineSequential {
		t.Errorf("engine = %v, want Sequential", rep.Engine)
	}
}

func TestReverseInfiniteDurationRejected(t *testing.T) {
	v := mgo.Color(16, 16, [3]byte{}).Reverse() // no WithDuration
	if !errors.Is(v.Err(), clip.ErrNoDuration) {
		t.Errorf("err = %v, want ErrNoDuration", v.Err())
	}
}

func TestBoomerangDoublesDuration(t *testing.T) {
	v := sampleClip(2 * time.Second).Boomerang()
	if v.Err() != nil {
		t.Fatalf("unexpected error: %v", v.Err())
	}
	if d := v.Duration(); d != 4*time.Second {
		t.Errorf("duration = %v, want 4s", d)
	}
}

// TestReverseFrameOrder renders a clip and its reverse to PNGs and checks that
// reversed frame i is byte-identical to forward frame N-1-i — the exact
// frame-index reflection, decoded losslessly.
func TestReverseFrameOrder(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	path, err := genmedia.TestPatternVideo(dir, "src.mp4", 32, 24, 1, "10")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}

	src, err := mgo.OpenVideo(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	fwdDir := filepath.Join(dir, "fwd")
	revDir := filepath.Join(dir, "rev")
	n, err := mgo.WriteFrameSequence(context.Background(), src, fwdDir, mgo.FrameSequenceOptions{})
	if err != nil {
		t.Fatalf("forward sequence: %v", err)
	}
	m, err := mgo.WriteFrameSequence(context.Background(), src.Reverse(), revDir, mgo.FrameSequenceOptions{})
	if err != nil {
		t.Fatalf("reverse sequence: %v", err)
	}
	if n != m {
		t.Fatalf("frame counts differ: forward %d, reverse %d", n, m)
	}
	if n < 2 {
		t.Fatalf("too few frames (%d) to test ordering", n)
	}

	for i := 0; i < n; i++ {
		rev := loadFramePixels(t, revDir, i)
		fwd := loadFramePixels(t, fwdDir, n-1-i)
		if !bytesEqual(rev, fwd) {
			t.Fatalf("reversed frame %d != forward frame %d", i, n-1-i)
		}
	}
}

// TestReverseAndBoomerangExport drives a reversed clip and a boomerang through
// WriteVideo end to end, exercising the sequential engine with the
// backward-buffered provider installed, then checks the muxed durations.
func TestReverseAndBoomerangExport(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	path, err := genmedia.TestPatternVideo(dir, "src.mp4", 32, 24, 1, "10")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	src, err := mgo.OpenVideo(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	cases := []struct {
		name string
		v    *mgo.Video
		want time.Duration
	}{
		{"reverse", src.Reverse(), time.Second},
		{"boomerang", src.Boomerang(), 2 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.v.Err() != nil {
				t.Fatalf("build: %v", c.v.Err())
			}
			out := filepath.Join(dir, c.name+".mp4")
			if err := mgo.WriteVideo(context.Background(), c.v, out, mgo.ExportOptions{}); err != nil {
				t.Fatalf("write: %v", err)
			}
			info, err := mgo.Probe(out)
			if err != nil {
				t.Fatalf("probe: %v", err)
			}
			if d := info.Duration; d < c.want-150*time.Millisecond || d > c.want+150*time.Millisecond {
				t.Errorf("duration = %v, want ~%v", d, c.want)
			}
		})
	}
}

// TestBoomerangSeamAlignment checks the boundary between the forward and reverse
// halves: the last forward frame and the first reverse frame must both show
// source frame N-1 and therefore be pixel-identical.
func TestBoomerangSeamAlignment(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	path, err := genmedia.TestPatternVideo(dir, "src.mp4", 32, 24, 1, "10")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	src, err := mgo.OpenVideo(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	boom := src.Boomerang()
	if boom.Err() != nil {
		t.Fatalf("boomerang: %v", boom.Err())
	}

	boomDir := filepath.Join(dir, "boom")
	n, err := mgo.WriteFrameSequence(context.Background(), boom, boomDir, mgo.FrameSequenceOptions{})
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}

	srcFrames := n / 2
	if srcFrames < 2 {
		t.Fatalf("too few frames (%d) to test seam", n)
	}

	// Both sides of the seam must show the same pixels (source frame N-1).
	lastFwd := loadFramePixels(t, boomDir, srcFrames-1)
	firstRev := loadFramePixels(t, boomDir, srcFrames)
	if !bytesEqual(lastFwd, firstRev) {
		t.Fatalf("seam frames %d and %d differ: boomerang forward/reverse boundary not aligned",
			srcFrames-1, srcFrames)
	}
}

func loadFramePixels(t *testing.T, dir string, idx int) []uint8 {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "frame_"+pad5(idx)+".png"))
	if err != nil {
		t.Fatalf("open frame %d: %v", idx, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode frame %d: %v", idx, err)
	}
	b := img.Bounds()
	out := make([]uint8, 0, b.Dx()*b.Dy()*3)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			out = append(out, uint8(r>>8), uint8(g>>8), uint8(bl>>8))
		}
	}
	return out
}

func bytesEqual(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pad5(n int) string {
	s := []byte("00000")
	for i := len(s) - 1; i >= 0 && n > 0; i-- {
		s[i] = byte('0' + n%10)
		n /= 10
	}
	return string(s)
}
