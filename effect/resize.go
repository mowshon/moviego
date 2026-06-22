package effect

import (
	"errors"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// ErrInvalidResize reports a Resize with no usable target: a non-positive Scale
// and no positive Width or Height. It catches a silent no-op from a caller
// mistake such as Resize{Scale: 0}.
var ErrInvalidResize = errors.New("resize: set a positive Scale, Width, or Height")

// Resize rescales a clip's frames (and its mask). Set Scale for a uniform
// factor, or Width/Height for a target size; giving only one of Width/Height
// preserves the aspect ratio. The mask is resized with the same kernel.
type Resize struct {
	Width  int
	Height int
	Scale  float64
}

func (r Resize) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (r Resize) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if r.Scale <= 0 && r.Width <= 0 && r.Height <= 0 {
		return nil, ErrInvalidResize
	}
	out := r.outSize(c.Size())
	if out.W < 1 {
		out.W = 1
	}
	if out.H < 1 {
		out.H = 1
	}
	fn := func(dst, src *clip.Frame) error { return imagex.ResizeInto(dst, src) }
	filterFn := func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
		in := ctx.Inputs[0]
		return ffmpeg.FilterFragment{
			Lines: []string{ffmpeg.Scale(in.Label, ctx.OutLabel, out.W, out.H)},
			Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
		}, true
	}
	return geometry(c, out, fn, fn, filterFn)
}

// outSize resolves the target size from the configured fields, given the input.
func (r Resize) outSize(in clip.Size) clip.Size {
	switch {
	case r.Scale > 0:
		return clip.Size{W: round(float64(in.W) * r.Scale), H: round(float64(in.H) * r.Scale)}
	case r.Width > 0 && r.Height > 0:
		return clip.Size{W: r.Width, H: r.Height}
	case r.Width > 0:
		return clip.Size{W: r.Width, H: round(float64(in.H) * float64(r.Width) / float64(in.W))}
	case r.Height > 0:
		return clip.Size{W: round(float64(in.W) * float64(r.Height) / float64(in.H)), H: r.Height}
	default:
		return in
	}
}

func round(f float64) int {
	if f < 0 {
		return int(f - 0.5)
	}
	return int(f + 0.5)
}
