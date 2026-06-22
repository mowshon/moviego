package render_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/render"
	"github.com/mowshon/moviego/v2/video"
)

// TestWriteVideoMuxesAudio exports a clip that carries an audio sidecar and
// verifies the output file has an audio stream of roughly the right duration.
func TestWriteVideoMuxesAudio(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.VideoWithAudio(t.TempDir(), "av.mp4", 48, 32, 3, "25")
	if err != nil {
		t.Fatalf("gen a/v: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()
	if n.Audio() == nil {
		t.Fatal("expected the source video to expose an audio sidecar")
	}

	out := filepath.Join(t.TempDir(), "out.mp4")
	if err := render.WriteVideo(context.Background(), n, out, render.ExportOptions{}); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe out: %v", err)
	}
	if info.Audio == nil {
		t.Fatal("output has no audio stream; mux did not run")
	}
	if info.Video == nil {
		t.Fatal("output has no video stream")
	}
	if d := info.Duration; d < 2500*time.Millisecond || d > 3500*time.Millisecond {
		t.Fatalf("output duration = %v, want ~3s", d)
	}
}

// TestWriteVideoDisableAudio confirms the opt-out skips the mux.
func TestWriteVideoDisableAudio(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.VideoWithAudio(t.TempDir(), "av.mp4", 48, 32, 2, "25")
	if err != nil {
		t.Fatalf("gen a/v: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	out := filepath.Join(t.TempDir(), "noaudio.mp4")
	if err := render.WriteVideo(context.Background(), n, out, render.ExportOptions{DisableAudio: true}); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe out: %v", err)
	}
	if info.Audio != nil {
		t.Fatal("output unexpectedly has audio with DisableAudio")
	}
}

// openAV opens a freshly generated a/v file as a video node.
func openAV(t *testing.T, secs float64) *video.VideoFileNode {
	t.Helper()
	src, err := genmedia.VideoWithAudio(t.TempDir(), "av.mp4", 48, 32, secs, "25")
	if err != nil {
		t.Fatalf("gen a/v: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return n
}

// TestWriteVideoCompositeMuxesAudio is the 5b integration gate for the reviewer's
// `Composite(videoWithAudio, overlay)` case: a composite of audio-bearing clips
// must surface and mux their mixed audio, not export silently.
func TestWriteVideoCompositeMuxesAudio(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	base := openAV(t, 3)
	defer base.Close()
	overlay := openAV(t, 2)
	defer overlay.Close()

	comp := composite.New([]composite.CompositeChild{
		{Clip: base, Start: 0},
		{Clip: overlay, Start: 0, Pos: composite.Position{X: 4, Y: 4}},
	}, composite.Options{Size: base.Size()})

	out := filepath.Join(t.TempDir(), "comp.mp4")
	if err := render.WriteVideo(context.Background(), comp, out, render.ExportOptions{
		Rate: clip.Rate{Num: 25, Den: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Audio == nil {
		t.Fatal("composite output has no audio stream; child audio was dropped")
	}
	if info.Video == nil {
		t.Fatal("composite output has no video stream")
	}
}

// TestWriteVideoConcatMuxesAudio is the 5b gate for `Concat(clip1, clip2)`: the
// chain must surface its children's audio sequenced back to back.
func TestWriteVideoConcatMuxesAudio(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	c1 := openAV(t, 2)
	defer c1.Close()
	c2 := openAV(t, 2)
	defer c2.Close()

	chain, err := composite.ConcatChain([]video.VideoClip{c1, c2})
	if err != nil {
		t.Fatalf("concat chain: %v", err)
	}

	out := filepath.Join(t.TempDir(), "concat.mp4")
	if err := render.WriteVideo(context.Background(), chain, out, render.ExportOptions{
		Rate: clip.Rate{Num: 25, Den: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Audio == nil {
		t.Fatal("concat output has no audio stream; child audio was dropped")
	}
	if d := info.Duration; d < 3500*time.Millisecond || d > 4500*time.Millisecond {
		t.Fatalf("concat duration = %v, want ~4s (2s + 2s)", d)
	}
}
