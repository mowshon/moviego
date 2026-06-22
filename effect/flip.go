package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// FlipH mirrors a clip left-to-right. The mask is flipped identically, and the
// node advertises FFmpeg's hflip for fusion. It is a same-size per-pixel effect
// built on PixelFn.
type FlipH struct {
	// Start is when the flip begins on the clip timeline; before it each frame
	// passes through unflipped. The zero value flips from the beginning.
	Start clip.Time
}

func (FlipH) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (f FlipH) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	return PixelFn{
		Name:     "flip-h",
		Sidecars: EffectTargets{Mask: true},
		Fn:       func(_ clip.Time, dst, src *clip.Frame) error { return imagex.FlipHInto(dst, src) },
		Start:    f.Start,
		Filter: func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
			in := ctx.Inputs[0]
			return ffmpeg.FilterFragment{
				Lines: []string{ffmpeg.HFlip(in.Label, ctx.OutLabel)},
				Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
			}, true
		},
	}.ApplyVideo(c)
}

// FlipV mirrors a clip top-to-bottom. The mask is flipped identically, and the
// node advertises FFmpeg's vflip for fusion.
type FlipV struct {
	// Start is when the flip begins on the clip timeline; before it each frame
	// passes through unflipped. The zero value flips from the beginning.
	Start clip.Time
}

func (FlipV) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (f FlipV) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	return PixelFn{
		Name:     "flip-v",
		Sidecars: EffectTargets{Mask: true},
		Fn:       func(_ clip.Time, dst, src *clip.Frame) error { return imagex.FlipVInto(dst, src) },
		Start:    f.Start,
		Filter: func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
			in := ctx.Inputs[0]
			return ffmpeg.FilterFragment{
				Lines: []string{ffmpeg.VFlip(in.Label, ctx.OutLabel)},
				Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
			}, true
		},
	}.ApplyVideo(c)
}
