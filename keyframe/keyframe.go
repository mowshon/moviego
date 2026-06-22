// Package keyframe interpolates a scalar property along time-stamped keyframes.
//
// It is the shared engine behind the facade's Animate (opacity, scale) and
// animated placement, and reuses the ease package for the curve applied within
// each segment. A Track is immutable once built and Eval is a pure function of
// time, so a Track is safe to share across the parallel render pipeline.
package keyframe

import (
	"sort"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
)

// Key pins a scalar value to a clip-local time.
type Key struct {
	At  clip.Time
	Val float64
}

// Track is a sorted sequence of keyframes with an easing curve applied within
// each segment. The zero Track is empty; build one with New.
type Track struct {
	keys []Key
	ease ease.Func
}

// New builds a Track from keys and an easing curve. The keys are copied and
// sorted by time, so the caller's slice is neither retained nor required to be
// pre-sorted. A nil curve defaults to ease.Linear.
func New(keys []Key, e ease.Func) Track {
	if e == nil {
		e = ease.Linear
	}
	cp := append([]Key(nil), keys...)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].At < cp[j].At })
	return Track{keys: cp, ease: e}
}

// Empty reports whether the track has no keyframes.
func (t Track) Empty() bool { return len(t.keys) == 0 }

// Eval returns the value at time at. It holds the first value before the first
// keyframe and the last value after the last one (clamped, never extrapolated).
// Between two keyframes the normalized progress is shaped by the easing curve
// and linearly interpolates the bracketing values. An empty track returns 0.
func (t Track) Eval(at clip.Time) float64 {
	if len(t.keys) == 0 {
		return 0
	}
	if at <= t.keys[0].At {
		return t.keys[0].Val
	}
	last := len(t.keys) - 1
	if at >= t.keys[last].At {
		return t.keys[last].Val
	}
	// Keyframe counts are tiny (a handful per animation), so a linear scan for
	// the bracketing segment [i, i+1) is simplest and fast enough.
	i := 0
	for i < last && t.keys[i+1].At <= at {
		i++
	}
	a, b := t.keys[i], t.keys[i+1]
	span := b.At - a.At
	if span <= 0 {
		return b.Val
	}
	p := t.ease(float64(at-a.At) / float64(span))
	return a.Val + (b.Val-a.Val)*p
}
