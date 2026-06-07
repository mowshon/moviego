// Package ease provides easing curves: pure functions that remap a normalized
// progress value to shape how an animation or transition accelerates.
//
// It is a tiny, dependency-free package shared across the library. Transitions
// remap their progress through an ease.Func (transition.Eased), and the
// keyframe and time-remap engines interpolate along one. An ease.Func takes a
// progress p, conventionally in [0,1], and returns the eased progress; the
// built-ins keep the endpoints fixed (f(0)=0, f(1)=1) so they compose cleanly.
package ease

// Func remaps a normalized progress value. By convention the input and output
// run over [0,1] with the endpoints fixed, but a Func is an ordinary function
// and may be called with any value; the built-ins are defined by their formula
// for every input.
type Func func(float64) float64

// Linear is the identity curve: progress passes through unchanged.
func Linear(p float64) float64 { return p }

// EaseIn accelerates from zero (slow start, fast finish). It is the quadratic
// p*p.
func EaseIn(p float64) float64 { return p * p }

// EaseOut decelerates to one (fast start, slow finish). It is the quadratic
// mirror of EaseIn.
func EaseOut(p float64) float64 { return p * (2 - p) }

// EaseInOut accelerates then decelerates, symmetric about the midpoint. It is
// the piecewise quadratic that matches EaseIn over the first half and EaseOut
// over the second.
func EaseInOut(p float64) float64 {
	if p < 0.5 {
		return 2 * p * p
	}
	return -1 + (4-2*p)*p
}

// EaseSmooth is the classic smoothstep 3p²-2p³: a gentle S-curve with zero
// slope at both endpoints, useful for camera moves and speed ramps where
// EaseInOut's midpoint kink is undesirable.
func EaseSmooth(p float64) float64 { return p * p * (3 - 2*p) }
