package video

import (
	"context"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// AccessClass classifies how a node maps output frames to source-frame reads,
// so the render planner can pick a decode strategy.
type AccessClass int

const (
	// AccessStatic has no time dependence (Image, Color).
	AccessStatic AccessClass = iota
	// AccessLinear maps output i to a monotonically increasing source frame
	// (file passthrough, forward subclip).
	AccessLinear
	// AccessBounded reorders within a bounded window.
	AccessBounded
	// AccessRandom reads arbitrary source times (reverse, loop, negative speed).
	AccessRandom
)

// VideoClip is the video graph contract. RenderInto fills caller-owned buffers
// in a single pass and is the export path; FrameInto and MaskInto are the
// opaque-only and mask-only conveniences. The capability methods (ParallelSafe,
// SourceAccess) describe how the node may be scheduled. Structured FFmpeg-filter
// advertisement (the fusion planner's Filter method) lands with the planner.
type VideoClip interface {
	clip.Clip

	// RenderInto fills rgbDst and, when the clip is transparent, alphaDst in a
	// single pass. It returns hasAlpha=false when fully opaque (alphaDst is then
	// left untouched). When ParallelSafe is true it is safe to call concurrently
	// for different t.
	RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (hasAlpha bool, err error)

	// FrameInto is the opaque-only path: it fills rgbDst and ignores alpha.
	FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error

	// MaskInto renders only the alpha sidecar; a false result means fully opaque.
	MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error)

	Size() clip.Size
	Rate() (clip.Rate, bool)
	HasMask() bool
	Audio() audio.AudioClip

	ParallelSafe() bool
	SourceAccess() AccessClass
}

// StaticMapper is implemented by clips whose pixels are constant over time
// (image and color sources). It lets a pure geometric effect (resize, crop,
// rotate) apply once at construction and return a new static clip, instead of
// re-running the transform for every output frame. rgbFn fills an outSize RGB24
// destination from the source frame; maskFn does the same for the Gray8 mask
// when the clip has one (it may be nil for clips with no mask).
type StaticMapper interface {
	VideoClip
	MapStatic(outSize clip.Size, rgbFn, maskFn func(dst, src *clip.Frame) error) (VideoClip, error)
}

// copyPixels copies the packed pixel data from src into dst. It assumes both
// frames are packed (Stride == W*bpp), which holds for every current producer
// (decoder output and pooled buffers). A stride-aware copy will be needed once
// crop/resize fast paths introduce sub-frame strides.
func copyPixels(dst, src *clip.Frame) {
	copy(dst.Pix, src.Pix)
}
