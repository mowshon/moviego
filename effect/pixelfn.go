package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// PixelFn is the low-burden hook for a custom same-size video effect. A
// contributor writes a single Fn that fills dst from src — both RGB24 frames of
// the same dimensions — and PixelFn supplies the full VideoClip plumbing by
// wrapping video.NewTransform. It covers per-pixel work; size-changing
// operations (Pad, FitTo, Crop, Resize) go through the geometry helper instead.
//
// Reach it through Fx:
//
//	v.Fx(effect.PixelFn{
//	    Name: "tint-red",
//	    Fn: func(t clip.Time, dst, src *clip.Frame) error {
//	        copy(dst.Pix, src.Pix)
//	        for i := 0; i < len(dst.Pix); i += 3 {
//	            dst.Pix[i] = 255
//	        }
//	        return nil
//	    },
//	})
type PixelFn struct {
	// Name identifies the effect in debug output. It is optional.
	Name string
	// Sidecars declares which sidecars the effect touches and is the single
	// source of truth: it becomes the effect's Targets(), and setting its Mask
	// flag is exactly what reuses Fn for the Gray8 alpha sidecar (otherwise the
	// mask passes through unchanged). The Video flag is implied — an effect with
	// no sidecars set still transforms RGB — so the zero value is Video-only.
	//
	// (It is named Sidecars rather than Targets because Go cannot have a struct
	// field and a method, Targets(), share a name.)
	Sidecars EffectTargets
	// Fn transforms one source frame into dst. dst and src share dimensions and
	// pixel format. It must not retain the frames past the call. When
	// Sidecars.Mask is set, Fn is also applied to the Gray8 alpha plane.
	Fn func(t clip.Time, dst, src *clip.Frame) error
	// Start is when the effect begins on the clip timeline. Before Start each
	// frame (and mask, when Sidecars.Mask is set) passes through unchanged; from
	// Start onward Fn applies. The zero value applies from the beginning. Because
	// it gates a same-size transform, the Filter advertisement is dropped whenever
	// Start is non-zero (a partial-timeline effect is not a single static filter).
	Start clip.Time
	// Filter, when set, advertises the equivalent FFmpeg filter via
	// TransformNode.WithFilter so a linear graph can fuse. Leave it nil for a
	// pure-Go effect. It is honored only when Start is zero.
	Filter func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)
}

// ErrNoPixelFn reports a PixelFn with no Fn set: there is nothing to apply.
var ErrNoPixelFn = clip.Wrap("pixel effect: Fn is nil", clip.ErrVideoCorrupted)

// Targets reports the sidecars the effect transforms, with Video always set.
func (p PixelFn) Targets() EffectTargets {
	t := p.Sidecars
	t.Video = true
	return t
}

// ApplyVideo wraps c in a per-frame transform driven by Fn. Fn is reused for the
// alpha sidecar exactly when Sidecars.Mask is set, so the declared target and
// the applied transform can never disagree. A non-zero Start gates Fn so frames
// before it pass through unchanged.
func (p PixelFn) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if p.Fn == nil {
		return nil, ErrNoPixelFn
	}
	rgbFn := gateFromStart(p.Fn, p.Start)
	var maskFn func(t clip.Time, dst, src *clip.Frame) error
	if p.Sidecars.Mask {
		maskFn = rgbFn
	}
	n := video.NewTransform(c, c.Size(), rgbFn, maskFn)
	// A start offset makes the node a no-op before Start, which no single static
	// filter expresses, so only advertise the filter for a from-the-start effect.
	if p.Filter != nil && p.Start <= 0 {
		n = n.WithFilter(p.Filter)
	}
	return n, nil
}

// gateFromStart returns fn unchanged when start is non-positive; otherwise it
// returns a wrapper that copies src into dst (passthrough) before start and
// applies fn from start onward.
func gateFromStart(fn func(t clip.Time, dst, src *clip.Frame) error, start clip.Time) func(t clip.Time, dst, src *clip.Frame) error {
	if start <= 0 {
		return fn
	}
	return func(t clip.Time, dst, src *clip.Frame) error {
		if t < start {
			copyFrameInto(dst, src)
			return nil
		}
		return fn(t, dst, src)
	}
}
