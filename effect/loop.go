package effect

import (
	"errors"

	"github.com/mowshon/moviego/v2/video"
)

// ErrInvalidLoopCount reports a loop count below 1, which would silently yield a
// zero- or negative-length clip.
var ErrInvalidLoopCount = errors.New("loop: N must be at least 1")

// Loop repeats the clip N times. Like any time transform it applies uniformly
// to the RGB, mask, and audio through the inner clip.
type Loop struct {
	N int
}

func (l Loop) Targets() EffectTargets {
	return EffectTargets{Video: true, Mask: true, Audio: true}
}

func (l Loop) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if l.N < 1 {
		return nil, ErrInvalidLoopCount
	}
	return video.Loop(c, l.N), nil
}
