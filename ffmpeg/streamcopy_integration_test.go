package ffmpeg_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// TestStreamCopyTrimRoundTrip cuts a segment with stream copy and confirms the
// output is a playable file of roughly the requested length. Stream copy is
// keyframe-accurate, so the tolerance is generous on the low side.
func TestStreamCopyTrimRoundTrip(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.TestPatternVideo(dir, "src.mp4", 64, 48, 5, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	out := filepath.Join(dir, "cut.mp4")
	if err := ffmpeg.StreamCopyTrim(ctx, src, out, time.Second, 2*time.Second); err != nil {
		t.Fatalf("stream copy: %v", err)
	}
	info, err := ffmpeg.Probe(ctx, out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil {
		t.Fatal("no video stream in stream-copied output")
	}
	// Keyframe snapping can include a little extra at the head, but the cut must
	// not be anywhere near the full 5s source.
	if d := info.Duration; d < 1500*time.Millisecond || d > 3200*time.Millisecond {
		t.Errorf("duration = %v, want ~2s (keyframe-snapped)", d)
	}
}

// TestConcatDemuxRoundTrip joins two identical-parameter clips and checks the
// result is about the sum of their durations.
func TestConcatDemuxRoundTrip(t *testing.T) {
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
	if err := ffmpeg.ConcatDemux(ctx, []string{a, b}, out); err != nil {
		t.Fatalf("concat demux: %v", err)
	}
	info, err := ffmpeg.Probe(ctx, out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if d := info.Duration; d < 3500*time.Millisecond || d > 4500*time.Millisecond {
		t.Errorf("duration = %v, want ~4s", d)
	}
}

// TestConcatDemuxRelativePaths is the regression guard for the bug where the
// concat list lived in the system temp dir while inputs were relative: FFmpeg
// resolves "file '...'" entries against the list's directory, so a relative
// input would be looked up next to the temp file and fail. ConcatDemux now
// absolutizes each input, so cwd-relative names must still work.
func TestConcatDemuxRelativePaths(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := genmedia.TestPatternVideo(dir, "a.mp4", 64, 48, 2, "25"); err != nil {
		t.Fatalf("gen a: %v", err)
	}
	if _, err := genmedia.TestPatternVideo(dir, "b.mp4", 64, 48, 2, "25"); err != nil {
		t.Fatalf("gen b: %v", err)
	}
	// Run from dir and pass bare, cwd-relative names.
	t.Chdir(dir)
	out := "joined.mp4"
	if err := ffmpeg.ConcatDemux(ctx, []string{"a.mp4", "b.mp4"}, out); err != nil {
		t.Fatalf("concat demux with relative paths: %v", err)
	}
	info, err := ffmpeg.Probe(ctx, out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if d := info.Duration; d < 3500*time.Millisecond || d > 4500*time.Millisecond {
		t.Errorf("duration = %v, want ~4s", d)
	}
}
