package transition

import (
	"math"

	"github.com/mowshon/moviego/v2/clip"
)

// Iris reveals B through an expanding aperture (Out false, the zero value: B
// grows from the center) or hides A through a contracting one (Out true: B is
// revealed from the edges inward). Center is the aperture center as a fraction
// of the frame; its zero value (0,0) is treated as the center (0.5,0.5), so a
// true corner-anchored iris needs a tiny offset (e.g. CenterX: 0.001). The alpha
// sidecar uses the identical aperture. The aperture is scaled so the endpoint
// contract holds for any center: at p == 1 it covers the whole frame.
type Iris struct {
	Shape   Shape
	Out     bool
	CenterX float64
	CenterY float64
}

var (
	_ Transition     = Iris{}
	_ MaskTransition = Iris{}
)

func (ir Iris) Name() string {
	if ir.Out {
		return "iris-out-" + ir.Shape.String()
	}
	return "iris-in-" + ir.Shape.String()
}

func (ir Iris) Frame(p float64, dst, a, b *clip.Frame)     { ir.eval(p, dst, a, b, 3) }
func (ir Iris) FrameMask(p float64, dst, a, b *clip.Frame) { ir.eval(p, dst, a, b, 1) }

// eval selects A or B per pixel by the shape aperture, copying bpp bytes. Shared
// by Frame (RGB) and FrameMask (alpha) so the aperture matches exactly.
func (ir Iris) eval(p float64, dst, a, b *clip.Frame, bpp int) {
	cx, cy := ir.CenterX, ir.CenterY
	if cx == 0 && cy == 0 {
		cx, cy = 0.5, 0.5
	}
	// Endpoints are exact regardless of frame parity or center: at p<=0 the
	// aperture is empty (fully A), at p>=1 it covers the frame (fully B). Handling
	// them here avoids an off-by-half at the center pixel, where the metric is 0
	// and m<=0 would otherwise leak B into a p==0 frame on odd-sized frames.
	if p <= 0 {
		copyAllPixels(dst, a)
		return
	}
	if p >= 1 {
		copyAllPixels(dst, b)
		return
	}
	pcx, pcy := cx*float64(dst.W), cy*float64(dst.H)
	hw, hh := float64(dst.W)/2, float64(dst.H)/2
	if hw == 0 || hh == 0 {
		return
	}
	// maxm is the aperture metric at the farthest frame corner from this center,
	// so p == 1 (in) / p == 0 (out) reaches every pixel regardless of center.
	maxm := irisCornerMax(ir.Shape, pcx, pcy, hw, hh, dst.W, dst.H)
	for y := 0; y < dst.H; y++ {
		drow := dst.Pix[y*dst.Stride:]
		arow := a.Pix[y*a.Stride:]
		brow := b.Pix[y*b.Stride:]
		ny := (float64(y) + 0.5 - pcy) / hh
		for x := 0; x < dst.W; x++ {
			nx := (float64(x) + 0.5 - pcx) / hw
			m := irisMetric(ir.Shape, nx, ny)
			var showB bool
			if ir.Out {
				showB = m >= (1-p)*maxm
			} else {
				showB = m <= p*maxm
			}
			src := arow
			if showB {
				src = brow
			}
			o := x * bpp
			copy(drow[o:o+bpp], src[o:o+bpp])
		}
	}
}

// irisMetric returns the distance of a normalized point (nx, ny) from the center
// under the shape's metric.
func irisMetric(s Shape, nx, ny float64) float64 {
	switch s {
	case Diamond:
		return math.Abs(nx) + math.Abs(ny)
	case Rectangle:
		return math.Max(math.Abs(nx), math.Abs(ny))
	default: // Circle
		return math.Hypot(nx, ny)
	}
}

// irisCornerMax returns the largest shape metric among the four frame corners,
// measured from the configured center (pcx, pcy) in the same normalized units as
// irisMetric. Using the geometric corners (0/W, 0/H), which lie just outside the
// outermost sampled pixel centers, keeps the aperture a hair larger than any
// sampled metric so the endpoints stay clean.
func irisCornerMax(s Shape, pcx, pcy, hw, hh float64, w, h int) float64 {
	max := 0.0
	for _, xp := range []float64{0, float64(w)} {
		for _, yp := range []float64{0, float64(h)} {
			nx := (xp - pcx) / hw
			ny := (yp - pcy) / hh
			if m := irisMetric(s, nx, ny); m > max {
				max = m
			}
		}
	}
	return max
}
