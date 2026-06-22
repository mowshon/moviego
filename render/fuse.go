package render

import (
	"fmt"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// fuse decides whether root can be rendered as a single FFmpeg filtergraph
// invocation (the FFmpegOnly engine), skipping the Go pixel path entirely
// when the whole graph can be expressed by FFmpeg. It returns the assembled
// graph and ok=true on success, or ok=false with a human-readable reason the
// planner logs.
//
// v1 fusion is deliberately narrow and conservative: it fuses a single linear
// file source through a chain of forward trim / positive speed / scale / crop /
// fade / color / arbitrary-angle-rotate wrappers — any node that advertises a
// filter. Anything else — a transparent (masked) output, a multi-layer
// composite or concat, an image/color source, a quarter-turn rotation (lossless
// Go transpose, intentionally not yet filter-gated), or any node that does not
// advertise a filter — declines, and the planner falls back to the Go
// engine, which stays the correctness baseline. The conservative default means
// a graph is only ever fused when its filter expression is well understood.
func fuse(root video.VideoClip, rate clip.Rate) (*ffmpeg.FilterGraph, bool, string) {
	if root.HasMask() {
		// Transparent multi-layer composites and alpha chains are explicitly out
		// of scope for v1 fusion; the Go path handles transparent export.
		return nil, false, "transparent output not fused in v1"
	}

	// Descend the single-child spine, collecting filter wrappers from the root
	// down to a single source leaf.
	var wrappers []video.Filterable // root-first; applied innermost-first below
	cur := root
	for {
		if _, ok := cur.(video.FilterSource); ok {
			return buildFusion(cur, wrappers, rate)
		}
		f, ok := cur.(video.Filterable)
		if !ok {
			return nil, false, fmt.Sprintf("node %T is not FFmpeg-expressible", cur)
		}
		p, ok := cur.(video.Parent)
		if !ok {
			return nil, false, fmt.Sprintf("node %T advertises a filter but exposes no child", cur)
		}
		kids := p.Children()
		if len(kids) != 1 {
			return nil, false, fmt.Sprintf("node %T has %d children; only single-source chains fuse in v1", cur, len(kids))
		}
		wrappers = append(wrappers, f)
		cur = kids[0]
	}
}

// buildFusion assembles the filtergraph for a fully expressible spine: leaf
// (a FilterSource) at the bottom, wrappers above it (collected root-first). The
// stream flows source -> innermost wrapper -> ... -> outermost wrapper -> fps,
// so wrappers are applied in reverse of collection order.
func buildFusion(leaf video.VideoClip, wrappers []video.Filterable, rate clip.Rate) (*ffmpeg.FilterGraph, bool, string) {
	src := leaf.(video.FilterSource)
	srcRate, _ := leaf.Rate()

	baseCtx := ffmpeg.FilterContext{OutputRate: rate, PixFmt: clip.RGB24}
	input, ok := src.FilterInput(baseCtx)
	if !ok {
		return nil, false, fmt.Sprintf("source %T declined fusion", leaf)
	}

	g := &ffmpeg.FilterGraph{Inputs: []ffmpeg.Input{input}}
	// The first input's video stream is FFmpeg label "0:v".
	pad := ffmpeg.Pad{Label: "0:v", Rate: srcRate, PixFmt: clip.RGB24}

	label := 0
	next := func() string { label++; return fmt.Sprintf("v%d", label) }

	for i := len(wrappers) - 1; i >= 0; i-- {
		ctx := ffmpeg.FilterContext{
			Inputs:     []ffmpeg.Pad{pad},
			OutLabel:   next(),
			OutputRate: rate,
			PixFmt:     pad.PixFmt,
		}
		frag, ok := wrappers[i].Filter(ctx)
		if !ok {
			return nil, false, fmt.Sprintf("wrapper %T declined fusion", wrappers[i])
		}
		for _, c := range frag.Constraints {
			if c == ffmpeg.ConstraintAlpha {
				return nil, false, "alpha-constrained filter not fused in v1"
			}
		}
		g.Lines = append(g.Lines, frag.Lines...)
		pad = frag.Out
	}

	// Normalize to the export rate as the final stage, matching the rate the Go
	// engine samples at (and injecting a concrete rate for sources that have
	// none). The render layer additionally caps the output to the scheduled
	// frame count so the fused export and the Go export agree on length.
	out := next()
	g.Lines = append(g.Lines, ffmpeg.FPS(pad.Label, out, rate))
	g.OutVideo = out
	return g, true, "fused single-source linear chain"
}
