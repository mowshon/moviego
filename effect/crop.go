package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// Crop extracts the rectangle (X, Y, W, H) from each frame and its mask. The
// rectangle is clamped to the clip bounds so an oversized request — including an
// X/Y past the source edge — yields the largest in-bounds window (at least 1px)
// rather than an error or a negative dimension.
type Crop struct {
	X, Y, W, H int
}

func (c Crop) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (cr Crop) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	in := c.Size()
	x, y, w, h := clampRect(cr.X, cr.Y, cr.W, cr.H, in)
	out := clip.Size{W: w, H: h}
	fn := func(dst, src *clip.Frame) error { return imagex.CropInto(dst, src, x, y) }
	filterFn := func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
		in := ctx.Inputs[0]
		return ffmpeg.FilterFragment{
			Lines: []string{ffmpeg.Crop(in.Label, ctx.OutLabel, w, h, x, y)},
			Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
		}, true
	}
	return geometry(c, out, fn, fn, filterFn)
}

// clampRect clamps a crop rectangle to lie within bounds, keeping a positive
// width and height. The origin is also clamped to the last in-bounds column/row
// (bounds-1), so an X/Y beyond the source still yields a 1px window instead of a
// negative width/height from bounds.W-x.
func clampRect(x, y, w, h int, bounds clip.Size) (int, int, int, int) {
	if x < 0 {
		x = 0
	} else if x > bounds.W-1 {
		x = bounds.W - 1
	}
	if y < 0 {
		y = 0
	} else if y > bounds.H-1 {
		y = bounds.H - 1
	}
	if w <= 0 || x+w > bounds.W {
		w = bounds.W - x
	}
	if h <= 0 || y+h > bounds.H {
		h = bounds.H - y
	}
	return x, y, w, h
}
