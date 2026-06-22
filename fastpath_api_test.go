package mgo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// TestConcatFilesDemuxFastPath joins two codec/parameter-compatible clips; the
// concat demuxer copies them with no re-encode and the result is ~the sum of
// their durations.
func TestConcatFilesDemuxFastPath(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	a, err := genmedia.TestPatternVideo(dir, "a.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen a: %v", err)
	}
	b, err := genmedia.TestPatternVideo(dir, "b.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen b: %v", err)
	}
	out := filepath.Join(dir, "joined.mp4")
	if err := mgo.ConcatFiles(ctx, out, a, b); err != nil {
		t.Fatalf("concat files: %v", err)
	}
	assertDurationNear(t, ctx, out, 4*time.Second)
}

// TestConcatFilesIncompatibleFallback joins two codec-incompatible clips (H.264
// and MPEG-4). The concat demuxer's stream copy cannot handle that, so
// ConcatFiles must fall back to the re-encode path and still produce a valid
// joined file.
func TestConcatFilesIncompatibleFallback(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	a, err := genmedia.EncodedVideo(dir, "h264.mp4", "libx264", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen h264: %v", err)
	}
	b, err := genmedia.EncodedVideo(dir, "mpeg4.mp4", "mpeg4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen mpeg4: %v", err)
	}
	out := filepath.Join(dir, "joined.mp4")
	if err := mgo.ConcatFiles(ctx, out, a, b); err != nil {
		t.Fatalf("concat files (fallback): %v", err)
	}
	assertDurationNear(t, ctx, out, 4*time.Second)
}

// TestConcatFilesCanceledNoFallback confirms a canceled context is reported as
// cancellation rather than triggering the expensive re-encode fallback.
func TestConcatFilesCanceledNoFallback(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	a, err := genmedia.TestPatternVideo(dir, "a.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen a: %v", err)
	}
	b, err := genmedia.TestPatternVideo(dir, "b.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen b: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = mgo.ConcatFiles(ctx, filepath.Join(dir, "out.mp4"), a, b)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func assertDurationNear(t *testing.T, ctx context.Context, path string, want time.Duration) {
	t.Helper()
	info, err := ffmpeg.Probe(ctx, path, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil {
		t.Fatal("output has no video stream")
	}
	if d := info.Duration; d < want-700*time.Millisecond || d > want+700*time.Millisecond {
		t.Errorf("duration = %v, want ~%v", d, want)
	}
}
