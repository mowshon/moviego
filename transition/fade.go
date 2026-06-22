package transition

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite/blend"
)

// FadeThroughColor fades A down to a solid color and then up into B ("fade to
// black" when Color is left at its zero value). Mid is the progress at which the
// frame is fully the color; it defaults to 0.5 (a symmetric dip). The through
// color is opaque, so the alpha sidecar fades A's alpha up to fully opaque at
// Mid, then down to B's alpha.
type FadeThroughColor struct {
	Color [3]byte
	Mid   float64
}

var (
	_ Transition     = FadeThroughColor{}
	_ MaskTransition = FadeThroughColor{}
)

func (FadeThroughColor) Name() string { return "fade-through-color" }

// mid returns the configured crossover progress, defaulting to 0.5.
func (f FadeThroughColor) mid() float64 {
	if f.Mid <= 0 || f.Mid >= 1 {
		return 0.5
	}
	return f.Mid
}

func (f FadeThroughColor) Frame(p float64, dst, a, b *clip.Frame) {
	mid := f.mid()
	if p < mid {
		// A -> color over [0, mid): weight toward the color rises 0 -> 1.
		blendTowardColor(dst, a, f.Color, unitToByte(p/mid))
		return
	}
	// color -> B over [mid, 1]: weight toward B rises 0 -> 1.
	blendFromColor(dst, b, f.Color, unitToByte((p-mid)/(1-mid)))
}

func (f FadeThroughColor) FrameMask(p float64, dst, a, b *clip.Frame) {
	mid := f.mid()
	if p < mid {
		// A's alpha -> fully opaque (the solid color hides everything).
		blendAlphaToward(dst, a, 255, unitToByte(p/mid))
		return
	}
	// fully opaque -> B's alpha.
	blendAlphaToward(dst, b, 255, unitToByte((1-p)/(1-mid)))
}

// blendTowardColor writes dst = src*(255-t) + col*t per channel: src fades into
// the solid color as t rises to 255.
func blendTowardColor(dst, src *clip.Frame, col [3]byte, t byte) {
	n := dst.W * dst.H
	for i := 0; i < n; i++ {
		o := i * 3
		dst.Pix[o] = blend.Blend1(col[0], src.Pix[o], t)
		dst.Pix[o+1] = blend.Blend1(col[1], src.Pix[o+1], t)
		dst.Pix[o+2] = blend.Blend1(col[2], src.Pix[o+2], t)
	}
}

// blendFromColor writes dst = col*(255-t) + src*t per channel: the solid color
// resolves into src as t rises to 255.
func blendFromColor(dst, src *clip.Frame, col [3]byte, t byte) {
	n := dst.W * dst.H
	for i := 0; i < n; i++ {
		o := i * 3
		dst.Pix[o] = blend.Blend1(src.Pix[o], col[0], t)
		dst.Pix[o+1] = blend.Blend1(src.Pix[o+1], col[1], t)
		dst.Pix[o+2] = blend.Blend1(src.Pix[o+2], col[2], t)
	}
}

// blendAlphaToward writes the Gray8 alpha dst = src*(255-t) + val*t, blending
// the source alpha toward the constant val as t rises to 255.
func blendAlphaToward(dst, src *clip.Frame, val, t byte) {
	n := dst.W * dst.H
	for i := 0; i < n; i++ {
		dst.Pix[i] = blend.Blend1(val, src.Pix[i], t)
	}
}
