package mgo

import (
	"errors"

	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/keyframe"
	"github.com/mowshon/moviego/v2/video"
)

// Keyframe pins a scalar property value to a clip-local time for Animate.
// Between keyframes the value is interpolated through the easing curve.
type Keyframe = keyframe.Key

// TimePoint maps an output (playback) time to a source (input) time for
// TimeRemap. See video.TimePoint for the curve rules.
type TimePoint = video.TimePoint

// PositionKeyframe pins an absolute (x, y) pixel placement to a clip-local time
// for AnimatePosition.
type PositionKeyframe struct {
	At   Time
	X, Y float64
}

// Prop selects which scalar clip property Animate drives.
type Prop int

const (
	// PropOpacity animates the clip's alpha (0 transparent, 1 opaque) by
	// attaching and scaling a mask. It is visible when the clip is composited.
	PropOpacity Prop = iota
	// PropScale animates a uniform zoom about the frame center. The clip size is
	// preserved: <1 letterboxes the content, >1 crops it.
	PropScale
)

// errUnknownProp guards an out-of-range Prop passed to Animate.
var errUnknownProp = errors.New("animate: unknown property")

// Animate keyframes a scalar clip property over clip-local time, interpolating
// between keyframes with the easing curve e (nil means Linear):
//
//	v.Animate(mgo.PropOpacity, []mgo.Keyframe{{At: 0, Val: 0}, {At: mgo.Sec(1), Val: 1}}, mgo.EaseInOut)
//
// Use PropOpacity or PropScale; for placement use AnimatePosition. An empty
// keyframe list records a build error surfaced by Err/WriteVideo.
func (v *Video) Animate(p Prop, keys []Keyframe, e EaseFunc) *Video {
	if v.err != nil {
		return v
	}
	track := keyframe.New(keys, e)
	switch p {
	case PropOpacity:
		return v.apply(effect.Opacity{Track: track})
	case PropScale:
		return v.apply(effect.Scale{Track: track})
	default:
		c := v.derive(v.inner)
		c.err = errUnknownProp
		return c
	}
}

// AnimatePosition keyframes the clip's placement within a Composite over
// clip-local time, interpolating x and y independently through the easing curve
// e (nil means Linear). It drives the same animated-position hook as
// PositionFunc, so it is a no-op for a standalone export. An empty keyframe list
// records a build error surfaced by Err/WriteVideo.
func (v *Video) AnimatePosition(keys []PositionKeyframe, e EaseFunc) *Video {
	if v.err != nil {
		return v
	}
	if len(keys) == 0 {
		c := v.derive(v.inner)
		c.err = effect.ErrNoKeyframes
		return c
	}
	xs := make([]keyframe.Key, len(keys))
	ys := make([]keyframe.Key, len(keys))
	for i, k := range keys {
		xs[i] = keyframe.Key{At: k.At, Val: k.X}
		ys[i] = keyframe.Key{At: k.At, Val: k.Y}
	}
	xt, yt := keyframe.New(xs, e), keyframe.New(ys, e)
	c := v.derive(v.inner)
	c.pos = Position{Animated: func(t Time) (float64, float64) {
		return xt.Eval(t), yt.Eval(t)
	}}
	return c
}

// TimeRemap warps the clip onto a new timeline defined by output→source
// TimePoints, easing within each segment (e nil means Linear). An all-forward
// curve keeps the clip on the parallel pipeline; a segment that rewinds the
// source forces sequential (random-access) rendering. The audio sidecar is
// remapped to match. See video.TimeRemap.
func (v *Video) TimeRemap(points []TimePoint, e EaseFunc) *Video {
	return v.deriveOrErr(video.TimeRemap(v.inner, points, e))
}

// Freeze holds the frame at source time at for hold, then resumes from at. It
// needs a known duration. Freeze stays on the parallel pipeline.
func (v *Video) Freeze(at, hold Time) *Video {
	return v.deriveOrErr(video.Freeze(v.inner, at, hold))
}

// FreezeStart holds frame 0 for hold before the clip plays.
func (v *Video) FreezeStart(hold Time) *Video {
	return v.deriveOrErr(video.FreezeStart(v.inner, hold))
}

// FreezeEnd holds the last frame for hold after the clip plays.
func (v *Video) FreezeEnd(hold Time) *Video {
	return v.deriveOrErr(video.FreezeEnd(v.inner, hold))
}

// Reverse plays the clip backward, audio sidecar included. It needs a known
// duration. Reversing reads the source non-monotonically, so the export runs on
// the sequential engine with a backward-buffered reader (see Describe to confirm
// the engine).
func (v *Video) Reverse() *Video {
	return v.deriveOrErr(video.Reverse(v.inner))
}

// Boomerang plays the clip forward then immediately backward, a seamless
// back-and-forth loop of twice the original duration. It is Reverse joined onto
// the original with Concat, so like Reverse it renders sequentially.
func (v *Video) Boomerang() *Video {
	if v.err != nil {
		return v
	}
	rev := v.Reverse()
	if rev.err != nil {
		return rev
	}
	return Concat(v, rev)
}

// deriveOrErr wraps a video constructor result into a new handle, carrying any
// build error and any earlier one. It is the shared tail of the time-remap
// methods.
func (v *Video) deriveOrErr(inner video.VideoClip, err error) *Video {
	if v.err != nil {
		return v
	}
	if err != nil {
		c := v.derive(v.inner)
		c.err = err
		return c
	}
	return v.derive(inner)
}
