package mgo

import "github.com/mowshon/moviego/v2/render"

// Report is the planner's verdict for a clip, produced without rendering (the
// chosen engine, whether fusion triggers, frame count, budget). See
// render.Report.
type Report = render.Report

// Engine identifies a render engine in a Report.
type Engine = render.Engine

// Render engines, re-exported so a Report's Engine can be compared from the
// facade.
const (
	EngineSequential = render.EngineSequential
	EnginePipeline   = render.EnginePipeline
	EngineFFmpegOnly = render.EngineFFmpegOnly
)

// Validate runs the planner over v without rendering, returning the first error
// an export would hit: a build error carried on the handle, or a graph the
// planner cannot schedule (unknown duration or rate). It is the cheap pre-flight
// check before WriteVideo.
func Validate(v *Video, opts ...ExportOptions) error {
	if v.err != nil {
		return v.err
	}
	return render.Validate(v.inner, firstExport(opts))
}

// Describe runs the planner over v without rendering and reports its decisions:
// the chosen engine, whether the FFmpeg-only fast path triggers, the frame
// count, output size/rate, and the memory budget. It directly answers questions
// like "did my graph drop to the sequential engine?" A build error carried on
// the handle is returned before planning.
func Describe(v *Video, opts ...ExportOptions) (Report, error) {
	if v.err != nil {
		return Report{}, v.err
	}
	return render.Describe(v.inner, firstExport(opts))
}

// firstExport returns the first ExportOptions or the zero value.
func firstExport(opts []ExportOptions) ExportOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return ExportOptions{}
}
