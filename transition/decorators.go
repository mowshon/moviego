package transition

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
)

// Eased remaps progress through an easing curve before calling t.Frame, so any
// transition can be eased without the transition knowing about easing. A nil
// curve returns t unchanged. When t also blends alpha (MaskTransition) the eased
// transition does too.
func Eased(t Transition, e ease.Func) Transition {
	if e == nil {
		return t
	}
	base := easedTransition{t: t, e: e}
	if mt, ok := t.(MaskTransition); ok {
		return easedMask{easedTransition: base, mt: mt}
	}
	return base
}

type easedTransition struct {
	t Transition
	e ease.Func
}

func (x easedTransition) Frame(p float64, dst, a, b *clip.Frame) {
	x.t.Frame(clampUnit(x.e(p)), dst, a, b)
}
func (x easedTransition) Name() string { return x.t.Name() }

type easedMask struct {
	easedTransition
	mt MaskTransition
}

func (x easedMask) FrameMask(p float64, dst, a, b *clip.Frame) {
	x.mt.FrameMask(clampUnit(x.e(p)), dst, a, b)
}

// Reversed swaps A and B and inverts progress, turning "wipe-left" into
// "wipe-right" without a second implementation. When the wrapped transition
// blends alpha the reversed one does too.
func Reversed(t Transition) Transition {
	base := reversedTransition{t: t}
	if mt, ok := t.(MaskTransition); ok {
		return reversedMask{reversedTransition: base, mt: mt}
	}
	return base
}

type reversedTransition struct{ t Transition }

func (x reversedTransition) Frame(p float64, dst, a, b *clip.Frame) {
	x.t.Frame(1-p, dst, b, a)
}
func (x reversedTransition) Name() string { return x.t.Name() + "-reversed" }

type reversedMask struct {
	reversedTransition
	mt MaskTransition
}

func (x reversedMask) FrameMask(p float64, dst, a, b *clip.Frame) {
	x.mt.FrameMask(1-p, dst, b, a)
}
