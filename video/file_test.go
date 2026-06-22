package video_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/video"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

func patternFile(t *testing.T) string {
	t.Helper()
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.TestPatternVideo(t.TempDir(), "pat.mp4", 48, 32, 3, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	return path
}

func TestVideoFileNodeMetadata(t *testing.T) {
	path := patternFile(t)
	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	if n.Size() != (clip.Size{W: 48, H: 32}) {
		t.Errorf("size = %v, want 48x32", n.Size())
	}
	if r, _ := n.Rate(); r != (clip.Rate{Num: 25, Den: 1}) {
		t.Errorf("rate = %v, want 25/1", r)
	}
	if d := n.Duration(); !clip.Finite(d) || d < 2900*time.Millisecond || d > 3100*time.Millisecond {
		t.Errorf("duration = %v, want ~3s", d)
	}
}

// TestVideoFileNodeGoldenFrame asserts the node's first frame matches a frame
// read straight from the decoder, i.e. the node adds no pixel distortion.
func TestVideoFileNodeGoldenFrame(t *testing.T) {
	path := patternFile(t)

	dec, err := videoio.OpenDecoder(context.Background(), path, videoio.DecoderOptions{
		Size: clip.Size{W: 48, H: 32},
		Rate: clip.Rate{Num: 25, Den: 1},
	})
	if err != nil {
		t.Fatalf("decoder: %v", err)
	}
	golden := clip.NewFrame(48, 32, clip.RGB24)
	if err := dec.ReadInto(golden); err != nil {
		t.Fatalf("decode frame 0: %v", err)
	}
	dec.Close()

	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()
	got := clip.NewFrame(48, 32, clip.RGB24)
	if err := n.FrameInto(context.Background(), 0, got); err != nil {
		t.Fatalf("frame into: %v", err)
	}
	if !bytes.Equal(got.Pix, golden.Pix) {
		t.Error("node frame 0 differs from decoder frame 0")
	}

	// A second read of the same index is served from the node cache and must be
	// byte-identical (it must not corrupt the caller's buffer either).
	again := clip.NewFrame(48, 32, clip.RGB24)
	if err := n.FrameInto(context.Background(), 0, again); err != nil {
		t.Fatalf("cached frame into: %v", err)
	}
	if !bytes.Equal(again.Pix, golden.Pix) {
		t.Error("cached frame 0 differs from golden")
	}
}

func TestVideoFileNodeRotatedGolden(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	// Stored 160x120 with a 90-degree display rotation -> displays as 120x160.
	path, err := genmedia.RotatedVideo(t.TempDir(), "rot.mp4", 160, 120, 1, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	if n.Size() != (clip.Size{W: 120, H: 160}) {
		t.Fatalf("size = %v, want post-rotation 120x160", n.Size())
	}
	// The decoder must emit display-oriented frames matching the post-rotation
	// size; a mismatch would error in ReadInto's validation.
	dst := clip.NewFrame(120, 160, clip.RGB24)
	if err := n.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame into rotated: %v", err)
	}
}

// TestWithStartVsSubclipPixels distinguishes placement from media trim:
// WithStart shifts only the composition start, so the same output time still
// reads the same source frame; Subclip shifts the media, so output t=0 reads the
// source frame at the subclip's start.
func TestWithStartVsSubclipPixels(t *testing.T) {
	path := patternFile(t) // 48x32, 25fps, 3s

	frameAt := func(c video.VideoClip, t clip.Time) []byte {
		dst := clip.NewFrame(48, 32, clip.RGB24)
		if err := c.FrameInto(context.Background(), t, dst); err != nil {
			panic(err)
		}
		return append([]byte(nil), dst.Pix...)
	}
	open := func() video.VideoClip {
		n, err := video.OpenFile(context.Background(), path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { n.Close() })
		return n
	}

	base0 := frameAt(open(), 0)
	base2s := frameAt(open(), 2*time.Second)

	shifted := open().WithStart(2 * time.Second).(video.VideoClip)
	if !bytes.Equal(frameAt(shifted, 0), base0) {
		t.Error("WithStart shifted the media; output t=0 should still read source frame 0")
	}

	sub := video.Subclip(open(), 2*time.Second, 3*time.Second)
	if !bytes.Equal(frameAt(sub, 0), base2s) {
		t.Error("Subclip output t=0 should read the source frame at 2s")
	}
}

func TestSubclipCloseLeavesParentUsable(t *testing.T) {
	path := patternFile(t)
	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	sub := video.Subclip(n, time.Second, 2*time.Second)
	if err := sub.Close(); err != nil {
		t.Fatalf("close sub: %v", err)
	}
	// The parent must still render after the trimmed wrapper is closed.
	dst := clip.NewFrame(48, 32, clip.RGB24)
	if err := n.FrameInto(context.Background(), 0, dst); err != nil {
		t.Errorf("parent unusable after closing subclip: %v", err)
	}
}

func TestVideoFileNodeContextRebind(t *testing.T) {
	path := patternFile(t)
	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	dst := clip.NewFrame(48, 32, clip.RGB24)
	if err := n.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	// A later access under a canceled context must rebind the decoder to that
	// context (and so fail), not reuse the decoder opened under Background.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A different output time (frame 25 at 25fps) misses the frame cache, so the
	// node must rebind the decoder to the canceled context and fail.
	if err := n.FrameInto(ctx, time.Second, dst); err == nil {
		t.Error("expected error after rebinding to a canceled context")
	}
}

func TestVideoFileNodeWithStartIndependent(t *testing.T) {
	path := patternFile(t)
	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	shifted := n.WithStart(2 * time.Second)
	if shifted.Start() != 2*time.Second {
		t.Errorf("start = %v, want 2s", shifted.Start())
	}
	// The original placement is unchanged (With* returns a new clip).
	if n.Start() != 0 {
		t.Errorf("original start mutated to %v", n.Start())
	}
}
