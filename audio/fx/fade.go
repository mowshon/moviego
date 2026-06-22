package audiofx

import (
	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// FadeIn ramps an audio clip's volume linearly from 0 to 1 over the first d of
// its local timeline. A non-positive d returns the clip unchanged.
func FadeIn(inner audio.AudioClip, d clip.Time) audio.AudioClip {
	if d <= 0 {
		return inner
	}
	return &gainNode{inner: inner, gain: func(t clip.Time) float32 {
		if t <= 0 {
			return 0
		}
		if t >= d {
			return 1
		}
		return float32(t) / float32(d)
	}}
}

// FadeOut ramps an audio clip's volume linearly from 1 to 0 over the last d of
// its local timeline. It requires a known duration; without one (or with a
// non-positive d) the clip is returned unchanged.
func FadeOut(inner audio.AudioClip, d clip.Time) audio.AudioClip {
	dur := inner.Duration()
	if d <= 0 || !clip.Finite(dur) {
		return inner
	}
	fadeStart := dur - d
	return &gainNode{inner: inner, gain: func(t clip.Time) float32 {
		if t <= fadeStart {
			return 1
		}
		if t >= dur {
			return 0
		}
		return float32(dur-t) / float32(d)
	}}
}
