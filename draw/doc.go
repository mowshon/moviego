// Package draw rasterizes simple 2D vector shapes (rectangles, circles,
// ellipses, lines) into a clip.Frame, the source for moviego's drawing and
// motion-graphics clips.
//
// It is a thin assembler over golang.org/x/image/vector — the same antialiased
// fill rasterizer x/image uses for glyphs — so it adds no new dependency. A
// Canvas collects drawing operations and Render flattens them into a single
// RGBA frame; the facade wraps that frame in a static image clip.
//
// # Extending with new shapes
//
// Every primitive is a Shape: a value that knows how to append its filled
// interior and its stroked outline to a rasterizer. Adding a new primitive
// (a rounded rectangle, a polygon, a star) is a matter of implementing the two
// Shape methods and nothing else — the Canvas, paint model, and frame plumbing
// are unchanged. Draw a custom Shape through Canvas.Draw:
//
//	c := draw.New(640, 360)
//	c.Draw(myStar, draw.Paint{Fill: color.RGBA{R: 255, A: 255}})
//	frame := c.Render()
//
// Fills and strokes are both expressed as filled paths (a stroke is the band
// between an outer and a reversed inner outline), so the rasterizer only ever
// fills — there is no separate stroking engine to maintain.
package draw
