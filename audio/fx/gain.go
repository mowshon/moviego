package audiofx

import (
	"context"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// gainNode wraps an AudioClip and scales every sample by gain(t), where t is
// the clip-local time of each sample frame. A constant gain is volume scaling;
// a time-varying gain is a fade. It owns no mutable state, so it is safe to use
// from the single audio-render goroutine and forwards all timeline metadata to
// the inner clip.
type gainNode struct {
	inner audio.AudioClip
	gain  func(t clip.Time) float32
}

// Timeline metadata forwards to the inner clip, re-wrapping the With* results so
// the gain survives a placement/duration change.

func (n *gainNode) Start() clip.Time    { return n.inner.Start() }
func (n *gainNode) Duration() clip.Time { return n.inner.Duration() }
func (n *gainNode) End() clip.Time      { return n.inner.End() }

func (n *gainNode) WithStart(t clip.Time) clip.Clip {
	return &gainNode{inner: mustAudioClip(n.inner.WithStart(t)), gain: n.gain}
}

func (n *gainNode) WithDuration(d clip.Time) clip.Clip {
	return &gainNode{inner: mustAudioClip(n.inner.WithDuration(d)), gain: n.gain}
}

func (n *gainNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	return &gainNode{inner: mustAudioClip(n.inner.WithEnd(end, changeDuration)), gain: n.gain}
}

// mustAudioClip asserts that c implements AudioClip. It panics with a clear
// message if the inner clip's With* mutator returns a plain clip.Clip — a
// broken implementation that the type system cannot prevent at compile time.
func mustAudioClip(c clip.Clip) audio.AudioClip {
	ac, ok := c.(audio.AudioClip)
	if !ok {
		panic("audio/fx: inner clip With* returned a non-AudioClip; the inner type must implement audio.AudioClip")
	}
	return ac
}

func (n *gainNode) SampleRate() int { return n.inner.SampleRate() }
func (n *gainNode) Channels() int   { return n.inner.Channels() }

// Close releases nothing: an effect wraps a clip it does not own (single-owner
// model, matching every other audio wrapper and the video nodes). The handle
// that opened the underlying source closes it; closing the inner here would tear
// down a source another branch of the graph might still need.
func (n *gainNode) Close() error { return nil }

// SamplesInto pulls the inner samples then applies the per-sample gain.
func (n *gainNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	if err := n.inner.SamplesInto(ctx, start, count, dst); err != nil {
		return err
	}
	rate := n.inner.SampleRate()
	ch := dst.Channels
	for j := 0; j < dst.Count; j++ {
		g := n.gain(start + clip.SampleTime(j, rate))
		if g == 1 {
			continue
		}
		base := j * ch
		for c := 0; c < ch; c++ {
			dst.Samples[base+c] *= g
		}
	}
	return nil
}
