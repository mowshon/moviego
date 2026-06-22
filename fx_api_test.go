package mgo

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/video"
)

type resizeEffect struct {
	scale float64
}

func (r resizeEffect) Targets() effect.EffectTargets {
	return effect.EffectTargets{Video: true, Mask: true}
}

func (r resizeEffect) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	return effect.Resize{Scale: r.scale}.ApplyVideo(c)
}

type audioDurationEffect struct {
	d clip.Time
}

func (a audioDurationEffect) ApplyAudio(c audio.AudioClip) (audio.AudioClip, error) {
	return c.WithDuration(a.d).(audio.AudioClip), nil
}

type facadeAudioClip struct {
	dur   clip.Time
	start clip.Time
}

func (c *facadeAudioClip) Start() clip.Time    { return c.start }
func (c *facadeAudioClip) Duration() clip.Time { return clip.DurationOr(c.dur, c.dur > 0) }
func (c *facadeAudioClip) End() clip.Time      { return clip.EndOr(c.start, c.dur, c.dur > 0) }

func (c *facadeAudioClip) WithStart(t clip.Time) clip.Clip {
	cp := *c
	cp.start = t
	return &cp
}

func (c *facadeAudioClip) WithDuration(d clip.Time) clip.Clip {
	cp := *c
	cp.dur = d
	return &cp
}

func (c *facadeAudioClip) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	cp := *c
	if changeDuration {
		cp.dur = end - cp.start
	} else if cp.dur > 0 {
		cp.start = end - cp.dur
	}
	return &cp
}

func (c *facadeAudioClip) SampleRate() int { return 48000 }
func (c *facadeAudioClip) Channels() int   { return 1 }
func (c *facadeAudioClip) Close() error    { return nil }

func (c *facadeAudioClip) SamplesInto(_ context.Context, _ clip.Time, count int, dst *clip.AudioBuffer) error {
	dst.Reset(count, c.Channels())
	return nil
}

func TestVideoFxAppliesConfiguredEffect(t *testing.T) {
	v := Color(8, 8, [3]byte{}).Fx(resizeEffect{scale: 0.5})
	if err := v.Err(); err != nil {
		t.Fatalf("Fx err = %v", err)
	}
	if got := v.Size(); got != (Size{W: 4, H: 4}) {
		t.Fatalf("Fx size = %v, want 4x4", got)
	}
}

func TestAudioFxAppliesConfiguredEffect(t *testing.T) {
	a := (&Audio{inner: &facadeAudioClip{dur: 2 * time.Second}}).Fx(audioDurationEffect{d: time.Second})
	if err := a.Err(); err != nil {
		t.Fatalf("Fx err = %v", err)
	}
	if got := a.Duration(); got != time.Second {
		t.Fatalf("Fx duration = %v, want 1s", got)
	}
}
