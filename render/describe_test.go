package render_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/render"
	"github.com/mowshon/moviego/v2/video"
)

// colorClip builds a finite-duration solid-color clip at 24fps.
func colorClip(dur time.Duration) video.VideoClip {
	c := video.NewColor(clip.Size{W: 16, H: 16}, [3]byte{0, 0, 0})
	return c.WithDuration(dur).(video.VideoClip)
}

// TestDescribeStaticPipeline reports the parallel pipeline for a static
// multi-worker graph.
func TestDescribeStaticPipeline(t *testing.T) {
	rep, err := render.Describe(colorClip(time.Second), render.ExportOptions{
		Rate:    clip.Rate{Num: 24, Den: 1},
		Workers: 4,
	})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if rep.Class != render.ClassStatic {
		t.Errorf("class = %s, want static", rep.Class)
	}
	if rep.Engine != render.EnginePipeline {
		t.Errorf("engine = %s, want pipeline", rep.Engine)
	}
	if rep.Frames != 24 {
		t.Errorf("frames = %d, want 24", rep.Frames)
	}
	if rep.Fused {
		t.Error("fused should be false without EnableFusion")
	}
}

// TestDescribeRandomAccessSequential locks the engine-reporting contract: a
// random-access graph (here a loop) drops to the sequential engine, and
// Describe surfaces it without rendering.
func TestDescribeRandomAccessSequential(t *testing.T) {
	looped := video.Loop(colorClip(time.Second), 2)
	rep, err := render.Describe(looped, render.ExportOptions{
		Rate:    clip.Rate{Num: 24, Den: 1},
		Workers: 4,
	})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if rep.Class != render.ClassRandom {
		t.Errorf("class = %s, want random-access", rep.Class)
	}
	if rep.Engine != render.EngineSequential {
		t.Errorf("engine = %s, want sequential", rep.Engine)
	}
	if rep.Workers != 1 {
		t.Errorf("workers = %d, want 1 on sequential", rep.Workers)
	}
}

// TestValidateNoDuration reports the planner error for an unschedulable graph
// (a static source with no duration).
func TestValidateNoDuration(t *testing.T) {
	c := video.NewColor(clip.Size{W: 8, H: 8}, [3]byte{0, 0, 0})
	err := render.Validate(c, render.ExportOptions{Rate: clip.Rate{Num: 24, Den: 1}})
	if !errors.Is(err, clip.ErrNoDuration) {
		t.Fatalf("err = %v, want ErrNoDuration", err)
	}
}
