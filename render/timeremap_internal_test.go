package render

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/video"
)

// openPattern decodes a short synthetic test-pattern clip, skipping when FFmpeg
// is unavailable.
func openPattern(t *testing.T) video.VideoClip {
	t.Helper()
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "remap.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return n
}

// TestFreezeEngineEquivalence checks a freeze (AccessLinear, pipeline-eligible)
// renders identically on the sequential oracle and the parallel pipeline.
func TestFreezeEngineEquivalence(t *testing.T) {
	src := openPattern(t)
	defer src.Close()
	frozen, err := video.FreezeStart(src, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	assertEngineEquivalence(t, frozen)
}

// TestTimeRemapWithRewindRoutesSequential checks that a TimeRemap whose curve
// contains a decreasing segment is classified ClassRandom, routed to the
// sequential engine, and produces correct frames (the reverseProvider is
// exercised instead of the O(N²) seek path).
func TestTimeRemapWithRewindRoutesSequential(t *testing.T) {
	src := openPattern(t)
	defer src.Close()
	rm, err := video.TimeRemap(src, []video.TimePoint{
		{Out: 0, In: 0},
		{Out: time.Second, In: 2 * time.Second}, // forward slow-mo
		{Out: 2 * time.Second, In: time.Second}, // rewind back to 1s
	}, nil)
	if err != nil {
		t.Fatalf("remap: %v", err)
	}
	if rm.SourceAccess() != video.AccessRandom {
		t.Fatalf("rewind remap access = %v, want Random", rm.SourceAccess())
	}
	plan, err := BuildPlan(rm, planOptions{})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Class != ClassRandom {
		t.Errorf("class = %v, want Random", plan.Class)
	}
	if plan.Engine != EngineSequential {
		t.Errorf("engine = %v, want Sequential", plan.Engine)
	}
	// Render should complete without error (exercises the reverseProvider path).
	frames := renderWith(t, rm, EngineSequential, 1)
	if len(frames) == 0 {
		t.Fatal("no frames rendered")
	}
}

// TestForwardTimeRemapEngineEquivalence checks an all-forward speed ramp stays
// pipeline-eligible and matches the sequential oracle byte-for-byte.
func TestForwardTimeRemapEngineEquivalence(t *testing.T) {
	src := openPattern(t)
	defer src.Close()
	rm, err := video.TimeRemap(src, []video.TimePoint{
		{Out: 0, In: 0},
		{Out: time.Second, In: 500 * time.Millisecond}, // slow-mo (forward)
		{Out: 2 * time.Second, In: 2 * time.Second},    // catch up (still forward)
	}, nil)
	if err != nil {
		t.Fatalf("remap: %v", err)
	}
	if rm.SourceAccess() != video.AccessLinear {
		t.Fatalf("forward remap access = %v, want Linear", rm.SourceAccess())
	}
	assertEngineEquivalence(t, rm)
}
