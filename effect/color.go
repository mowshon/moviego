package effect

import (
	"math"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// Brightness shifts every channel by Delta, a normalized offset in [-1, 1]
// (0.1 brightens by ~10% of full range). A zero Delta is a passthrough. It is a
// per-channel lookup, so the per-frame cost is three table reads per pixel, and
// it advertises FFmpeg's eq for fusion.
type Brightness struct {
	Delta float64
	// Start delays the effect until then; the zero value applies from the start.
	Start clip.Time
}

func (b Brightness) Targets() EffectTargets { return EffectTargets{Video: true} }

func (b Brightness) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if b.Delta == 0 {
		return c, nil
	}
	lut := uniformLUT(func(v float64) float64 { return v + b.Delta })
	filter := unaryFilter(func(in, out string) string { return ffmpeg.EqColor(in, out, b.Delta, 1, 1, 1) })
	return lutEffect("brightness", lut, b.Start, filter).ApplyVideo(c)
}

// Contrast scales each channel around mid-gray by Amount (1.0 = unchanged, >1
// increases contrast, <1 flattens it). A zero or negative Amount is a
// passthrough. Per-channel lookup; advertises FFmpeg's eq.
type Contrast struct {
	Amount float64
	Start  clip.Time
}

func (cc Contrast) Targets() EffectTargets { return EffectTargets{Video: true} }

func (cc Contrast) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if cc.Amount <= 0 {
		return c, nil
	}
	lut := uniformLUT(func(v float64) float64 { return (v-0.5)*cc.Amount + 0.5 })
	filter := unaryFilter(func(in, out string) string { return ffmpeg.EqColor(in, out, 0, cc.Amount, 1, 1) })
	return lutEffect("contrast", lut, cc.Start, filter).ApplyVideo(c)
}

// Gamma applies a power curve: output = input^(1/Value). Value > 1 brightens
// midtones, < 1 darkens them; 1.0 (and any non-positive Value) is a passthrough.
// Per-channel lookup; advertises FFmpeg's eq.
type Gamma struct {
	Value float64
	Start clip.Time
}

func (g Gamma) Targets() EffectTargets { return EffectTargets{Video: true} }

func (g Gamma) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if g.Value <= 0 || g.Value == 1 {
		return c, nil
	}
	inv := 1 / g.Value
	lut := uniformLUT(func(v float64) float64 { return math.Pow(v, inv) })
	filter := unaryFilter(func(in, out string) string { return ffmpeg.EqColor(in, out, 0, 1, 1, g.Value) })
	return lutEffect("gamma", lut, g.Start, filter).ApplyVideo(c)
}

// Invert produces a photographic negative (output = 1 - input) on every channel.
// It is a per-channel lookup with no faithful single-filter form here, so it
// renders on the Go path.
type Invert struct {
	Start clip.Time
}

func (i Invert) Targets() EffectTargets { return EffectTargets{Video: true} }

func (i Invert) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	lut := uniformLUT(func(v float64) float64 { return 1 - v })
	return lutEffect("invert", lut, i.Start, nil).ApplyVideo(c)
}

// Saturation scales each pixel's distance from its luma by Amount (1.0 =
// unchanged, <1 desaturates, >1 more vivid). The zero value is a passthrough,
// not gray — and so is any negative Amount — so for full desaturation use
// Grayscale rather than Amount 0. Unlike the per-channel ops it mixes channels
// through luma, so it is a per-pixel function rather than a lookup; it
// advertises FFmpeg's eq.
type Saturation struct {
	Amount float64
	Start  clip.Time
}

func (s Saturation) Targets() EffectTargets { return EffectTargets{Video: true} }

func (s Saturation) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if s.Amount <= 0 {
		return c, nil
	}
	fn := func(dst, src *clip.Frame) error { saturateInto(dst, src, s.Amount); return nil }
	filter := unaryFilter(func(in, out string) string { return ffmpeg.EqColor(in, out, 0, 1, s.Amount, 1) })
	return videoPixelFn("saturation", fn, s.Start, filter).ApplyVideo(c)
}

// ColorBalance shifts the red/green/blue mix independently in the Shadows, Mids
// and Highlights tonal ranges; each is an [r, g, b] adjustment in [-1, 1]. The
// per-channel shift is a function of that channel's own value (a triangular
// weighting across the three ranges), so it precomputes one lookup table per
// channel. An all-zero config is a passthrough; advertises FFmpeg's colorbalance.
type ColorBalance struct {
	Shadows    [3]float64
	Mids       [3]float64
	Highlights [3]float64
	Start      clip.Time
}

func (cb ColorBalance) Targets() EffectTargets { return EffectTargets{Video: true} }

func (cb ColorBalance) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if cb.Shadows == [3]float64{} && cb.Mids == [3]float64{} && cb.Highlights == [3]float64{} {
		return c, nil
	}
	var lut rgbLUT
	for ch := 0; ch < 3; ch++ {
		s, m, h := cb.Shadows[ch], cb.Mids[ch], cb.Highlights[ch]
		lut[ch] = channelLUT(func(v float64) float64 {
			ws, wm, wh := tonalWeights(v)
			return v + s*ws + m*wm + h*wh
		})
	}
	filter := unaryFilter(func(in, out string) string {
		return ffmpeg.ColorBalance(in, out, cb.Shadows, cb.Mids, cb.Highlights)
	})
	return lutEffect("color-balance", lut, cb.Start, filter).ApplyVideo(c)
}

// HSL rotates hue by Hue degrees, scales saturation by (1 + Sat) and shifts
// lightness by Light, all in HSL space. The zero value is an identity. It mixes
// channels per pixel; it advertises FFmpeg's hue for the hue+saturation part and
// stays Go-only when Light is set (hue has no matching lightness term).
type HSL struct {
	Hue   float64 // degrees
	Sat   float64 // saturation delta (0 = unchanged)
	Light float64 // lightness delta in [-1, 1]
	Start clip.Time
}

func (h HSL) Targets() EffectTargets { return EffectTargets{Video: true} }

func (h HSL) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if h.Hue == 0 && h.Sat == 0 && h.Light == 0 {
		return c, nil
	}
	fn := func(dst, src *clip.Frame) error { hslInto(dst, src, h.Hue, 1+h.Sat, h.Light); return nil }
	var filter func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)
	if h.Light == 0 {
		filter = unaryFilter(func(in, out string) string { return ffmpeg.Hue(in, out, h.Hue, 1+h.Sat) })
	}
	return videoPixelFn("hsl", fn, h.Start, filter).ApplyVideo(c)
}

// Grayscale converts RGB frames to luma, the canonical name for the v1
// BlackAndWhite effect; it reuses the same conversion.
type Grayscale struct {
	Start clip.Time
}

func (g Grayscale) Targets() EffectTargets { return EffectTargets{Video: true} }

func (g Grayscale) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	return BlackAndWhite{Start: g.Start}.ApplyVideo(c)
}

// lumaR/G/B are the Rec.601 luma weights, matching blackAndWhiteInto.
const lumaR, lumaG, lumaB = 0.299, 0.587, 0.114

// saturateInto blends each pixel toward its luma by amount (amount 1 leaves it
// unchanged, 0 collapses to gray).
func saturateInto(dst, src *clip.Frame, amount float64) {
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			i := x * 3
			r, g, b := float64(s[i]), float64(s[i+1]), float64(s[i+2])
			luma := lumaR*r + lumaG*g + lumaB*b
			d[i] = clampByte(int(luma + amount*(r-luma) + 0.5))
			d[i+1] = clampByte(int(luma + amount*(g-luma) + 0.5))
			d[i+2] = clampByte(int(luma + amount*(b-luma) + 0.5))
		}
	}
}

// hslInto rotates hue by hueDeg degrees, multiplies saturation by satMul and
// adds lightDelta to lightness, converting through HSL per pixel.
func hslInto(dst, src *clip.Frame, hueDeg, satMul, lightDelta float64) {
	hueShift := hueDeg / 360
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			i := x * 3
			hh, ss, ll := rgbToHSL(float64(s[i])/255, float64(s[i+1])/255, float64(s[i+2])/255)
			hh = math.Mod(hh+hueShift, 1)
			if hh < 0 {
				hh++
			}
			ss = clamp01(ss * satMul)
			ll = clamp01(ll + lightDelta)
			r, g, b := hslToRGB(hh, ss, ll)
			d[i] = clampByte(int(r*255 + 0.5))
			d[i+1] = clampByte(int(g*255 + 0.5))
			d[i+2] = clampByte(int(b*255 + 0.5))
		}
	}
}

// tonalWeights returns the shadow/mid/highlight weights for a normalized value:
// triangular ramps that peak at 0, 0.5 and 1 and sum to 1 across the range.
func tonalWeights(v float64) (shadow, mid, highlight float64) {
	shadow = math.Max(0, 1-2*v)
	highlight = math.Max(0, 2*v-1)
	mid = 1 - math.Abs(2*v-1)
	return shadow, mid, highlight
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	maxc := math.Max(r, math.Max(g, b))
	minc := math.Min(r, math.Min(g, b))
	l = (maxc + minc) / 2
	if maxc == minc {
		return 0, 0, l // achromatic
	}
	d := maxc - minc
	if l > 0.5 {
		s = d / (2 - maxc - minc)
	} else {
		s = d / (maxc + minc)
	}
	switch maxc {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	if s == 0 {
		return l, l, l // achromatic
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return hueToRGB(p, q, h+1.0/3), hueToRGB(p, q, h), hueToRGB(p, q, h-1.0/3)
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 1.0/2:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	default:
		return p
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
