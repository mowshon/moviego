package effect

import (
	"math"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// Rotate turns a clip clockwise by Degrees, an arbitrary angle. Exact multiples
// of 90 take the lossless transpose fast path (the v1 behavior). Other angles
// sample with bilinear interpolation: when Expand is true the output canvas
// grows to the rotated bounding box so no corner is clipped; otherwise it keeps
// the input size and the corners rotate out of frame (matching FFmpeg's
// rotate=...:ow:oh). The exposed margins are black (RGB) / transparent (mask),
// and the mask is rotated identically.
type Rotate struct {
	Degrees float64
	Expand  bool
}

func (r Rotate) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (r Rotate) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if q := r.Degrees / 90; q == math.Trunc(q) {
		return r.applyQuarter(c, int(q))
	}
	return r.applyArbitrary(c)
}

// applyQuarter rotates by an exact quarter-turn via the lossless transpose. A
// 90- or 270-degree turn swaps width and height.
func (r Rotate) applyQuarter(c video.VideoClip, q int) (video.VideoClip, error) {
	quarter := ((q % 4) + 4) % 4
	in := c.Size()
	out := in
	if quarter == 1 || quarter == 3 {
		out = clip.Size{W: in.H, H: in.W}
	}
	fn := func(dst, src *clip.Frame) error { return imagex.RotateInto(dst, src, quarter) }
	// Rotation is intentionally not advertised as a filter in v1 fusion: the Go
	// transpose is exact, and matching FFmpeg's transpose direction across
	// rotated-metadata inputs needs its own parity gate. A rotated graph falls
	// back to the Go path (correct, just not fused).
	return geometry(c, out, fn, fn, nil)
}

// applyArbitrary rotates by a non-quarter angle with bilinear sampling, growing
// the canvas when Expand is set.
func (r Rotate) applyArbitrary(c video.VideoClip) (video.VideoClip, error) {
	in := c.Size()
	angle := r.Degrees * math.Pi / 180
	out := in
	if r.Expand {
		out = expandedBounds(in, angle)
	}
	fn := func(dst, src *clip.Frame) error { return imagex.RotateAngleInto(dst, src, angle, [3]byte{}) }
	filterFn := func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
		fin := ctx.Inputs[0]
		return ffmpeg.FilterFragment{
			Lines: []string{ffmpeg.RotateAngle(fin.Label, ctx.OutLabel, angle, out.W, out.H, [3]byte{})},
			Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: fin.Rate, PixFmt: fin.PixFmt},
		}, true
	}
	return geometry(c, out, fn, fn, filterFn)
}

// expandedBounds returns the smallest canvas that holds a size rotated by angle
// radians. Each dimension is rounded up (ceil) so no rotated corner is clipped,
// then up to the next even number: chroma-subsampled encoders (yuv420p) reject
// odd dimensions, and the fused FFmpeg path is handed these same ow/oh, so both
// engines agree.
func expandedBounds(in clip.Size, angle float64) clip.Size {
	sin, cos := math.Sincos(angle)
	w := math.Abs(float64(in.W)*cos) + math.Abs(float64(in.H)*sin)
	h := math.Abs(float64(in.W)*sin) + math.Abs(float64(in.H)*cos)
	return clip.Size{W: evenCeil(w), H: evenCeil(h)}
}

// evenCeil rounds v up to the nearest even integer, with a floor of 2.
func evenCeil(v float64) int {
	n := int(math.Ceil(v))
	if n < 2 {
		return 2
	}
	if n%2 != 0 {
		n++
	}
	return n
}
