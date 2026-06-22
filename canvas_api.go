package mgo

import (
	"image/color"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/draw"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// Paint describes how a shape is colored (fill, stroke, stroke width). See
// draw.Paint.
type Paint = draw.Paint

// Shape is a custom drawing primitive. Implement it and draw it with
// Canvas.Draw to extend the canvas with new shapes. See draw.Shape.
type Shape = draw.Shape

// Canvas builds a still vector graphic — rectangles, circles, ellipses, lines,
// or custom shapes — and turns it into a clip. Drawing methods return the canvas
// so calls chain; WithDuration finalizes it into a *Video:
//
//	logo := mgo.NewCanvas(640, 360).
//		Background(color.Black).
//		Circle(320, 180, 80, mgo.Paint{Fill: color.White}).
//		Line(0, 180, 640, 180, color.White, 4).
//		WithDuration(3 * time.Second)
type Canvas struct {
	c *draw.Canvas
}

// NewCanvas creates a w×h transparent drawing surface.
func NewCanvas(w, h int) *Canvas {
	return &Canvas{c: draw.New(w, h)}
}

// Background sets a solid fill behind every shape. An opaque background makes
// the resulting clip opaque (no alpha mask).
func (c *Canvas) Background(col color.Color) *Canvas {
	c.c.Background(col)
	return c
}

// Rect draws an axis-aligned rectangle with top-left (x, y) and size (w, h).
func (c *Canvas) Rect(x, y, w, h int, p Paint) *Canvas {
	c.c.Rect(x, y, w, h, p)
	return c
}

// Circle draws a circle of radius r centered at (cx, cy).
func (c *Canvas) Circle(cx, cy, r int, p Paint) *Canvas {
	c.c.Circle(cx, cy, r, p)
	return c
}

// Ellipse draws an ellipse of radii (rx, ry) centered at (cx, cy).
func (c *Canvas) Ellipse(cx, cy, rx, ry int, p Paint) *Canvas {
	c.c.Ellipse(cx, cy, rx, ry, p)
	return c
}

// Line draws a round-capped segment of the given width from (x1, y1) to
// (x2, y2) in col.
func (c *Canvas) Line(x1, y1, x2, y2 int, col color.Color, width float64) *Canvas {
	c.c.Line(x1, y1, x2, y2, col, width)
	return c
}

// Draw appends a custom Shape painted with p, the extension point for shapes
// beyond the built-in primitives.
func (c *Canvas) Draw(s Shape, p Paint) *Canvas {
	c.c.Draw(s, p)
	return c
}

// Image rasterizes the canvas into a still clip with no duration yet. Give it
// one with WithDuration, or use the canvas WithDuration shortcut.
func (c *Canvas) Image() *Video {
	node, err := canvasNode(c.c)
	if err != nil {
		return &Video{err: err}
	}
	return &Video{inner: node}
}

// WithDuration rasterizes the canvas and returns a clip of duration d.
func (c *Canvas) WithDuration(d Time) *Video {
	return c.Image().WithDuration(d)
}

// canvasNode rasterizes a draw.Canvas into a static image node, splitting any
// alpha into a mask sidecar so a transparent graphic composites correctly.
func canvasNode(c *draw.Canvas) (video.VideoClip, error) {
	f := c.Render()
	if f.Format == clip.RGBA {
		rgb, alpha, err := imagex.SplitAlpha(f)
		if err != nil {
			return nil, err
		}
		return video.NewImage(rgb, alpha), nil
	}
	return video.NewImage(f, nil), nil
}
