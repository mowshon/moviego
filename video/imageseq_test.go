package video_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// seqFrame builds an RGB24 frame whose every pixel carries marker, so the
// active image is identifiable from a single byte.
func seqFrame(w, h int, marker byte) *clip.Frame {
	f := clip.NewFrame(w, h, clip.RGB24)
	for i := range f.Pix {
		f.Pix[i] = marker
	}
	return f
}

// markerAt renders n at local time t and returns the first pixel byte.
func markerAt(t *testing.T, n video.VideoClip, at clip.Time) byte {
	t.Helper()
	dst := clip.NewFrame(n.Size().W, n.Size().H, clip.RGB24)
	if err := n.FrameInto(context.Background(), at, dst); err != nil {
		t.Fatalf("frame at %v: %v", at, err)
	}
	return dst.Pix[0]
}

func TestImageSequenceFPSBoundary(t *testing.T) {
	frames := []*clip.Frame{seqFrame(2, 2, 10), seqFrame(2, 2, 20), seqFrame(2, 2, 30)}
	n, err := video.NewImageSequenceFrames(frames, video.ImageSequenceOptions{FPS: clip.Rate{Num: 2, Den: 1}})
	if err != nil {
		t.Fatalf("seq: %v", err)
	}
	// Each image lasts 0.5s; boundaries belong to the next image.
	if d := n.Duration(); d != 1500*time.Millisecond {
		t.Errorf("duration = %v, want 1.5s", d)
	}
	cases := []struct {
		at   clip.Time
		want byte
	}{
		{0, 10},
		{490 * time.Millisecond, 10},
		{500 * time.Millisecond, 20}, // boundary -> next image
		{990 * time.Millisecond, 20},
		{time.Second, 30},
		{2 * time.Second, 30}, // beyond total clamps to last
	}
	for _, c := range cases {
		if got := markerAt(t, n, c.at); got != c.want {
			t.Errorf("marker at %v = %d, want %d", c.at, got, c.want)
		}
	}
}

func TestImageSequenceDurations(t *testing.T) {
	frames := []*clip.Frame{seqFrame(2, 2, 1), seqFrame(2, 2, 2)}
	n, err := video.NewImageSequenceFrames(frames, video.ImageSequenceOptions{
		Durations: []clip.Time{time.Second, 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("seq: %v", err)
	}
	if d := n.Duration(); d != 3*time.Second {
		t.Errorf("duration = %v, want 3s", d)
	}
	if got := markerAt(t, n, 500*time.Millisecond); got != 1 {
		t.Errorf("at 0.5s = %d, want 1", got)
	}
	if got := markerAt(t, n, time.Second); got != 2 { // boundary -> second image
		t.Errorf("at 1s = %d, want 2", got)
	}
	if got := markerAt(t, n, 2900*time.Millisecond); got != 2 {
		t.Errorf("at 2.9s = %d, want 2", got)
	}
}

func TestImageSequenceAlphaSplit(t *testing.T) {
	rgba := clip.NewFrame(2, 2, clip.RGBA)
	for i := 0; i < len(rgba.Pix); i += 4 {
		rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2], rgba.Pix[i+3] = 5, 6, 7, 128
	}
	n, err := video.NewImageSequenceFrames([]*clip.Frame{rgba}, video.ImageSequenceOptions{FPS: clip.Rate{Num: 1, Den: 1}})
	if err != nil {
		t.Fatalf("seq: %v", err)
	}
	if !n.HasMask() {
		t.Fatal("transparent sequence should report a mask")
	}
	rgbDst := clip.NewFrame(2, 2, clip.RGB24)
	alphaDst := clip.NewFrame(2, 2, clip.Gray8)
	hasAlpha, err := n.RenderInto(context.Background(), 0, rgbDst, alphaDst)
	if err != nil || !hasAlpha {
		t.Fatalf("render: hasAlpha=%v err=%v", hasAlpha, err)
	}
	if rgbDst.Pix[0] != 5 || alphaDst.Pix[0] != 128 {
		t.Errorf("split = rgb %d alpha %d, want 5/128", rgbDst.Pix[0], alphaDst.Pix[0])
	}
}

func TestImageSequenceEmptyErrors(t *testing.T) {
	if _, err := video.NewImageSequenceFrames(nil, video.ImageSequenceOptions{}); err == nil {
		t.Error("expected error for empty sequence")
	}
}

// TestImageSequenceDurationsLengthMismatch ensures a Durations slice that does
// not cover every image is rejected, rather than collapsing trailing images
// onto one timestamp.
func TestImageSequenceDurationsLengthMismatch(t *testing.T) {
	frames := []*clip.Frame{seqFrame(2, 2, 1), seqFrame(2, 2, 2), seqFrame(2, 2, 3)}
	_, err := video.NewImageSequenceFrames(frames, video.ImageSequenceOptions{
		Durations: []clip.Time{time.Second, time.Second}, // only 2 for 3 images
	})
	if !errors.Is(err, video.ErrDurationCount) {
		t.Errorf("err = %v, want ErrDurationCount", err)
	}
}

// TestImageSequenceHeterogeneousFrames ensures a frame whose format differs from
// the first is rejected at construction (memory mode), preventing a silent
// RGB24←RGBA copy at render time.
func TestImageSequenceHeterogeneousFrames(t *testing.T) {
	opaque := seqFrame(2, 2, 1) // RGB24
	transparent := clip.NewFrame(2, 2, clip.RGBA)
	_, err := video.NewImageSequenceFrames([]*clip.Frame{opaque, transparent}, video.ImageSequenceOptions{
		FPS: clip.Rate{Num: 1, Den: 1},
	})
	if !errors.Is(err, video.ErrInconsistentSequence) {
		t.Errorf("err = %v, want ErrInconsistentSequence for mixed formats", err)
	}
}
