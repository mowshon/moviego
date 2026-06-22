package effect

import (
	"math"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// GaussianBlur is a true Gaussian blur applied as two separable 1D passes
// (horizontal then vertical), so the per-pixel cost is O(2r) rather than O(r²).
// Radius is the Gaussian sigma in pixels; a zero or negative Radius is a
// passthrough. It supersedes the cheaper resample Blur for quality (keep Blur
// for the fast path) and advertises FFmpeg's gblur for fusion. The mask is
// blurred identically.
type GaussianBlur struct {
	Radius float64
	Start  clip.Time
}

func (g GaussianBlur) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (g GaussianBlur) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if g.Radius <= 0 {
		return c, nil
	}
	kernel := gaussianKernel(g.Radius)
	pool := clip.NewFramePool()
	fn := func(_ clip.Time, dst, src *clip.Frame) error { return gaussianBlurInto(dst, src, kernel, pool) }
	return PixelFn{
		Name:     "gaussian-blur",
		Sidecars: EffectTargets{Mask: true},
		Fn:       fn,
		Start:    g.Start,
		Filter:   unaryFilter(func(in, out string) string { return ffmpeg.GBlur(in, out, g.Radius) }),
	}.ApplyVideo(c)
}

// MotionBlur smears each pixel along a direction, simulating motion. Angle is
// the smear direction in degrees (0 = horizontal); Distance is its length in
// pixels. A zero or negative Distance is a passthrough. It has no faithful
// single-filter form, so it renders on the Go path. The mask is blurred
// identically.
type MotionBlur struct {
	Angle    float64
	Distance float64
	Start    clip.Time
}

func (m MotionBlur) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (m MotionBlur) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if m.Distance <= 0 {
		return c, nil
	}
	sin, cos := math.Sincos(m.Angle * math.Pi / 180)
	fn := func(_ clip.Time, dst, src *clip.Frame) error {
		motionBlurInto(dst, src, cos, sin, m.Distance)
		return nil
	}
	return PixelFn{
		Name:     "motion-blur",
		Sidecars: EffectTargets{Mask: true},
		Fn:       fn,
		Start:    m.Start,
	}.ApplyVideo(c)
}

// gaussianKernel returns a normalized 1D Gaussian kernel for the given sigma.
// Its half-width is 3 sigma (the radius past which weights are negligible),
// clamped to at least 1.
func gaussianKernel(sigma float64) []float64 {
	rad := int(math.Ceil(3 * sigma))
	if rad < 1 {
		rad = 1
	}
	kernel := make([]float64, 2*rad+1)
	twoSigmaSq := 2 * sigma * sigma
	var sum float64
	for i := -rad; i <= rad; i++ {
		w := math.Exp(-float64(i*i) / twoSigmaSq)
		kernel[i+rad] = w
		sum += w
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	return kernel
}

// gaussianBlurInto runs a horizontal then a vertical 1D convolution, using a
// pooled scratch frame for the intermediate. It works for any bytes-per-pixel,
// so the same code blurs RGB and the Gray8 mask.
func gaussianBlurInto(dst, src *clip.Frame, kernel []float64, pool *clip.FramePool) error {
	tmp := pool.Get(src.W, src.H, src.Format)
	defer tmp.Release()
	rad := len(kernel) / 2
	bpp := src.Format.BytesPerPixel()
	convolve1D(tmp, src, kernel, rad, bpp, true)
	convolve1D(dst, tmp, kernel, rad, bpp, false)
	return nil
}

// convolve1D convolves src into dst along one axis (horizontal when horiz is
// true, else vertical). Samples past an edge clamp to the edge pixel.
func convolve1D(dst, src *clip.Frame, kernel []float64, rad, bpp int, horiz bool) {
	limX, limY := src.W-1, src.H-1
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			do := y*dst.Stride + x*bpp
			for c := 0; c < bpp; c++ {
				var acc float64
				for k := -rad; k <= rad; k++ {
					sx, sy := x, y
					if horiz {
						sx = clampInt(x+k, 0, limX)
					} else {
						sy = clampInt(y+k, 0, limY)
					}
					acc += float64(src.Pix[sy*src.Stride+sx*bpp+c]) * kernel[k+rad]
				}
				dst.Pix[do+c] = clampByte(int(acc + 0.5))
			}
		}
	}
}

// motionBlurInto averages samples taken along the (cos, sin) direction across
// distance pixels, centered on each pixel. It works for any bytes-per-pixel.
func motionBlurInto(dst, src *clip.Frame, cos, sin, distance float64) {
	taps := int(distance + 0.5)
	if taps < 1 {
		taps = 1
	}
	half := distance / 2
	step := distance / float64(taps)
	bpp := src.Format.BytesPerPixel()
	limX, limY := src.W-1, src.H-1
	acc := make([]float64, bpp)
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			for c := range acc {
				acc[c] = 0
			}
			for i := 0; i <= taps; i++ {
				off := float64(i)*step - half
				sx := clampInt(int(float64(x)+cos*off+0.5), 0, limX)
				sy := clampInt(int(float64(y)+sin*off+0.5), 0, limY)
				so := sy*src.Stride + sx*bpp
				for c := 0; c < bpp; c++ {
					acc[c] += float64(src.Pix[so+c])
				}
			}
			do := y*dst.Stride + x*bpp
			n := float64(taps + 1)
			for c := 0; c < bpp; c++ {
				dst.Pix[do+c] = clampByte(int(acc[c]/n + 0.5))
			}
		}
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
