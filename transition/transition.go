package transition

import "github.com/mowshon/moviego/v2/clip"

// Transition produces the frames shown while an outgoing clip hands over to an
// incoming clip. Frame is a pure function of progress and the two source frames
// — the same shape as the compositor's per-row blends — so an implementation
// holds only immutable config and is safe to share across the parallel render
// pipeline.
type Transition interface {
	// Frame writes the transition result at progress p into dst. p == 0 is fully
	// A, p == 1 is fully B. dst, a and b are RGB24 frames of the same size;
	// implementations must not retain them past the call.
	Frame(p float64, dst, a, b *clip.Frame)

	// Name identifies the transition for debug output and Describe reports (e.g.
	// "crossfade", "wipe-left").
	Name() string
}

// MaskTransition is an optional extension for transitions that also blend the
// alpha sidecar (transparent clips). The engine type-asserts for it at runtime;
// a plain Transition is treated as opaque, so the common case stays one method.
type MaskTransition interface {
	Transition
	// FrameMask writes the blended alpha at progress p into dst. dst, a and b are
	// Gray8 frames of the same size.
	FrameMask(p float64, dst, a, b *clip.Frame)
}

// funcTransition adapts a plain function into a Transition.
type funcTransition struct {
	name string
	fn   func(p float64, dst, a, b *clip.Frame)
}

// Func adapts a function into a Transition for one-off inline use, avoiding a
// type declaration:
//
//	fade := transition.Func("fade", func(p float64, dst, a, b *clip.Frame) {
//	    // blend A and B by p
//	})
func Func(name string, fn func(p float64, dst, a, b *clip.Frame)) Transition {
	return funcTransition{name: name, fn: fn}
}

func (f funcTransition) Frame(p float64, dst, a, b *clip.Frame) { f.fn(p, dst, a, b) }
func (f funcTransition) Name() string                           { return f.name }

// clampUnit clamps p into [0,1]; an easing curve may overshoot and the built-ins
// index tables and pixels by p, so clamping keeps every transition in bounds.
func clampUnit(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// unitToByte maps a clamped progress to a 0..255 blend weight, rounded.
func unitToByte(p float64) byte {
	return byte(clampUnit(p)*255 + 0.5)
}

// copyAllPixels copies src verbatim into dst row by row. dst and src share
// dimensions and format; it works for both RGB24 and Gray8 frames and is stride
// safe (it copies only the W*bpp bytes of each row).
func copyAllPixels(dst, src *clip.Frame) {
	rowBytes := dst.W * dst.Format.BytesPerPixel()
	for y := 0; y < dst.H; y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+rowBytes], src.Pix[y*src.Stride:y*src.Stride+rowBytes])
	}
}
