package video

import (
	"context"

	"github.com/mowshon/moviego/v2/clip"
)

// Parent is implemented by graph nodes that wrap one or more child clips. The
// render planner walks Children to enumerate decodable sources and to reason
// about scheduling capabilities without knowing concrete node types.
type Parent interface {
	// Children returns this node's direct child clips. The order is not
	// significant to the planner; it only enumerates the reachable set.
	Children() []VideoClip
}

// FileSource is a file-backed video source for which the render planner sets up
// a single sequential decoder. SourceKey identifies the decoder session so that
// the same node reached during RenderInto reads from the matching decoder; two
// distinct nodes (e.g. independent placement copies) are distinct sources.
type FileSource interface {
	VideoClip
	SourceKey() any
	SourcePath() string
	SourceSize() clip.Size
	SourceRate() clip.Rate
}

// FrameProvider supplies decoded source frames for file-backed sources during a
// pipelined render. A single sequential decoder per source fills an
// index-addressed table; file nodes copy the frame for their source index out
// of it instead of seeking their own decoder. This is what lets the expensive
// pixel work above a source fan out across workers while decode stays
// sequential and seek-free.
type FrameProvider interface {
	// SourceFrameInto copies the frame at source index idx for the source
	// identified by key into dst. It blocks until that frame is decoded and
	// returns clip.ErrEOF for an index past the end of the stream.
	SourceFrameInto(ctx context.Context, key any, idx int, dst *clip.Frame) error
}

type providerKey struct{}

// WithFrameProvider returns a context carrying p, so file sources reached during
// RenderInto read through it. The render pipeline installs it; interactive
// callers never do, so FrameAt keeps using each node's own decoder.
func WithFrameProvider(ctx context.Context, p FrameProvider) context.Context {
	return context.WithValue(ctx, providerKey{}, p)
}

// frameProviderFrom returns the provider installed on ctx, or nil for the
// interactive (own-decoder) path.
func frameProviderFrom(ctx context.Context) FrameProvider {
	p, _ := ctx.Value(providerKey{}).(FrameProvider)
	return p
}

// WalkSources visits every FileSource reachable from root, deduplicating by
// SourceKey, and calls fn once per distinct source. It is the planner's source
// enumeration: file nodes are leaves, and Parent nodes expose their children.
func WalkSources(root VideoClip, fn func(FileSource)) {
	seen := make(map[any]struct{})
	var walk func(VideoClip)
	walk = func(c VideoClip) {
		if fs, ok := c.(FileSource); ok {
			key := fs.SourceKey()
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				fn(fs)
			}
		}
		if p, ok := c.(Parent); ok {
			for _, child := range p.Children() {
				walk(child)
			}
		}
	}
	walk(root)
}
