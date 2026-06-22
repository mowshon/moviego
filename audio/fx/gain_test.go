package audiofx_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	audiofx "github.com/mowshon/moviego/v2/audio/fx"
	"github.com/mowshon/moviego/v2/clip"
)

const sr = 48000

// constClip is a mono source of a constant value over [0, dur).
type constClip struct {
	val float32
	dur clip.Time
}

func (c *constClip) Start() clip.Time    { return 0 }
func (c *constClip) Duration() clip.Time { return c.dur }
func (c *constClip) End() clip.Time      { return c.dur }
func (c *constClip) WithStart(clip.Time) clip.Clip {
	return c
}

func (c *constClip) WithDuration(d clip.Time) clip.Clip {
	cp := *c
	cp.dur = d
	return &cp
}
func (c *constClip) WithEnd(clip.Time, bool) clip.Clip { return c }
func (c *constClip) SampleRate() int                   { return sr }
func (c *constClip) Channels() int                     { return 1 }
func (c *constClip) Close() error                      { return nil }

func (c *constClip) SamplesInto(_ context.Context, _ clip.Time, count int, dst *clip.AudioBuffer) error {
	dst.Reset(count, 1)
	for i := 0; i < count; i++ {
		dst.Samples[i] = c.val
	}
	return nil
}

func pull(t *testing.T, c audio.AudioClip, start clip.Time, count int) *clip.AudioBuffer {
	t.Helper()
	buf := &clip.AudioBuffer{}
	if err := c.SamplesInto(context.Background(), start, count, buf); err != nil {
		t.Fatalf("SamplesInto: %v", err)
	}
	return buf
}

func TestMultiplyVolume(t *testing.T) {
	v := audiofx.MultiplyVolume(&constClip{val: 0.5, dur: time.Second}, 0.5)
	buf := pull(t, v, 0, 16)
	for i, s := range buf.Samples {
		if s != 0.25 {
			t.Fatalf("sample %d = %f, want 0.25", i, s)
		}
	}
}

// TestFadeIn checks the linear ramp at 0, the midpoint, and full scale.
func TestFadeIn(t *testing.T) {
	d := time.Second
	f := audiofx.FadeIn(&constClip{val: 1, dur: 2 * time.Second}, d)
	buf := pull(t, f, 0, 2*sr)
	if buf.Samples[0] != 0 {
		t.Fatalf("fade-in start = %f, want 0", buf.Samples[0])
	}
	mid := clip.SampleIndex(d/2, sr)
	if g := buf.Samples[mid]; g < 0.49 || g > 0.51 {
		t.Fatalf("fade-in midpoint = %f, want ~0.5", g)
	}
	full := clip.SampleIndex(d, sr)
	if buf.Samples[full] != 1 {
		t.Fatalf("fade-in at d = %f, want 1", buf.Samples[full])
	}
}

func TestFadeOut(t *testing.T) {
	dur := 2 * time.Second
	d := time.Second
	f := audiofx.FadeOut(&constClip{val: 1, dur: dur}, d)
	buf := pull(t, f, 0, 2*sr)
	// Before the fade region, full volume.
	before := clip.SampleIndex(dur-d-100*time.Millisecond, sr)
	if buf.Samples[before] != 1 {
		t.Fatalf("pre-fade sample = %f, want 1", buf.Samples[before])
	}
	// Midpoint of the fade region ~0.5.
	mid := clip.SampleIndex(dur-d/2, sr)
	if g := buf.Samples[mid]; g < 0.49 || g > 0.51 {
		t.Fatalf("fade-out midpoint = %f, want ~0.5", g)
	}
}
