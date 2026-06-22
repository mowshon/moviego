package render

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// Report is the planner's verdict for a clip, produced without rendering. It
// answers "what would an export of this graph do" — which engine runs, whether
// the FFmpeg-only fast path triggers, the frame count, and the memory budget —
// so a caller can confirm, for example, whether a graph dropped to the
// sequential engine.
type Report struct {
	// Engine is the render engine the planner selected.
	Engine Engine
	// Class is the graph's source-access classification.
	Class Class
	// Frames is the scheduled frame count (an upper bound; a source ending early
	// renders fewer).
	Frames int
	// Transparent reports whether the root carries a mask (an RGBA encode feed).
	Transparent bool
	// Size is the output frame size; Rate is the resolved output rate.
	Size clip.Size
	Rate clip.Rate
	// Workers is the render worker count (1 on the sequential engine).
	Workers int
	// Fused reports whether the export would run as one FFmpeg invocation
	// (Engine == EngineFFmpegOnly), which requires EnableFusion and a fully
	// FFmpeg-expressible graph. FusionReason records why it did or did not.
	Fused        bool
	FusionReason string
	// Budget is the planner's memory ceiling in bytes.
	Budget int64
}

// Describe runs the planner over root with the given export options and reports
// its decisions without rendering. It surfaces any carried build error and the
// validation errors a real export would hit first (missing duration or rate).
func Describe(root video.VideoClip, opts ExportOptions) (Report, error) {
	plan, err := BuildPlan(root, planOptions{
		rate:         opts.Rate,
		workers:      opts.Workers,
		enableFusion: opts.EnableFusion,
		debugf:       opts.Debugf,
	})
	if err != nil {
		return Report{}, err
	}
	return Report{
		Engine:       plan.Engine,
		Class:        plan.Class,
		Frames:       plan.Frames,
		Transparent:  plan.Transparent,
		Size:         plan.Size,
		Rate:         plan.Rate,
		Workers:      plan.Workers,
		Fused:        plan.Engine == EngineFFmpegOnly,
		FusionReason: plan.FusionReason,
		Budget:       plan.Budget,
	}, nil
}

// Validate runs the planner's validation over root without rendering, returning
// the first error an export would hit (a carried build error, an unknown
// duration, or an unknown rate) or nil when the graph is schedulable.
func Validate(root video.VideoClip, opts ExportOptions) error {
	_, err := BuildPlan(root, planOptions{
		rate:         opts.Rate,
		workers:      opts.Workers,
		enableFusion: opts.EnableFusion,
	})
	return err
}
