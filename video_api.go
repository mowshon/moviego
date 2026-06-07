package mgo

import (
	"context"
	"fmt"

	"github.com/moviego/moviego/composite"
	"github.com/moviego/moviego/effect"
	"github.com/moviego/moviego/video"
)

// Video is the facade handle for a video clip. Its methods build new graph
// nodes and return new handles; the underlying clip is never mutated. A build
// error (e.g. an effect that needs a known duration) is carried on the handle
// and surfaced by Err and by WriteVideo, so calls can be chained fluently.
//
// pos and layer are placement metadata consumed only when the handle is passed
// to Composite; they have no effect on a standalone export.
type Video struct {
	inner video.VideoClip
	err   error
	pos   composite.Position
	layer int
}

// OpenVideo opens a video file, probing it for metadata. The decoder is created
// lazily at export time. The returned Video owns the file: close it (not a
// derived clip) when done.
func OpenVideo(path string) (*Video, error) {
	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// Image opens a still image as a clip. A transparent image carries a mask.
// Give it a duration with WithDuration and a rate via ExportOptions to export.
func Image(path string) (*Video, error) {
	n, err := video.OpenImage(path)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// Color builds a solid-color clip of the given size. Give it a duration with
// WithDuration and a rate via ExportOptions to export.
func Color(w, h int, col [3]byte) *Video {
	return &Video{inner: video.NewColor(Size{W: w, H: h}, col)}
}

// ImageSequence builds a clip that plays the given image files in order, each
// shown for 1/fps seconds.
func ImageSequence(paths []string, fps Rate) (*Video, error) {
	n, err := video.NewImageSequence(paths, video.ImageSequenceOptions{FPS: fps})
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// Subclip trims the clip to the source window [start, end). The end is
// optional: with only a start, the window runs to the end of the media. A
// negative end counts back from the media duration. See video.Subclip for the
// boundary rules. The returned handle does not own the source, so closing it
// leaves the original (and sibling trims) usable.
func (v *Video) Subclip(start Time, end ...Time) *Video {
	if v.err != nil {
		return v
	}
	// 0 is the "to the end of the media" sentinel understood by video.Subclip.
	e := Time(0)
	if len(end) > 0 {
		e = end[0]
	}
	return v.derive(video.Subclip(v.inner, start, e))
}

// WithDuration sets the clip's composition duration (required for static
// sources such as images and colors before export).
func (v *Video) WithDuration(d Time) *Video {
	if v.err != nil {
		return v
	}
	c := v.inner.WithDuration(d)
	vc, ok := c.(video.VideoClip)
	if !ok {
		out := v.derive(v.inner)
		out.err = fmt.Errorf("video: WithDuration returned %T, not VideoClip", c)
		return out
	}
	return v.derive(vc)
}

// WithStart sets the clip's composition start.
func (v *Video) WithStart(t Time) *Video {
	if v.err != nil {
		return v
	}
	c := v.inner.WithStart(t)
	vc, ok := c.(video.VideoClip)
	if !ok {
		out := v.derive(v.inner)
		out.err = fmt.Errorf("video: WithStart returned %T, not VideoClip", c)
		return out
	}
	return v.derive(vc)
}

// Resize rescales the clip (and its mask) by a uniform factor.
func (v *Video) Resize(scale float64) *Video {
	return v.apply(effect.Resize{Scale: scale})
}

// ResizeTo rescales the clip to an exact width and height.
func (v *Video) ResizeTo(w, h int) *Video {
	return v.apply(effect.Resize{Width: w, Height: h})
}

// Crop extracts the (x, y, w, h) rectangle, clamped to the clip bounds.
func (v *Video) Crop(x, y, w, h int) *Video {
	return v.apply(effect.Crop{X: x, Y: y, W: w, H: h})
}

// Rotate turns the clip clockwise by an arbitrary angle, growing the canvas to
// fit so no corner is clipped. Exact multiples of 90 take the lossless fast
// path. For in-place rotation that keeps the original size (clipping the
// corners), use Fx(effect.Rotate{Degrees: d}).
func (v *Video) Rotate(degrees float64) *Video {
	return v.apply(effect.Rotate{Degrees: degrees, Expand: true})
}

// FlipH mirrors the clip left-to-right (and its mask). Passing a start time
// delays the flip until then; with no start it applies from the beginning.
func (v *Video) FlipH(start ...Time) *Video {
	return v.apply(effect.FlipH{Start: first(start)})
}

// FlipV mirrors the clip top-to-bottom (and its mask). Passing a start time
// delays the flip until then; with no start it applies from the beginning.
func (v *Video) FlipV(start ...Time) *Video {
	return v.apply(effect.FlipV{Start: first(start)})
}

// Pad centers the clip on a w x h canvas filled with col, letterboxing it. The
// target must be at least the clip's size on each axis. It changes the clip
// size, so it applies to the whole timeline (no start offset).
func (v *Video) Pad(w, h int, col [3]byte) *Video {
	return v.apply(effect.Pad{W: w, H: h, Color: col})
}

// FitTo scales the clip to fit within w x h (preserving aspect) and letterboxes
// it to exactly w x h with black bars, the common social aspect conversion. It
// changes the clip size, so it applies to the whole timeline (no start offset).
func (v *Video) FitTo(w, h int) *Video {
	return v.apply(effect.FitTo{W: w, H: h})
}

// Blur softens the clip; larger radius is softer. A zero radius is a no-op.
// Passing a start time delays the blur until then.
func (v *Video) Blur(radius float64, start ...Time) *Video {
	return v.apply(effect.Blur{Radius: radius, Start: first(start)})
}

// Sharpen enhances edges via an unsharp mask; larger amount sharpens more. A
// zero amount is a no-op. Passing a start time delays sharpening until then.
func (v *Video) Sharpen(amount float64, start ...Time) *Video {
	return v.apply(effect.Sharpen{Amount: amount, Start: first(start)})
}

// BlackAndWhite converts the clip's RGB frames to grayscale. Passing a start
// time delays the filter; with no start it applies from the beginning.
func (v *Video) BlackAndWhite(start ...Time) *Video {
	return v.apply(effect.BlackAndWhite{Start: first(start)})
}

// Grayscale converts the clip's RGB frames to luma (the canonical name for
// BlackAndWhite). Passing a start time delays the conversion.
func (v *Video) Grayscale(start ...Time) *Video {
	return v.apply(effect.Grayscale{Start: first(start)})
}

// Brightness shifts every channel by delta, a normalized offset in [-1, 1]. A
// zero delta is a no-op. Passing a start time delays the effect.
func (v *Video) Brightness(delta float64, start ...Time) *Video {
	return v.apply(effect.Brightness{Delta: delta, Start: first(start)})
}

// Contrast scales each channel around mid-gray by amount (1.0 = unchanged). A
// zero or negative amount is a no-op. Passing a start time delays the effect.
func (v *Video) Contrast(amount float64, start ...Time) *Video {
	return v.apply(effect.Contrast{Amount: amount, Start: first(start)})
}

// Saturation scales color vividness by amount (1.0 = unchanged, <1 desaturates,
// >1 more vivid). A zero or negative amount is a no-op (not gray) — use
// Grayscale for full desaturation. Passing a start time delays the effect.
func (v *Video) Saturation(amount float64, start ...Time) *Video {
	return v.apply(effect.Saturation{Amount: amount, Start: first(start)})
}

// Gamma applies a power curve; value > 1 brightens midtones, < 1 darkens them.
// A non-positive value (or 1.0) is a no-op. Passing a start time delays it.
func (v *Video) Gamma(value float64, start ...Time) *Video {
	return v.apply(effect.Gamma{Value: value, Start: first(start)})
}

// Invert produces a photographic negative. Passing a start time delays it.
func (v *Video) Invert(start ...Time) *Video {
	return v.apply(effect.Invert{Start: first(start)})
}

// GaussianBlur applies a true (separable) Gaussian blur of the given radius
// (sigma in pixels); a zero radius is a no-op. Higher quality than Blur. Passing
// a start time delays the effect.
func (v *Video) GaussianBlur(radius float64, start ...Time) *Video {
	return v.apply(effect.GaussianBlur{Radius: radius, Start: first(start)})
}

// Vignette darkens the frame toward its corners; amount is the corner strength
// in [0, 1] (0 is a no-op). For control over where the falloff begins, use
// Fx(effect.Vignette{Amount: a, Radius: r}). Passing a start time delays it.
func (v *Video) Vignette(amount float64, start ...Time) *Video {
	return v.apply(effect.Vignette{Amount: amount, Start: first(start)})
}

// LUT applies a 3D color lookup table from a Resolve/Adobe .cube file. A parse
// error is carried on the handle and surfaced by Err/WriteVideo.
func (v *Video) LUT(path string) *Video {
	return v.apply(effect.LUT{Path: path})
}

// first returns the first variadic start time, or zero when none was given. It
// backs the optional start parameter on the same-size effect methods.
func first(start []Time) Time {
	if len(start) > 0 {
		return start[0]
	}
	return 0
}

// FadeIn ramps the clip up from black over d.
func (v *Video) FadeIn(d Time) *Video {
	return v.apply(effect.FadeIn{Dur: d})
}

// FadeOut ramps the clip down to black over the last d. It needs a known
// duration.
func (v *Video) FadeOut(d Time) *Video {
	return v.apply(effect.FadeOut{Dur: d})
}

// WaterDrop adds a centered ripple that uses the video as the background while
// circular waves expand outward. A zero duration uses the effect default. Use
// Fx with effect.WaterDrop{X: ..., Y: ...} to choose the landing point.
func (v *Video) WaterDrop(d Time) *Video {
	return v.apply(effect.WaterDrop{Dur: d})
}

// Speed plays the clip faster (factor > 1) or slower (factor < 1).
func (v *Video) Speed(factor float64) *Video {
	return v.apply(effect.MultiplySpeed{Factor: factor})
}

// Loop repeats the clip n times.
func (v *Video) Loop(n int) *Video {
	return v.apply(effect.Loop{N: n})
}

// Fx applies a custom video effect. Pass a configured effect value to customize
// its behavior, for example effect.WaterDrop{Amplitude: 12}.
func (v *Video) Fx(e effect.VideoEffect) *Video {
	if e == nil {
		return v
	}
	return v.apply(e)
}

// Position sets where this clip is placed within a Composite. It is a no-op for
// a standalone export. Placement survives later effect/transform calls, so it
// can be set anywhere in a fluent chain.
func (v *Video) Position(p Position) *Video {
	if v.err != nil {
		return v
	}
	c := v.derive(v.inner)
	c.pos = p
	return c
}

// PositionFunc animates this clip's placement within a Composite: fn is called
// with the clip's local playback time and returns its Position for that instant.
// Only the returned position's absolute X/Y are used (build it with At), since
// an animated placement supplies absolute pixels and overrides keyword/relative
// placement. It is a no-op for a standalone export. This exposes the existing
// composite.Position.Animated hook.
func (v *Video) PositionFunc(fn func(t Time) Position) *Video {
	if v.err != nil {
		return v
	}
	c := v.derive(v.inner)
	c.pos = Position{Animated: func(t Time) (float64, float64) {
		p := fn(t)
		return p.X, p.Y
	}}
	return c
}

// Layer sets this clip's z-order within a Composite (higher renders on top).
func (v *Video) Layer(n int) *Video {
	if v.err != nil {
		return v
	}
	c := v.derive(v.inner)
	c.layer = n
	return c
}

// apply runs a video effect, propagating any earlier build error. Placement
// metadata is carried onto the result.
func (v *Video) apply(e effect.VideoEffect) *Video {
	if v.err != nil {
		return v
	}
	next, err := e.ApplyVideo(v.inner)
	if err != nil {
		c := v.derive(v.inner)
		c.err = err
		return c
	}
	return v.derive(next)
}

// derive returns a new handle wrapping inner while carrying this handle's
// placement (position and layer), so a fluent chain never silently drops a
// Position/Layer set earlier.
func (v *Video) derive(inner video.VideoClip) *Video {
	return &Video{inner: inner, pos: v.pos, layer: v.layer}
}

// Err returns the first build error recorded on this handle, if any.
func (v *Video) Err() error { return v.err }

// Duration reports the clip's duration. It returns Infinite when the length is
// unknown or unbounded (a generated source before WithDuration).
func (v *Video) Duration() Time { return v.inner.Duration() }

// Size returns the clip's display size.
func (v *Video) Size() Size { return v.inner.Size() }

// Rate returns the clip's frame rate; ok is false when unknown.
func (v *Video) Rate() (Rate, bool) { return v.inner.Rate() }

// Close releases the clip's resources. It is idempotent.
func (v *Video) Close() error { return v.inner.Close() }
