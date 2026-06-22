package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
)

// rgbLUT is a per-channel 8-bit lookup table. A color op whose output channel
// depends only on the same input channel (brightness, contrast, gamma, invert,
// color balance) is exactly this shape, so the per-frame work is three table
// reads per pixel with no float math in the hot loop. Channels that share one
// curve point all three entries at the same table.
type rgbLUT [3][256]byte

// channelLUT builds one 8-bit table from a normalized curve: each input byte is
// mapped to [0,1], passed through fn, and written back clamped to a byte.
func channelLUT(fn func(v float64) float64) (t [256]byte) {
	for i := 0; i < 256; i++ {
		t[i] = clampByte(int(fn(float64(i)/255)*255 + 0.5))
	}
	return t
}

// uniformLUT builds an rgbLUT whose three channels share one curve.
func uniformLUT(fn func(v float64) float64) rgbLUT {
	t := channelLUT(fn)
	return rgbLUT{t, t, t}
}

// applyRGBLUT writes src mapped through lut into dst. Both are RGB24 frames of
// equal size (color effects never touch the Gray8 mask sidecar).
func applyRGBLUT(dst, src *clip.Frame, lut *rgbLUT) {
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			i := x * 3
			d[i] = lut[0][s[i]]
			d[i+1] = lut[1][s[i+1]]
			d[i+2] = lut[2][s[i+2]]
		}
	}
}

// lutEffect wraps a precomputed rgbLUT as a Video-only PixelFn, capturing the
// table once so each frame is pure lookups. filter advertises the FFmpeg
// equivalent for fusion (nil for a Go-only effect); PixelFn drops it when start
// is non-zero.
func lutEffect(name string, lut rgbLUT, start clip.Time, filter func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)) PixelFn {
	l := lut
	return PixelFn{
		Name:   name,
		Fn:     func(_ clip.Time, dst, src *clip.Frame) error { applyRGBLUT(dst, src, &l); return nil },
		Start:  start,
		Filter: filter,
	}
}

// unaryFilter adapts a one-input filtergraph line builder into the Filter
// closure shape, carrying the input pad's rate and pixel format through. It
// keeps each color effect's fusion advertisement to a single line.
func unaryFilter(build func(in, out string) string) func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
	return func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
		in := ctx.Inputs[0]
		return ffmpeg.FilterFragment{
			Lines: []string{build(in.Label, ctx.OutLabel)},
			Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
		}, true
	}
}

// videoPixelFn builds a Video-only PixelFn from a time-independent per-frame
// function, the shape every channel-mixing color op (saturation, HSL, vignette)
// shares.
func videoPixelFn(name string, fn func(dst, src *clip.Frame) error, start clip.Time, filter func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)) PixelFn {
	return PixelFn{
		Name:   name,
		Fn:     func(_ clip.Time, dst, src *clip.Frame) error { return fn(dst, src) },
		Start:  start,
		Filter: filter,
	}
}
