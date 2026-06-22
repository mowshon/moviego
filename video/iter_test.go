package video_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// fakeClip is a deterministic VideoClip for testing the export schedule without
// FFmpeg. It returns ErrEOF after eofAt frames when eofAt > 0.
type fakeClip struct {
	rate  clip.Rate
	dur   clip.Time
	eofAt int
	calls int
	lastT clip.Time
}

func (c *fakeClip) Start() clip.Time    { return 0 }
func (c *fakeClip) Duration() clip.Time { return c.dur }
func (c *fakeClip) End() clip.Time      { return c.dur }
func (c *fakeClip) WithStart(clip.Time) clip.Clip {
	return c
}
func (c *fakeClip) WithDuration(clip.Time) clip.Clip  { return c }
func (c *fakeClip) WithEnd(clip.Time, bool) clip.Clip { return c }
func (c *fakeClip) Close() error                      { return nil }
func (c *fakeClip) Size() clip.Size                   { return clip.Size{W: 2, H: 2} }
func (c *fakeClip) Rate() (clip.Rate, bool)           { return c.rate, true }
func (c *fakeClip) HasMask() bool                     { return false }
func (c *fakeClip) Audio() audio.AudioClip            { return nil }
func (c *fakeClip) ParallelSafe() bool                { return true }
func (c *fakeClip) SourceAccess() video.AccessClass   { return video.AccessLinear }
func (c *fakeClip) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := c.RenderInto(ctx, t, dst, nil)
	return err
}

func (c *fakeClip) MaskInto(context.Context, clip.Time, *clip.Frame) (bool, error) {
	return false, nil
}

func (c *fakeClip) RenderInto(_ context.Context, t clip.Time, _, _ *clip.Frame) (bool, error) {
	c.lastT = t
	if c.eofAt > 0 && c.calls >= c.eofAt {
		return false, clip.ErrEOF
	}
	c.calls++
	return false, nil
}

func TestIterFramesFloorCount(t *testing.T) {
	cases := []struct {
		name string
		rate clip.Rate
		dur  clip.Time
		want int
	}{
		{"30fps 2s", clip.Rate{Num: 30, Den: 1}, 2 * time.Second, 60},
		{"30fps just under 2s", clip.Rate{Num: 30, Den: 1}, 2*time.Second - time.Microsecond, 60},
		{"29.97 1001ms", clip.Rate{Num: 30000, Den: 1001}, 1001 * time.Millisecond, 30},
		{"25fps 1s", clip.Rate{Num: 25, Den: 1}, time.Second, 25},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fc := &fakeClip{rate: c.rate, dur: c.dur}
			n, err := video.IterFrames(context.Background(), fc, func(clip.RenderedFrame) error { return nil })
			if err != nil {
				t.Fatalf("iter: %v", err)
			}
			if n != c.want {
				t.Errorf("count = %d, want %d", n, c.want)
			}
		})
	}
}

func TestIterFramesStopsAtEOF(t *testing.T) {
	fc := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 10 * time.Second, eofAt: 7}
	n, err := video.IterFrames(context.Background(), fc, func(clip.RenderedFrame) error { return nil })
	if err != nil {
		t.Fatalf("iter: %v", err)
	}
	if n != 7 {
		t.Errorf("count = %d, want 7 (EOF)", n)
	}
}

func TestIterFramesCancel(t *testing.T) {
	fc := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 10 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := video.IterFrames(ctx, fc, func(clip.RenderedFrame) error { return nil })
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
