package video

import "github.com/mowshon/moviego/v2/ffmpeg"

// Filterable is the optional capability a graph node implements to advertise an
// FFmpeg filter-chain fragment to the fusion planner.
// A node returns ok=false when it cannot be expressed as a filter at the given
// context, which makes the planner fall back to Go rendering for the whole
// graph. It is kept off the core VideoClip interface so a node opts in without
// every clip having to grow a method.
type Filterable interface {
	Filter(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)
}

// FilterSource is the optional capability a leaf source implements to advertise
// itself as an FFmpeg "-i" input for a fused command. The planner assigns the
// input index and the resulting stream label; the source only describes how to
// open it. Returning ok=false forces the Go path.
type FilterSource interface {
	FilterInput(ctx ffmpeg.FilterContext) (ffmpeg.Input, bool)
}
