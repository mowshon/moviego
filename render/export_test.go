package render_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/render"
	"github.com/mowshon/moviego/v2/video"
)

func sourceFile(t *testing.T) string {
	t.Helper()
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.TestPatternVideo(t.TempDir(), "src.mp4", 48, 32, 5, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	return path
}

func TestWriteVideoOpenTrimWrite(t *testing.T) {
	src := sourceFile(t)
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	clip := video.Subclip(n, time.Second, 3*time.Second) // 2s window
	out := filepath.Join(t.TempDir(), "out.mp4")
	if err := render.WriteVideo(context.Background(), clip, out, render.ExportOptions{}); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe out: %v", err)
	}
	if d := info.Duration; d < 1800*time.Millisecond || d > 2200*time.Millisecond {
		t.Errorf("output duration = %v, want ~2s", d)
	}
}

// TestWriteVideoShortEOF schedules more frames than the source contains (via an
// extended duration) and asserts the export stops cleanly at EOF rather than
// erroring, producing only the frames that exist.
func TestWriteVideoShortEOF(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "short.mp4", 48, 32, 1, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	extended := n.WithDuration(3 * time.Second).(video.VideoClip) // schedules ~90 frames; only ~30 exist
	out := filepath.Join(t.TempDir(), "out.mp4")
	if err := render.WriteVideo(context.Background(), extended, out, render.ExportOptions{}); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if d := info.Duration; d > 1500*time.Millisecond {
		t.Errorf("output duration = %v, want ~1s (stopped at source EOF)", d)
	}
}

func TestWriteVideoCancelMidExport(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	// A source long enough that a full export far outlasts the deadline.
	src, err := genmedia.TestPatternVideo(t.TempDir(), "long.mp4", 480, 360, 30, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	out := filepath.Join(t.TempDir(), "out.mp4")

	start := time.Now()
	err = render.WriteVideo(ctx, n, out, render.ExportOptions{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected cancellation error mid-export")
	}
	// Prompt return proves the decoder and encoder were unblocked (killed) by
	// cancellation rather than running to completion.
	if elapsed > 15*time.Second {
		t.Errorf("export took %v to abort; processes may not have been killed", elapsed)
	}
}

func TestWriteVideoRejectsZeroRate(t *testing.T) {
	src := sourceFile(t)
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	out := filepath.Join(t.TempDir(), "out.mp4")
	err = render.WriteVideo(context.Background(), n, out, render.ExportOptions{
		Rate: clip.Rate{Num: 30, Den: 0}, // zero denominator
	})
	if !errors.Is(err, clip.ErrNoRate) {
		t.Errorf("error = %v, want ErrNoRate", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("output file created despite invalid rate")
	}
}

func TestWriteVideoExtraOutputArgsHLS(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "playlist.m3u8")
	segments := filepath.Join(dir, "piece_%03d.ts")
	c := video.NewColor(clip.Size{W: 64, H: 36}, [3]byte{20, 120, 200})
	v := c.WithDuration(3 * time.Second).(video.VideoClip)

	err := render.WriteVideo(context.Background(), v, out, render.ExportOptions{
		Rate: clip.Rate{Num: 10, Den: 1},
		ExtraOutputArgs: []string{
			"-f", "hls",
			"-hls_time", "1",
			"-force_key_frames", "expr:gte(t,n_forced*1)",
			"-hls_segment_filename", segments,
		},
	})
	if err != nil {
		t.Fatalf("write hls: %v", err)
	}
	playlist, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read playlist: %v", err)
	}
	if !strings.Contains(string(playlist), "#EXTM3U") {
		t.Fatalf("playlist missing HLS header:\n%s", playlist)
	}
	got, err := filepath.Glob(filepath.Join(dir, "piece_*.ts"))
	if err != nil {
		t.Fatalf("glob segments: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("segments = %v, want multiple custom-named HLS segments", got)
	}
}

func TestWriteVideoCancel(t *testing.T) {
	src := sourceFile(t)
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(t.TempDir(), "out.mp4")
	if err := render.WriteVideo(ctx, n, out, render.ExportOptions{}); err == nil {
		t.Fatal("expected cancellation error")
	}
}
