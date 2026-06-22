package mgo

import (
	"context"
	"fmt"

	"github.com/mowshon/moviego/v2/audio"
	audiofx "github.com/mowshon/moviego/v2/audio/fx"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/video"
)

// Audio is the facade handle for an audio clip. Its methods build new graph
// nodes and return new handles; the underlying clip is never mutated. A build
// error is carried on the handle and surfaced by Err.
type Audio struct {
	inner audio.AudioClip
	err   error
}

// OpenAudio opens an audio file, probing it for metadata. The decoder is created
// lazily at render time. Close the returned handle when done.
func OpenAudio(path string) (*Audio, error) {
	n, err := audio.OpenFile(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return &Audio{inner: n}, nil
}

// Mix sums audio clips over a shared timeline (overlapping clips are added). The
// output rate and channel count are the max across inputs.
func Mix(clips ...*Audio) *Audio {
	inners := make([]audio.AudioClip, 0, len(clips))
	for _, c := range clips {
		if c == nil {
			continue
		}
		if c.err != nil {
			return &Audio{err: c.err}
		}
		if c.inner != nil {
			inners = append(inners, c.inner)
		}
	}
	return &Audio{inner: audio.Mix(inners...)}
}

// Volume scales the clip's amplitude by factor (0.5 = half, 2 = double).
func (a *Audio) Volume(factor float64) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	return &Audio{inner: audiofx.MultiplyVolume(a.inner, factor)}
}

// FadeIn ramps the volume from 0 to 1 over the first d.
func (a *Audio) FadeIn(d Time) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	return &Audio{inner: audiofx.FadeIn(a.inner, d)}
}

// FadeOut ramps the volume from 1 to 0 over the last d. It needs a known
// duration; without one the clip is returned unchanged.
func (a *Audio) FadeOut(d Time) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	return &Audio{inner: audiofx.FadeOut(a.inner, d)}
}

// Subclip trims the clip to the window [start, end). The end is optional and
// follows the same rules as video.Subclip: with only a start, the window runs to
// the end of the audio; a negative end counts back from the audio duration. An
// open-ended trim of an unbounded source stays unbounded rather than producing a
// multi-year length.
func (a *Audio) Subclip(start Time, end ...Time) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	// 0 is the "to the end" sentinel resolved by audio.Subclip against the
	// source duration, identical to the video facade.
	e := Time(0)
	if len(end) > 0 {
		e = end[0]
	}
	return &Audio{inner: audio.Subclip(a.inner, start, e)}
}

// WithStart sets the clip's composition start (where it begins in a mix).
func (a *Audio) WithStart(t Time) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	c := a.inner.WithStart(t)
	ac, ok := c.(audio.AudioClip)
	if !ok {
		return &Audio{err: fmt.Errorf("audio: WithStart returned %T, not AudioClip", c)}
	}
	return &Audio{inner: ac}
}

// WithDuration sets the clip's composition duration.
func (a *Audio) WithDuration(d Time) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	c := a.inner.WithDuration(d)
	ac, ok := c.(audio.AudioClip)
	if !ok {
		return &Audio{err: fmt.Errorf("audio: WithDuration returned %T, not AudioClip", c)}
	}
	return &Audio{inner: ac}
}

// Loop repeats the clip n times back to back.
func (a *Audio) Loop(n int) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	return &Audio{inner: audio.Loop(a.inner, n)}
}

// Speed time-scales the clip by factor (>1 faster/higher-pitched). A zero or
// negative factor is rejected with audio.ErrInvalidSpeed on the returned handle.
func (a *Audio) Speed(factor float64) *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	if factor <= 0 {
		return &Audio{err: clip.Wrap("speed", audio.ErrInvalidSpeed)}
	}
	return &Audio{inner: audio.Speed(a.inner, factor)}
}

// Reverse plays the clip backward (a sample flip). An unbounded clip has no end
// to reflect from and is returned unchanged.
func (a *Audio) Reverse() *Audio {
	if a.err != nil || a.inner == nil {
		return a
	}
	return &Audio{inner: audio.Reverse(a.inner)}
}

// Fx applies a custom audio effect. Pass a configured effect value to customize
// its behavior.
func (a *Audio) Fx(e effect.AudioEffect) *Audio {
	if a.err != nil || a.inner == nil || e == nil {
		return a
	}
	next, err := e.ApplyAudio(a.inner)
	if err != nil {
		return &Audio{err: err}
	}
	return &Audio{inner: next}
}

// Err returns the first build error recorded on this handle, if any.
func (a *Audio) Err() error { return a.err }

// Duration reports the clip's duration. It returns clip.Infinite when the
// length is unknown or unbounded (including a nil handle).
func (a *Audio) Duration() Time {
	if a.inner == nil {
		return Infinite
	}
	return a.inner.Duration()
}

// Close releases the clip's resources. It is idempotent.
func (a *Audio) Close() error {
	if a.inner == nil {
		return nil
	}
	return a.inner.Close()
}

// AudioClip returns the video's audio sidecar as an Audio handle, or nil when
// the video has no audio.
func (v *Video) AudioClip() *Audio {
	if v.err != nil {
		return &Audio{err: v.err}
	}
	a := v.inner.Audio()
	if a == nil {
		return nil
	}
	return &Audio{inner: a}
}

// WithAudio replaces the video's audio sidecar with a's clip. A nil handle (or a
// handle with no clip) removes the audio.
func (v *Video) WithAudio(a *Audio) *Video {
	if v.err != nil {
		return v
	}
	if a != nil && a.err != nil {
		c := v.derive(v.inner)
		c.err = a.err
		return c
	}
	var ac audio.AudioClip
	if a != nil {
		ac = a.inner
	}
	return v.derive(video.WithAudio(v.inner, ac))
}

// WithoutAudio removes the video's audio sidecar.
func (v *Video) WithoutAudio() *Video {
	if v.err != nil {
		return v
	}
	return v.derive(video.WithoutAudio(v.inner))
}

// Volume scales the video's audio sidecar by factor. It is a no-op when the
// video has no audio.
func (v *Video) Volume(factor float64) *Video {
	return v.mapAudio(func(a audio.AudioClip) audio.AudioClip { return audiofx.MultiplyVolume(a, factor) })
}

// AudioFadeIn ramps the video's audio up from silence over the first d.
func (v *Video) AudioFadeIn(d Time) *Video {
	return v.mapAudio(func(a audio.AudioClip) audio.AudioClip { return audiofx.FadeIn(a, d) })
}

// AudioFadeOut ramps the video's audio down to silence over the last d.
func (v *Video) AudioFadeOut(d Time) *Video {
	return v.mapAudio(func(a audio.AudioClip) audio.AudioClip { return audiofx.FadeOut(a, d) })
}

// mapAudio applies fn to the video's audio sidecar and re-attaches it. It is a
// no-op when there is no audio.
func (v *Video) mapAudio(fn func(audio.AudioClip) audio.AudioClip) *Video {
	if v.err != nil {
		return v
	}
	a := v.inner.Audio()
	if a == nil {
		return v
	}
	return v.derive(video.WithAudio(v.inner, fn(a)))
}
