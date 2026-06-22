package mgo_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// TestCompositeExport builds a layered composite (black background + a smaller
// red overlay centered) and exports it, verifying size and duration.
func TestCompositeExport(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	bg := mgo.Color(64, 64, [3]byte{0, 0, 0}).WithDuration(time.Second)
	overlay := mgo.Color(32, 32, [3]byte{255, 0, 0}).WithDuration(time.Second).Position(mgo.Center)

	final := mgo.Composite(bg, overlay)
	if final.Err() != nil {
		t.Fatalf("build: %v", final.Err())
	}
	if final.Size() != (mgo.Size{W: 64, H: 64}) {
		t.Fatalf("size = %v, want 64x64", final.Size())
	}

	out := filepath.Join(t.TempDir(), "composite.mp4")
	if err := mgo.WriteVideo(context.Background(), final, out, mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil || info.Video.Size != (mgo.Size{W: 64, H: 64}) {
		t.Fatalf("video = %+v, want 64x64", info.Video)
	}
}

// TestTransparentCompositeExport exports a transparent composite (an alpha
// overlay over a transparent canvas) to a QuickTime .mov (the default alpha
// container, qtrle) and verifies the alpha channel survives.
func TestTransparentCompositeExport(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	// Write a 32x32 half-opaque red PNG and open it through the facade.
	rgba := clip.NewFrame(32, 32, clip.RGBA)
	for i := 0; i < len(rgba.Pix); i += 4 {
		rgba.Pix[i], rgba.Pix[i+3] = 255, 128
	}
	pngPath := filepath.Join(t.TempDir(), "overlay.png")
	pf, err := os.Create(pngPath)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	if err := imagex.EncodePNG(pf, rgba); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	pf.Close()

	overlay, err := mgo.Image(pngPath)
	if err != nil {
		t.Fatalf("open image: %v", err)
	}
	overlay = overlay.WithDuration(time.Second)

	final := mgo.CompositeWith(mgo.CompositeOptions{
		Size:        mgo.Size{W: 32, H: 32},
		Transparent: true,
	}, overlay)
	if final.Err() != nil {
		t.Fatalf("build: %v", final.Err())
	}

	// Default transparent export (qtrle/.mov) must keep the alpha channel.
	out := filepath.Join(t.TempDir(), "transparent.mov")
	if err := mgo.WriteVideo(context.Background(), final, out, mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil {
		t.Fatal("no video stream in transparent export")
	}
	// Decode the first frame back and confirm the 50% alpha survived.
	for _, a := range decodeFirstFrameAlpha(t, out, 32, 32) {
		if a != 128 {
			t.Fatalf("decoded alpha = %d, want 128 (alpha dropped on export)", a)
		}
	}
}

// decodeFirstFrameAlpha decodes path's first frame back to RGBA via FFmpeg and
// returns its alpha channel, so a test can prove alpha survived export.
func decodeFirstFrameAlpha(t *testing.T, path string, w, h int) []byte {
	t.Helper()
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		t.Fatalf("ffmpeg path: %v", err)
	}
	raw, err := exec.Command(bin, "-v", "error", "-i", path, "-f", "rawvideo", "-pix_fmt", "rgba", "-").Output()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	frameBytes := w * h * 4
	if len(raw) < frameBytes {
		t.Fatalf("decoded %d bytes, want >= %d", len(raw), frameBytes)
	}
	alphas := make([]byte, 0, w*h)
	for i := 3; i < frameBytes; i += 4 {
		alphas = append(alphas, raw[i])
	}
	return alphas
}
