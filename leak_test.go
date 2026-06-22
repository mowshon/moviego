package mgo_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// settledGoroutines waits for the goroutine count to drop to at most baseline +
// slack, giving FFmpeg's process reaper and stderr-drain goroutines a moment to
// exit after Wait/Kill. It returns the final count.
func settledGoroutines(baseline, slack int) int {
	last := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		runtime.GC()
		last = runtime.NumGoroutine()
		if last <= baseline+slack {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last
}

// TestExportNoGoroutineLeak runs several exports and asserts the goroutine count
// returns to its baseline afterward — decoder, encoder, worker-pool, and
// stderr-drain goroutines must all exit when an export completes. (Process death
// is owned by ffmpeg.Proc's CommandContext + process group; Wait blocks until
// the child exits, so a returned WriteVideo leaves no orphan.)
func TestExportNoGoroutineLeak(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "src.mp4", 64, 48, 2, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}

	baseline := settledGoroutines(0, 0)
	for i := 0; i < 3; i++ {
		v, err := mgo.OpenVideo(src)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		out := filepath.Join(t.TempDir(), "out.mp4")
		if err := mgo.WriteVideo(context.Background(), v.Subclip(0, mgo.Sec(1)), out, mgo.ExportOptions{
			Rate: mgo.Rate{Num: 30, Den: 1},
		}); err != nil {
			v.Close()
			t.Fatalf("write %d: %v", i, err)
		}
		v.Close()
	}

	// Allow a couple of goroutines of slack for the runtime's own bookkeeping.
	if got := settledGoroutines(baseline, 2); got > baseline+2 {
		t.Errorf("goroutines = %d after exports, baseline %d (leak suspected)", got, baseline)
	}
}

// TestExportCancelNoLeak cancels an export mid-flight and asserts the goroutine
// count still settles to baseline: canceling the context must kill FFmpeg and
// unblock every stage, not strand a goroutine on a full channel or a blocked
// pipe read.
func TestExportCancelNoLeak(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "src.mp4", 128, 96, 5, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}

	baseline := settledGoroutines(0, 0)

	v, err := mgo.OpenVideo(src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer v.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the export starts, mid-render.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	out := filepath.Join(t.TempDir(), "out.mp4")
	err = mgo.WriteVideo(ctx, v, out, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}})
	if err == nil {
		t.Skip("export finished before cancel fired; timing-dependent, not a failure")
	}

	if got := settledGoroutines(baseline, 2); got > baseline+2 {
		t.Errorf("goroutines = %d after cancel, baseline %d (leak suspected)", got, baseline)
	}
}
