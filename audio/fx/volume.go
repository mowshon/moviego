package audiofx

import (
	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// MultiplyVolume scales an audio clip's amplitude by a constant factor (0.5 =
// half volume, 2 = double). The returned clip applies the gain lazily in
// SamplesInto; the input is unchanged.
func MultiplyVolume(inner audio.AudioClip, factor float64) audio.AudioClip {
	f := float32(factor)
	return &gainNode{inner: inner, gain: func(_ clip.Time) float32 { return f }}
}
