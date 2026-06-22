package mgo_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// TestFacadeColorResizeFadeExport drives a fully synthetic graph (no input
// file) through the static source, the eager resize, the lazy fade, and the
// sequential encoder, then verifies the output's size and duration.
func TestFacadeColorResizeFadeExport(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	clip := mgo.Color(64, 48, [3]byte{255, 0, 0}).
		WithDuration(time.Second).
		Resize(0.5). // 32x24
		FadeIn(500 * time.Millisecond)
	if clip.Err() != nil {
		t.Fatalf("build: %v", clip.Err())
	}
	if clip.Size() != (mgo.Size{W: 32, H: 24}) {
		t.Fatalf("size = %v, want 32x24", clip.Size())
	}

	out := filepath.Join(t.TempDir(), "color.mp4")
	if err := mgo.WriteVideo(context.Background(), clip, out, mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil || info.Video.Size != (mgo.Size{W: 32, H: 24}) {
		t.Fatalf("video = %+v, want 32x24", info.Video)
	}
	if d := info.Duration; d < 900*time.Millisecond || d > 1100*time.Millisecond {
		t.Errorf("duration = %v, want ~1s", d)
	}
}

// TestFacadeFadeOutNoDurationError confirms a build error (fade-out without a
// known duration) is surfaced rather than producing a file.
func TestFacadeFadeOutNoDurationError(t *testing.T) {
	clip := mgo.Color(16, 16, [3]byte{}).FadeOut(time.Second) // no duration set
	if clip.Err() == nil {
		t.Fatal("expected a build error for fade-out without duration")
	}
	err := mgo.WriteVideo(context.Background(), clip, filepath.Join(t.TempDir(), "x.mp4"), mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	})
	if err == nil {
		t.Error("WriteVideo should surface the build error")
	}
}
