package mgo

import "github.com/mowshon/moviego/v2/ease"

// EaseFunc is an easing curve: a pure remap of normalized progress, used by
// transitions and the keyframe/time-remap engines. See the ease package.
type EaseFunc = ease.Func

// Easing curves, re-exported from the ease package so callers need only import
// the facade. Linear passes progress through; EaseIn/Out/InOut are quadratic
// accelerate/decelerate/both; EaseSmooth is smoothstep (an S-curve with zero
// slope at both ends).
var (
	Linear     = ease.Linear
	EaseIn     = ease.EaseIn
	EaseOut    = ease.EaseOut
	EaseInOut  = ease.EaseInOut
	EaseSmooth = ease.EaseSmooth
)
