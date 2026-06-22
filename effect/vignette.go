package effect

import (
	"math"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// Vignette darkens the frame toward its corners. Amount is the strength at the
// corners in [0, 1] (0 is a passthrough); Radius is where the falloff begins as
// a fraction of the center-to-corner distance (0 darkens from the center
// outward, the default 0.5 keeps the inner half bright). It has no faithful
// single-filter form, so it renders on the Go path; only RGB is affected.
type Vignette struct {
	Amount float64
	Radius float64
	Start  clip.Time
}

func (vg Vignette) Targets() EffectTargets { return EffectTargets{Video: true} }

func (vg Vignette) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if vg.Amount <= 0 {
		return c, nil
	}
	radius := vg.Radius
	if radius <= 0 {
		radius = 0.5
	}
	// Keep radius strictly below 1 so the (1-radius) falloff denominator is never
	// zero; a radius at or past the corner just confines the darkening to the very
	// edge.
	if radius > maxVignetteRadius {
		radius = maxVignetteRadius
	}
	amount := math.Min(vg.Amount, 1)
	size := c.Size()
	gain := vignetteGain(size, amount, radius)
	fn := func(_ clip.Time, dst, src *clip.Frame) error { applyGain(dst, src, gain); return nil }
	return PixelFn{Name: "vignette", Fn: fn, Start: vg.Start}.ApplyVideo(c)
}

// maxVignetteRadius caps Radius just below 1 so the falloff denominator stays
// positive; at the cap the darkening is confined to the extreme corner.
const maxVignetteRadius = 0.999

// vignetteGain precomputes a per-pixel brightness multiplier: 1.0 within radius,
// easing down to (1 - amount) at the corners. Precomputing once keeps the
// per-frame work to one multiply per channel. radius is assumed in [0, 1).
func vignetteGain(size clip.Size, amount, radius float64) []float64 {
	cx, cy := float64(size.W-1)/2, float64(size.H-1)/2
	maxDist := math.Hypot(cx, cy)
	// A single-pixel (or degenerate) clip has no corner-to-center distance, so
	// there is nothing to darken; invDist 0 collapses every pixel to the center.
	var invDist float64
	if maxDist > 0 {
		invDist = 1 / maxDist
	}
	denom := 1 - radius
	gain := make([]float64, size.W*size.H)
	for y := 0; y < size.H; y++ {
		dy := float64(y) - cy
		for x := 0; x < size.W; x++ {
			dx := float64(x) - cx
			d := math.Hypot(dx, dy) * invDist
			f := clamp01((d - radius) / denom)
			gain[y*size.W+x] = 1 - amount*f*f
		}
	}
	return gain
}

// applyGain multiplies each RGB pixel by its precomputed gain.
func applyGain(dst, src *clip.Frame, gain []float64) {
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			g := gain[y*src.W+x]
			i := x * 3
			d[i] = clampByte(int(float64(s[i])*g + 0.5))
			d[i+1] = clampByte(int(float64(s[i+1])*g + 0.5))
			d[i+2] = clampByte(int(float64(s[i+2])*g + 0.5))
		}
	}
}
