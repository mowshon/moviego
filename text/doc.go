// Package text rasterizes text and subtitles into clip.Frame buffers that the
// video graph consumes as static image sources.
//
// The font stack is pinned to the pure-Go golang.org/x/image stack
// (font/opentype over font/sfnt), with the embedded Go Regular face as the
// default so rendering is deterministic in CI without a system font. A parsed
// font is concurrency-safe and reused; the per-size font.Face it produces is
// not, so one is created per rasterization call.
//
// Scope is caption-stable, not pixel-identical to Pillow/MoviePy: line layout,
// wrapping, and the clip's reported height are reproducible and match MoviePy's
// height math (baseline anchor, real_font_size + 2*stroke + (lines-1)*line
// spacing), but anti-aliased glyph pixels are not asserted byte-for-byte. This
// divergence is recorded in docs/compatibility.md.
//
// Rasterization (Render) produces a clip.Frame and lives below the video graph;
// New wraps that frame in a video.ImageNode, and SubtitlesNode is a transparent,
// canvas-sized video clip that swaps the active cue over time. The package
// imports video but never composite.
package text
