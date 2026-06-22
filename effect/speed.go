package effect

import (
	"errors"

	"github.com/mowshon/moviego/v2/video"
)

// ErrInvalidFactor reports a non-positive speed factor. Only forward (positive)
// speed is supported; reverse/zero would yield a non-monotonic or undefined
// source-time map and break the planner's scheduling assumptions.
var ErrInvalidFactor = errors.New("multiply speed: factor must be positive")

// MultiplySpeed plays the clip faster (Factor > 1) or slower (Factor < 1). It
// is a time transform, so it applies uniformly to the RGB, mask, and audio: the
// same output→source time map drives all three through the inner clip. Only
// positive factors are supported.
type MultiplySpeed struct {
	Factor float64
}

func (m MultiplySpeed) Targets() EffectTargets {
	return EffectTargets{Video: true, Mask: true, Audio: true}
}

func (m MultiplySpeed) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if m.Factor <= 0 {
		return nil, ErrInvalidFactor
	}
	return video.MultiplySpeed(c, m.Factor), nil
}
