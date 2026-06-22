package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/keyframe"
	"github.com/mowshon/moviego/v2/video"
)

// Scale animates a uniform zoom about the frame center over clip-local time. The
// clip size stays fixed: the content is resampled by the keyframed factor and
// re-centered, so a factor > 1 crops to the frame (zoom in) and a factor < 1
// letterboxes it (zoom out) — black for an opaque clip, transparent for a mask.
// Keeping the size fixed lets the result export standalone or composite without
// a per-frame-varying frame size. Reach it through Animate(PropScale, …).
type Scale struct {
	Track keyframe.Track
}

func (s Scale) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (s Scale) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if s.Track.Empty() {
		return nil, ErrNoKeyframes
	}
	tr := s.Track
	// One intermediate-frame pool shared by the RGB and mask transforms. The pool
	// keys by byte length: RGB24 and Gray8 frames of the same dimensions have a 3:1
	// ratio of bytes so they land in different buckets and never collide.
	pool := clip.NewFramePool()
	fn := func(t clip.Time, dst, src *clip.Frame) error {
		return scaleCenteredInto(dst, src, tr.Eval(t), pool)
	}
	return video.NewTransform(c, c.Size(), fn, fn), nil
}

// scaleCenteredInto resamples src by factor and writes it centered in dst (same
// dimensions as src), clearing the uncovered margin. A factor at or below zero
// yields an empty (cleared) frame.
func scaleCenteredInto(dst, src *clip.Frame, factor float64, pool *clip.FramePool) error {
	if factor <= 0 {
		clear(dst.Pix)
		return nil
	}
	cw := int(float64(src.W)*factor + 0.5)
	ch := int(float64(src.H)*factor + 0.5)
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	if cw == dst.W && ch == dst.H {
		copy(dst.Pix, src.Pix)
		return nil
	}
	tmp := pool.Get(cw, ch, src.Format)
	defer tmp.Release()
	if err := imagex.ResizeInto(tmp, src); err != nil {
		return err
	}
	blitCentered(dst, tmp)
	return nil
}

// blitCentered clears dst, then copies src into it centered, keeping only the
// overlapping region. It handles src both larger than dst (center crop) and
// smaller (centered with a cleared border), and any mix of the two axes.
func blitCentered(dst, src *clip.Frame) {
	clear(dst.Pix)
	bpp := dst.Format.BytesPerPixel()
	ox := (dst.W - src.W) / 2
	oy := (dst.H - src.H) / 2
	x0, y0 := max(0, ox), max(0, oy)
	x1, y1 := min(dst.W, ox+src.W), min(dst.H, oy+src.H)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	rowBytes := (x1 - x0) * bpp
	for y := y0; y < y1; y++ {
		so := (y-oy)*src.Stride + (x0-ox)*bpp
		do := y*dst.Stride + x0*bpp
		copy(dst.Pix[do:do+rowBytes], src.Pix[so:so+rowBytes])
	}
}
