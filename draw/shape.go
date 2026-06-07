package draw

import (
	"math"

	"golang.org/x/image/vector"
)

// kappa is the control-point distance that makes four cubic Béziers approximate
// a circle: 4/3 * (√2 - 1). It is how Circle and Ellipse trace crisp arcs at any
// size instead of faceting like a polyline.
const kappa = 0.5522847498307936

// Shape is a fillable, strokable 2D primitive. Implement it to add a new
// drawing primitive: Fill appends the solid interior as a path, and Outline
// appends the stroke band (the ring between the outer and inner edges) for a
// stroke of the given width. Both append to the rasterizer in its pixel
// coordinate space; the Canvas fills each with the appropriate paint color.
//
// A primitive that has no interior (a Line) appends nothing in Fill; one that is
// always solid may append the same region in both.
type Shape interface {
	Fill(rz *vector.Rasterizer)
	Outline(rz *vector.Rasterizer, width float64)
}

// Rect is an axis-aligned rectangle with its top-left at (X, Y).
type Rect struct {
	X, Y, W, H float64
}

// Fill traces the rectangle clockwise.
func (r Rect) Fill(rz *vector.Rasterizer) {
	traceRect(rz, r.X, r.Y, r.W, r.H, false)
}

// Outline traces an outer rectangle grown by width/2 and an inner one shrunk by
// width/2 wound the opposite way, so the rasterizer's winding leaves a ring.
func (r Rect) Outline(rz *vector.Rasterizer, width float64) {
	h := width / 2
	traceRect(rz, r.X-h, r.Y-h, r.W+width, r.H+width, false)
	traceRect(rz, r.X+h, r.Y+h, r.W-width, r.H-width, true)
}

// traceRect appends a rectangle path; reverse winds it counter-clockwise so it
// subtracts from an enclosing path (the hole of a ring).
func traceRect(rz *vector.Rasterizer, x, y, w, h float64, reverse bool) {
	if w <= 0 || h <= 0 {
		return
	}
	f := func(px, py float64) { rz.LineTo(float32(px), float32(py)) }
	rz.MoveTo(float32(x), float32(y))
	if reverse {
		f(x, y+h)
		f(x+w, y+h)
		f(x+w, y)
	} else {
		f(x+w, y)
		f(x+w, y+h)
		f(x, y+h)
	}
	rz.ClosePath()
}

// Ellipse is an axis-aligned ellipse centered at (CX, CY) with radii RX, RY.
type Ellipse struct {
	CX, CY, RX, RY float64
}

// Circle is an Ellipse with equal radii.
type Circle struct {
	CX, CY, R float64
}

func (c Circle) Fill(rz *vector.Rasterizer) {
	Ellipse{CX: c.CX, CY: c.CY, RX: c.R, RY: c.R}.Fill(rz)
}

func (c Circle) Outline(rz *vector.Rasterizer, width float64) {
	Ellipse{CX: c.CX, CY: c.CY, RX: c.R, RY: c.R}.Outline(rz, width)
}

func (e Ellipse) Fill(rz *vector.Rasterizer) {
	traceEllipse(rz, e.CX, e.CY, e.RX, e.RY, false)
}

// Outline draws the ring between an outer and an inner ellipse, the inner one
// wound the opposite way so it cuts a hole.
func (e Ellipse) Outline(rz *vector.Rasterizer, width float64) {
	h := width / 2
	traceEllipse(rz, e.CX, e.CY, e.RX+h, e.RY+h, false)
	traceEllipse(rz, e.CX, e.CY, e.RX-h, e.RY-h, true)
}

// traceEllipse appends a four-segment cubic-Bézier ellipse. reverse swaps the
// winding direction so the path subtracts (the inner edge of a ring).
func traceEllipse(rz *vector.Rasterizer, cx, cy, rx, ry float64, reverse bool) {
	if rx <= 0 || ry <= 0 {
		return
	}
	ox, oy := rx*kappa, ry*kappa
	rz.MoveTo(float32(cx+rx), float32(cy))
	cube := func(c1x, c1y, c2x, c2y, ex, ey float64) {
		rz.CubeTo(float32(c1x), float32(c1y), float32(c2x), float32(c2y), float32(ex), float32(ey))
	}
	if reverse {
		cube(cx+rx, cy-oy, cx+ox, cy-ry, cx, cy-ry)
		cube(cx-ox, cy-ry, cx-rx, cy-oy, cx-rx, cy)
		cube(cx-rx, cy+oy, cx-ox, cy+ry, cx, cy+ry)
		cube(cx+ox, cy+ry, cx+rx, cy+oy, cx+rx, cy)
	} else {
		cube(cx+rx, cy+oy, cx+ox, cy+ry, cx, cy+ry)
		cube(cx-ox, cy+ry, cx-rx, cy+oy, cx-rx, cy)
		cube(cx-rx, cy-oy, cx-ox, cy-ry, cx, cy-ry)
		cube(cx+ox, cy-ry, cx+rx, cy-oy, cx+rx, cy)
	}
	rz.ClosePath()
}

// Line is a straight segment from (X1, Y1) to (X2, Y2). It has no interior: its
// thickness comes entirely from Outline, which traces a round-capped capsule of
// the requested width.
type Line struct {
	X1, Y1, X2, Y2 float64
}

// Fill appends nothing: a line is a stroke-only primitive.
func (Line) Fill(*vector.Rasterizer) {}

// Outline traces the capsule (a width-thick quad along the segment with
// semicircular round caps at each end) as a single filled path.
func (l Line) Outline(rz *vector.Rasterizer, width float64) {
	if width <= 0 {
		return
	}
	dx, dy := l.X2-l.X1, l.Y2-l.Y1
	length := math.Hypot(dx, dy)
	r := width / 2
	if length == 0 { // degenerate segment: a dot
		traceEllipse(rz, l.X1, l.Y1, r, r, false)
		return
	}
	ux, uy := dx/length, dy/length // unit direction
	nx, ny := -uy*r, ux*r          // left normal scaled to the half-width

	rz.MoveTo(float32(l.X1+nx), float32(l.Y1+ny))
	rz.LineTo(float32(l.X2+nx), float32(l.Y2+ny))
	traceCap(rz, l.X2, l.Y2, nx, ny) // end cap: +normal → -normal
	rz.LineTo(float32(l.X1-nx), float32(l.Y1-ny))
	traceCap(rz, l.X1, l.Y1, -nx, -ny) // start cap: -normal → +normal
	rz.ClosePath()
}

// capSegments is how many line segments approximate each round cap; a half-turn
// in 12 steps reads as smooth at any reasonable stroke width.
const capSegments = 12

// traceCap appends a semicircular arc around (cx, cy), sweeping the offset
// vector (ox, oy) — a point on the rim — through 180° to its opposite side, so
// the cap closes the capsule outline.
func traceCap(rz *vector.Rasterizer, cx, cy, ox, oy float64) {
	start := math.Atan2(oy, ox)
	for i := 1; i <= capSegments; i++ {
		a := start + math.Pi*float64(i)/capSegments
		rz.LineTo(float32(cx+math.Cos(a)*math.Hypot(ox, oy)), float32(cy+math.Sin(a)*math.Hypot(ox, oy)))
	}
}
