package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// FadeIn ramps the RGB from Color up to the clip over the first Dur of
// playback. It transforms only the RGB channel; the mask and audio are
// untouched. A zero Dur is a no-op (the clip plays at full strength from t=0).
type FadeIn struct {
	Dur   clip.Time
	Color [3]byte
}

func (f FadeIn) Targets() EffectTargets { return EffectTargets{Video: true} }

func (f FadeIn) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if f.Dur <= 0 {
		return c, nil
	}
	dur := f.Dur
	col := f.Color
	rgbFn := func(t clip.Time, dst, src *clip.Frame) error {
		factor := 1.0
		if dur > 0 && t < dur {
			factor = float64(t) / float64(dur)
		}
		blendToward(dst, src, col, factor)
		return nil
	}
	n := video.NewTransform(c, c.Size(), rgbFn, nil)
	if dur > 0 {
		n = n.WithFilter(func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
			in := ctx.Inputs[0]
			return ffmpeg.FilterFragment{
				Lines: []string{ffmpeg.Fade(in.Label, ctx.OutLabel, ffmpeg.FadeInKind, 0, dur, col)},
				Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
			}, true
		})
	}
	return n, nil
}

// FadeOut ramps the RGB from the clip down to Color over the last Dur. It needs
// a known duration to locate the end and returns ErrNoDuration otherwise.
type FadeOut struct {
	Dur   clip.Time
	Color [3]byte
}

func (f FadeOut) Targets() EffectTargets { return EffectTargets{Video: true} }

func (f FadeOut) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if f.Dur <= 0 {
		return c, nil
	}
	total := c.Duration()
	if !clip.Finite(total) {
		return nil, clip.Wrap("fade out", clip.ErrNoDuration)
	}
	dur := f.Dur
	col := f.Color
	rgbFn := func(t clip.Time, dst, src *clip.Frame) error {
		factor := 1.0
		if dur > 0 {
			remaining := total - t
			if remaining < dur {
				factor = float64(remaining) / float64(dur)
				if factor < 0 {
					factor = 0
				}
			}
		}
		blendToward(dst, src, col, factor)
		return nil
	}
	n := video.NewTransform(c, c.Size(), rgbFn, nil)
	// Advertise the FFmpeg fade only when the fade window fits inside the clip
	// (dur <= total). When dur > total the Go fade starts already partway faded
	// (factor = remaining/dur = total/dur < 1 at t=0), but an FFmpeg
	// fade=out:st=0 would start from the full frame — a visible divergence — and
	// FFmpeg has no clean way to begin a fade-out mid-curve. So an over-long
	// fade-out simply declines fusion and renders in Go, which is exact.
	if dur > 0 && dur <= total {
		st := total - dur
		n = n.WithFilter(func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
			in := ctx.Inputs[0]
			return ffmpeg.FilterFragment{
				Lines: []string{ffmpeg.Fade(in.Label, ctx.OutLabel, ffmpeg.FadeOutKind, st, dur, col)},
				Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
			}, true
		})
	}
	return n, nil
}

// blendToward writes src*factor + col*(1-factor) into dst, per channel, with
// rounding. factor is clamped to [0, 1]. dst and src are RGB24 of equal size.
func blendToward(dst, src *clip.Frame, col [3]byte, factor float64) {
	if factor > 1 {
		factor = 1
	}
	if factor < 0 {
		factor = 0
	}
	a := int(factor*255 + 0.5)
	b := 255 - a
	cr, cg, cb := int(col[0])*b, int(col[1])*b, int(col[2])*b
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			i := x * 3
			d[i] = byte((int(s[i])*a + cr + 127) / 255)
			d[i+1] = byte((int(s[i+1])*a + cg + 127) / 255)
			d[i+2] = byte((int(s[i+2])*a + cb + 127) / 255)
		}
	}
}
