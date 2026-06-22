package audio_test

import (
	"context"

	"github.com/mowshon/moviego/v2/clip"
)

// fakeClip is a synthetic, FFmpeg-free AudioClip whose sample at clip-local
// frame index i is gen(i) on every channel. It zero-pads indices outside
// [0, durIdx) so it behaves like a real bounded source.
type fakeClip struct {
	sr, ch int
	dur    clip.Time
	start  clip.Time
	gen    func(i int) float32
}

func newFake(sr, ch int, dur clip.Time, gen func(i int) float32) *fakeClip {
	return &fakeClip{sr: sr, ch: ch, dur: dur, gen: gen}
}

func (c *fakeClip) Start() clip.Time    { return c.start }
func (c *fakeClip) Duration() clip.Time { return clip.DurationOr(c.dur, c.dur > 0) }
func (c *fakeClip) End() clip.Time      { return clip.EndOr(c.start, c.dur, c.dur > 0) }

func (c *fakeClip) WithStart(t clip.Time) clip.Clip {
	cp := *c
	cp.start = t
	return &cp
}

func (c *fakeClip) WithDuration(d clip.Time) clip.Clip {
	cp := *c
	cp.dur = d
	return &cp
}

func (c *fakeClip) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	cp := *c
	if changeDuration {
		cp.dur = end - cp.start
	} else if cp.dur > 0 {
		cp.start = end - cp.dur
	}
	return &cp
}

func (c *fakeClip) SampleRate() int { return c.sr }
func (c *fakeClip) Channels() int   { return c.ch }
func (c *fakeClip) Close() error    { return nil }

func (c *fakeClip) SamplesInto(_ context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	dst.Reset(count, c.ch)
	base := clip.SampleIndex(start, c.sr)
	durIdx := clip.SampleIndex(c.dur, c.sr)
	for j := 0; j < count; j++ {
		i := base + j
		var v float32
		if i >= 0 && i < durIdx {
			v = c.gen(i)
		}
		for k := 0; k < c.ch; k++ {
			dst.Samples[j*c.ch+k] = v
		}
	}
	return nil
}
