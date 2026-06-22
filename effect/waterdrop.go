package effect

import (
	"math"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

const (
	defaultWaterDropDur        = 2 * time.Second
	defaultWaterDropAmplitude  = 8.0
	defaultWaterDropWavelength = 24.0
	defaultWaterDropSpeed      = 180.0
	defaultWaterDropShade      = 22.0
)

// WaterDrop ripples the clip from a configurable point, like a drop landing on
// the video surface. The RGB uses the source video as the background, radially
// displacing pixels and adding a small moving highlight/shadow so the circular
// waves remain visible on low-texture footage. The mask is displaced with the
// same wave map.
//
// All distance fields are in pixels. Zero values choose useful defaults, so
// WaterDrop{} is a visible two-second center ripple starting at t=0.
type WaterDrop struct {
	// Start is when the drop lands on the clip timeline.
	Start clip.Time
	// Dur is the active ripple duration. A zero or negative duration uses the
	// default two-second ripple.
	Dur clip.Time
	// Amplitude is the maximum radial displacement in pixels.
	Amplitude float64
	// Wavelength is the distance between wave rings in pixels.
	Wavelength float64
	// Speed is the outward wave speed in pixels per second.
	Speed float64
	// Damping controls how quickly trailing waves fade with distance.
	Damping float64
	// X and Y are the drop landing point in pixels. Non-zero coordinates are used
	// automatically; otherwise WaterDrop{} keeps its centered default.
	X, Y float64
	// UseOrigin makes X and Y explicit even when both coordinates are zero.
	UseOrigin bool
}

func (w WaterDrop) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (w WaterDrop) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	cfg := w.config(c.Size())
	rgbFn := func(t clip.Time, dst, src *clip.Frame) error {
		return waterDropInto(dst, src, t, cfg, true)
	}
	maskFn := func(t clip.Time, dst, src *clip.Frame) error {
		return waterDropInto(dst, src, t, cfg, false)
	}
	return video.NewTransform(c, c.Size(), rgbFn, maskFn), nil
}

type waterDropConfig struct {
	start      clip.Time
	dur        clip.Time
	amplitude  float64
	wavelength float64
	speed      float64
	damping    float64
	cx, cy     float64
}

func (w WaterDrop) config(size clip.Size) waterDropConfig {
	dur := w.Dur
	if dur <= 0 {
		dur = defaultWaterDropDur
	}
	amplitude := w.Amplitude
	if amplitude == 0 {
		amplitude = defaultWaterDropAmplitude
	}
	wavelength := w.Wavelength
	if wavelength <= 0 {
		wavelength = defaultWaterDropWavelength
	}
	speed := w.Speed
	if speed <= 0 {
		speed = defaultWaterDropSpeed
	}
	damping := w.Damping
	if damping <= 0 {
		damping = wavelength * 3
	}
	cx, cy := float64(size.W-1)/2, float64(size.H-1)/2
	if w.UseOrigin || w.X != 0 || w.Y != 0 {
		cx, cy = w.X, w.Y
	}
	return waterDropConfig{
		start:      w.Start,
		dur:        dur,
		amplitude:  amplitude,
		wavelength: wavelength,
		speed:      speed,
		damping:    damping,
		cx:         cx,
		cy:         cy,
	}
}

func waterDropInto(dst, src *clip.Frame, t clip.Time, cfg waterDropConfig, shade bool) error {
	if dst.Format != src.Format || dst.W != src.W || dst.H != src.H {
		return clip.Wrap("water drop", clip.ErrVideoCorrupted)
	}
	if t < cfg.start || t >= cfg.start+cfg.dur {
		copyFrame(dst, src)
		return nil
	}

	elapsed := t - cfg.start
	progress := float64(elapsed) / float64(cfg.dur)
	envelope := 1 - progress
	front := cfg.speed * elapsed.Seconds()
	phaseScale := 2 * math.Pi / cfg.wavelength
	bpp := src.Format.BytesPerPixel()

	for y := 0; y < dst.H; y++ {
		dy := float64(y) - cfg.cy
		for x := 0; x < dst.W; x++ {
			dx := float64(x) - cfg.cx
			r := math.Hypot(dx, dy)
			wave := waterDropWave(r, front, cfg, phaseScale)
			displace := cfg.amplitude * wave * envelope
			sx, sy := float64(x), float64(y)
			if r > 0 {
				sx += dx / r * displace
				sy += dy / r * displace
			}

			do := y*dst.Stride + x*bpp
			sampleFrame(dst.Pix[do:do+bpp], src, sx, sy)
			if shade && bpp >= 3 {
				addRGBShade(dst.Pix[do:do+bpp], wave*envelope*defaultWaterDropShade)
			}
		}
	}
	return nil
}

func waterDropWave(r, front float64, cfg waterDropConfig, phaseScale float64) float64 {
	waveAge := front - r
	if waveAge < -cfg.wavelength {
		return 0
	}
	trailing := waveAge
	if trailing < 0 {
		trailing = 0
	}
	return math.Sin(waveAge*phaseScale) * math.Exp(-trailing/cfg.damping)
}

func sampleFrame(dst []byte, src *clip.Frame, x, y float64) {
	x = clampFloat(x, 0, float64(src.W-1))
	y = clampFloat(y, 0, float64(src.H-1))
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := x0+1, y0+1
	if x1 >= src.W {
		x1 = src.W - 1
	}
	if y1 >= src.H {
		y1 = src.H - 1
	}
	tx, ty := x-float64(x0), y-float64(y0)
	w00, w10 := (1-tx)*(1-ty), tx*(1-ty)
	w01, w11 := (1-tx)*ty, tx*ty
	bpp := src.Format.BytesPerPixel()
	o00 := y0*src.Stride + x0*bpp
	o10 := y0*src.Stride + x1*bpp
	o01 := y1*src.Stride + x0*bpp
	o11 := y1*src.Stride + x1*bpp
	for c := 0; c < bpp; c++ {
		v := float64(src.Pix[o00+c])*w00 +
			float64(src.Pix[o10+c])*w10 +
			float64(src.Pix[o01+c])*w01 +
			float64(src.Pix[o11+c])*w11
		dst[c] = byte(v + 0.5)
	}
}

func addRGBShade(px []byte, shade float64) {
	for i := 0; i < 3; i++ {
		v := int(px[i]) + int(shade)
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		px[i] = byte(v)
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func copyFrame(dst, src *clip.Frame) {
	bpp := src.Format.BytesPerPixel()
	rowBytes := src.W * bpp
	for y := 0; y < src.H; y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+rowBytes], src.Pix[y*src.Stride:y*src.Stride+rowBytes])
	}
}
