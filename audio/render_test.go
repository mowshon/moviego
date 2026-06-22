package audio_test

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// cancelOnPull wraps a clip and cancels the render context the first time it is
// pulled, so cancellation lands deterministically mid-render (not via timing).
type cancelOnPull struct {
	*fakeClip
	cancel context.CancelFunc
	pulls  int
}

func (c *cancelOnPull) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	c.pulls++
	if c.pulls == 1 {
		c.cancel()
	}
	return c.fakeClip.SamplesInto(ctx, start, count, dst)
}

// TestRenderToTempCancel is the 4d/5b cancel gate: a context cancelled during
// the audio render must abort with a context error (joined with any encoder
// close error) rather than finishing a full file. Without the errors.Join in
// RenderToTemp a broken-pipe close could mask the cancellation.
func TestRenderToTempCancel(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	ctx, cancel := context.WithCancel(context.Background())
	root := &cancelOnPull{
		fakeClip: newFake(48000, 2, 60*time.Second, func(int) float32 { return 0 }),
		cancel:   cancel,
	}
	out := filepath.Join(t.TempDir(), "cancel.m4a")

	err := audio.RenderToTemp(ctx, root, out, "aac", "", 60*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RenderToTemp on cancel = %v, want context.Canceled", err)
	}
	if root.pulls == 0 {
		t.Fatal("expected at least one pull before cancellation")
	}
}

// TestRenderToTempBitrate proves the bitrate argument reaches the encoder: the
// same non-silent signal encoded at 32k vs 256k AAC must yield a materially
// smaller file at the lower rate. (AudioStream has no bitrate field to probe, so
// file size is the observable.)
func TestRenderToTempBitrate(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	// A loud tone so the encoder actually spends its bit budget.
	root := newFake(44100, 2, 5*time.Second, func(i int) float32 {
		return float32(math.Sin(float64(i)*0.15)) * 0.7
	})
	dir := t.TempDir()
	lo := filepath.Join(dir, "lo.m4a")
	hi := filepath.Join(dir, "hi.m4a")
	ctx := context.Background()
	if err := audio.RenderToTemp(ctx, root, lo, "aac", "32k", 5*time.Second); err != nil {
		t.Fatalf("render 32k: %v", err)
	}
	if err := audio.RenderToTemp(ctx, root, hi, "aac", "256k", 5*time.Second); err != nil {
		t.Fatalf("render 256k: %v", err)
	}
	loSize, hiSize := fileSize(t, lo), fileSize(t, hi)
	if hiSize <= loSize {
		t.Fatalf("256k file (%d B) not larger than 32k file (%d B); bitrate ignored", hiSize, loSize)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Size()
}
