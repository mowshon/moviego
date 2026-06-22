package audiofx

import (
	"math"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// EqualPowerFadeIn ramps an audio clip in over the first d of its local timeline
// with a constant-power curve, gain = sqrt(t/d). Paired with EqualPowerFadeOut
// on the outgoing clip it keeps the summed power flat across a crossfade, where
// a linear pair dips ~3 dB at the midpoint. A non-positive d returns the clip
// unchanged.
func EqualPowerFadeIn(inner audio.AudioClip, d clip.Time) audio.AudioClip {
	if d <= 0 {
		return inner
	}
	df := float64(d)
	return &gainNode{inner: inner, gain: func(t clip.Time) float32 {
		if t <= 0 {
			return 0
		}
		if t >= d {
			return 1
		}
		return float32(math.Sqrt(float64(t) / df))
	}}
}

// EqualPowerFadeOut ramps an audio clip out over the last d of its local
// timeline with a constant-power curve, gain = sqrt((dur-t)/d). It requires a
// known duration; without one (or with a non-positive d) the clip is returned
// unchanged.
func EqualPowerFadeOut(inner audio.AudioClip, d clip.Time) audio.AudioClip {
	dur := inner.Duration()
	if d <= 0 || !clip.Finite(dur) {
		return inner
	}
	df := float64(d)
	fadeStart := dur - d
	return &gainNode{inner: inner, gain: func(t clip.Time) float32 {
		if t <= fadeStart {
			return 1
		}
		if t >= dur {
			return 0
		}
		return float32(math.Sqrt(float64(dur-t) / df))
	}}
}
