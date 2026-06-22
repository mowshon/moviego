package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// Blur softens a clip by downscaling and upscaling through the CatmullRom
// kernel: a cheap blur whose strength grows with Radius. It is a same-size
// per-pixel effect. A zero or negative Radius is a passthrough.
//
// The mask is blurred identically. This blur has no faithful single-filter
// FFmpeg form, so it renders on the Go path.
type Blur struct {
	// Radius sets the blur strength in pixels (larger = softer).
	Radius float64
	// Start is when the blur begins on the clip timeline; before it each frame
	// passes through sharp. The zero value blurs from the beginning.
	Start clip.Time
}

func (b Blur) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (b Blur) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if b.Radius <= 0 {
		return c, nil
	}
	pool := clip.NewFramePool()
	fn := func(_ clip.Time, dst, src *clip.Frame) error {
		return blurInto(dst, src, b.Radius, pool)
	}
	return PixelFn{
		Name:     "blur",
		Sidecars: EffectTargets{Mask: true},
		Fn:       fn,
		Start:    b.Start,
	}.ApplyVideo(c)
}

// blurInto resamples src down to a fraction of its size and back into dst, which
// smooths detail. The downscale factor grows with radius. A scratch frame for
// the reduced image comes from pool so a high frame count reuses buffers.
func blurInto(dst, src *clip.Frame, radius float64, pool *clip.FramePool) error {
	dw := scaleDown(src.W, radius)
	dh := scaleDown(src.H, radius)
	if dw == src.W && dh == src.H {
		copyPixels(dst, src)
		return nil
	}
	small := pool.Get(dw, dh, src.Format)
	defer small.Release()
	if err := imagex.ResizeInto(small, src); err != nil {
		return err
	}
	return imagex.ResizeInto(dst, small)
}

// scaleDown reduces dim by (1 + radius), clamped to at least 1, so a larger
// radius means a coarser intermediate and a softer result.
func scaleDown(dim int, radius float64) int {
	d := int(float64(dim)/(1+radius) + 0.5)
	if d < 1 {
		d = 1
	}
	return d
}

// copyPixels copies packed pixel data verbatim (both frames are packed).
func copyPixels(dst, src *clip.Frame) { copy(dst.Pix, src.Pix) }
