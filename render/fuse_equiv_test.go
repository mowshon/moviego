package render_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/render"
	"github.com/mowshon/moviego/v2/video"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// TestFFmpegOnlyApproxGoRender verifies that a graph the planner fuses
// (file -> crop) produces essentially the same pixels as the Go render of the
// same graph. A crop is an exact pixel op, so the only
// expected difference is the codec/colorspace round-trip (the Go path decodes
// to RGB and re-encodes; the fused path stays in the source's YUV), which is
// bounded by a small tolerance. A trim is intentionally excluded here: its
// frame-boundary rule (FFmpeg trim pts>=start vs the Go floor index) is a
// documented timing divergence, not a fusion bug.
func TestFFmpegOnlyApproxGoRender(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.TestPatternVideo(dir, "src.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}

	build := func() video.VideoClip {
		n, err := video.OpenFile(ctx, src)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		cropped, err := effect.Crop{X: 8, Y: 4, W: 32, H: 32}.ApplyVideo(n)
		if err != nil {
			t.Fatalf("crop: %v", err)
		}
		return cropped
	}

	// Go render (fusion not enabled) is the oracle.
	var goEngine, fuseEngine string
	goOut := filepath.Join(dir, "go.mp4")
	if err := render.WriteVideo(ctx, build(), goOut, render.ExportOptions{
		Rate:   clip.Rate{Num: 25, Den: 1},
		Debugf: func(f string, a ...any) { goEngine = fmt.Sprintf(f, a...) },
	}); err != nil {
		t.Fatalf("go write: %v", err)
	}

	// FFmpeg-only (fusion opted in).
	fuseOut := filepath.Join(dir, "fuse.mp4")
	if err := render.WriteVideo(ctx, build(), fuseOut, render.ExportOptions{
		Rate:         clip.Rate{Num: 25, Den: 1},
		EnableFusion: true,
		Debugf:       func(f string, a ...any) { fuseEngine = fmt.Sprintf(f, a...) },
	}); err != nil {
		t.Fatalf("fuse write: %v", err)
	}

	if !strings.Contains(goEngine, "engine=sequential") && !strings.Contains(goEngine, "engine=pipeline") {
		t.Errorf("default (no EnableFusion) should pick a Go engine; debug = %q", goEngine)
	}
	if !strings.Contains(fuseEngine, "engine=ffmpeg-only") {
		t.Fatalf("EnableFusion export should fuse; debug = %q", fuseEngine)
	}

	// Both outputs must have the cropped size and a comparable length.
	gi := probe(t, ctx, goOut)
	fi := probe(t, ctx, fuseOut)
	if gi.Video.Size != (clip.Size{W: 32, H: 32}) || fi.Video.Size != (clip.Size{W: 32, H: 32}) {
		t.Fatalf("sizes: go=%v fuse=%v, want 32x32", gi.Video.Size, fi.Video.Size)
	}
	if d := gi.Duration - fi.Duration; d < -120e6 || d > 120e6 {
		t.Errorf("durations diverge: go=%v fuse=%v", gi.Duration, fi.Duration)
	}

	// The first decoded frame should match within codec tolerance; a wrong crop
	// region or frame would blow far past it.
	gf := firstFrame(t, ctx, goOut)
	ff := firstFrame(t, ctx, fuseOut)
	if mean := meanAbsDiff(gf, ff); mean > 16 {
		t.Errorf("mean abs pixel diff = %.2f, want <= 16 (fused vs Go render)", mean)
	}
}

// renderBoth renders build() twice — once with fusion disabled (the Go oracle)
// and once with fusion enabled (the default) — and asserts the engine actually
// taken on each path, returning the two output file paths.
func renderBoth(t *testing.T, ctx context.Context, dir string, rate clip.Rate, build func() video.VideoClip) (goOut, fuseOut string) {
	t.Helper()
	var goEngine, fuseEngine string
	goOut = filepath.Join(dir, "go.mp4")
	// Fusion is opt-in; the default export is the Go oracle.
	if err := render.WriteVideo(ctx, build(), goOut, render.ExportOptions{
		Rate:   rate,
		Debugf: func(f string, a ...any) { goEngine = fmt.Sprintf(f, a...) },
	}); err != nil {
		t.Fatalf("go write: %v", err)
	}
	fuseOut = filepath.Join(dir, "fuse.mp4")
	if err := render.WriteVideo(ctx, build(), fuseOut, render.ExportOptions{
		Rate:         rate,
		EnableFusion: true,
		Debugf:       func(f string, a ...any) { fuseEngine = fmt.Sprintf(f, a...) },
	}); err != nil {
		t.Fatalf("fuse write: %v", err)
	}
	if !strings.Contains(goEngine, "engine=sequential") && !strings.Contains(goEngine, "engine=pipeline") {
		t.Errorf("default (no EnableFusion) should pick a Go engine; debug = %q", goEngine)
	}
	if !strings.Contains(fuseEngine, "engine=ffmpeg-only") {
		t.Fatalf("EnableFusion export should fuse; debug = %q", fuseEngine)
	}
	return goOut, fuseOut
}

// TestFFmpegOnlyApproxGoRenderSpeed checks the setpts (speed) fragment against
// the Go render. A 2x speed keeps the same source frames retimed, so frame 0 of
// both outputs is source frame 0 and must match within codec tolerance.
func TestFFmpegOnlyApproxGoRenderSpeed(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.TestPatternVideo(dir, "src.mp4", 64, 48, 3, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	build := func() video.VideoClip {
		n, err := video.OpenFile(ctx, src)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		sped, err := effect.MultiplySpeed{Factor: 2}.ApplyVideo(n)
		if err != nil {
			t.Fatalf("speed: %v", err)
		}
		return sped
	}
	goOut, fuseOut := renderBoth(t, ctx, dir, clip.Rate{Num: 25, Den: 1}, build)
	if mean := meanAbsDiff(firstFrame(t, ctx, goOut), firstFrame(t, ctx, fuseOut)); mean > 16 {
		t.Errorf("speed: mean abs pixel diff = %.2f, want <= 16", mean)
	}
}

// TestFFmpegOnlyApproxGoRenderFade brackets the fade fragment against the Go
// render: frame 0 is near-black on both (fade just begun), and a frame well
// after the 1s fade window is the full source frame on both. This pins the fade
// curve direction and the window length without matching fragile mid-fade
// floats byte-for-byte.
func TestFFmpegOnlyApproxGoRenderFade(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.TestPatternVideo(dir, "src.mp4", 64, 48, 3, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	build := func() video.VideoClip {
		n, err := video.OpenFile(ctx, src)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		faded, err := effect.FadeIn{Dur: time.Second}.ApplyVideo(n)
		if err != nil {
			t.Fatalf("fade: %v", err)
		}
		return faded
	}
	goOut, fuseOut := renderBoth(t, ctx, dir, clip.Rate{Num: 25, Den: 1}, build)

	// Frame 0: fade just started, both essentially black.
	if l := meanLevel(firstFrame(t, ctx, goOut)); l > 8 {
		t.Errorf("fade go frame0 brightness = %.2f, want near 0", l)
	}
	if l := meanLevel(firstFrame(t, ctx, fuseOut)); l > 8 {
		t.Errorf("fade fuse frame0 brightness = %.2f, want near 0", l)
	}
	// Frame 50 (t=2s, well past the 1s fade): full source frame on both paths.
	gf := nthFrame(t, ctx, goOut, 50)
	ff := nthFrame(t, ctx, fuseOut, 50)
	if l := meanLevel(gf); l < 24 {
		t.Errorf("fade go frame50 brightness = %.2f, want bright (fade done)", l)
	}
	if mean := meanAbsDiff(gf, ff); mean > 16 {
		t.Errorf("fade post-window: mean abs pixel diff = %.2f, want <= 16", mean)
	}
}

// TestFFmpegOnlyWithAudioSidecar exercises the fused path's audio mux: a single
// linear video graph that carries an audio sidecar must fuse the video and still
// emit an output with a playable audio stream (the -map N:a:0 / -shortest path).
func TestFFmpegOnlyWithAudioSidecar(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.VideoWithAudio(dir, "av.mp4", 64, 48, 3, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(ctx, src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()
	cropped, err := effect.Crop{X: 8, Y: 4, W: 32, H: 32}.ApplyVideo(n)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}

	var engine string
	out := filepath.Join(dir, "av_out.mp4")
	if err := render.WriteVideo(ctx, cropped, out, render.ExportOptions{
		Rate:         clip.Rate{Num: 25, Den: 1},
		EnableFusion: true,
		Debugf:       func(f string, a ...any) { engine = fmt.Sprintf(f, a...) },
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(engine, "engine=ffmpeg-only") {
		t.Fatalf("graph with audio sidecar should still fuse the video; debug = %q", engine)
	}
	info := probe(t, ctx, out)
	if info.Video.Size != (clip.Size{W: 32, H: 32}) {
		t.Errorf("video size = %v, want 32x32", info.Video.Size)
	}
	if info.Audio == nil {
		t.Error("fused output has no audio stream; the -map N:a:0 mux did not run")
	}
}

// TestRotatedSourceDeclinesFusion gates issue #2: a source carrying display
// rotation must NOT take the fused path (FFmpeg does not auto-rotate a raw
// filter_complex input), falling back to the Go engine that rotates correctly.
func TestRotatedSourceDeclinesFusion(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.RotatedVideo(dir, "rot.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(ctx, src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()
	cropped, err := effect.Crop{X: 4, Y: 4, W: 16, H: 16}.ApplyVideo(n)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	var engine string
	out := filepath.Join(dir, "rot_out.mp4")
	// Opt into fusion so the rotation gate is actually exercised (without
	// EnableFusion the export would be a Go engine regardless).
	if err := render.WriteVideo(ctx, cropped, out, render.ExportOptions{
		Rate:         clip.Rate{Num: 25, Den: 1},
		EnableFusion: true,
		Debugf:       func(f string, a ...any) { engine = fmt.Sprintf(f, a...) },
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if strings.Contains(engine, "engine=ffmpeg-only") {
		t.Errorf("rotated source must not fuse; debug = %q", engine)
	}
}

// TestFadeOutLongFadeDeclinesFusion gates the High finding: a FadeOut whose
// duration exceeds the clip starts already partway faded in the Go path, which
// the FFmpeg fade cannot reproduce, so it must decline fusion. A fade that fits
// the clip still fuses.
func TestFadeOutLongFadeDeclinesFusion(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.TestPatternVideo(dir, "src.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	engineFor := func(fadeDur time.Duration, name string) string {
		n, err := video.OpenFile(ctx, src)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer n.Close()
		faded, err := effect.FadeOut{Dur: fadeDur}.ApplyVideo(n)
		if err != nil {
			t.Fatalf("fade: %v", err)
		}
		var engine string
		if err := render.WriteVideo(ctx, faded, filepath.Join(dir, name), render.ExportOptions{
			Rate:         clip.Rate{Num: 25, Den: 1},
			EnableFusion: true,
			Debugf:       func(f string, a ...any) { engine = fmt.Sprintf(f, a...) },
		}); err != nil {
			t.Fatalf("write: %v", err)
		}
		return engine
	}
	// 4s fade on a 2s clip: must fall back to Go.
	if e := engineFor(4*time.Second, "long.mp4"); strings.Contains(e, "engine=ffmpeg-only") {
		t.Errorf("over-long fade-out must not fuse; debug = %q", e)
	}
	// 1s fade on a 2s clip: fits, so it fuses.
	if e := engineFor(time.Second, "short.mp4"); !strings.Contains(e, "engine=ffmpeg-only") {
		t.Errorf("in-bounds fade-out should fuse; debug = %q", e)
	}
}

// countProgress records the total and the number of Step calls.
type countProgress struct {
	total int
	steps int
}

func (c *countProgress) SetTotal(n int) { c.total = n }
func (c *countProgress) Step()          { c.steps++ }

// TestFFmpegOnlyProgressAdvances gates the Medium finding: the fused path must
// report per-frame progress (it parses FFmpeg's -progress stream), not sit at 0
// until completion.
func TestFFmpegOnlyProgressAdvances(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src, err := genmedia.TestPatternVideo(dir, "src.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(ctx, src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()
	cropped, err := effect.Crop{X: 8, Y: 4, W: 32, H: 32}.ApplyVideo(n)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	prog := &countProgress{}
	var engine string
	if err := render.WriteVideo(ctx, cropped, filepath.Join(dir, "out.mp4"), render.ExportOptions{
		Rate:         clip.Rate{Num: 25, Den: 1},
		EnableFusion: true,
		Progress:     prog,
		Debugf:       func(f string, a ...any) { engine = fmt.Sprintf(f, a...) },
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(engine, "engine=ffmpeg-only") {
		t.Fatalf("expected fused export; debug = %q", engine)
	}
	if prog.total <= 0 {
		t.Errorf("SetTotal not called with a positive total (got %d)", prog.total)
	}
	if prog.steps <= 0 {
		t.Error("progress did not advance on the fused path (stuck at 0)")
	}
	if prog.steps > prog.total {
		t.Errorf("progress steps = %d overshot total = %d", prog.steps, prog.total)
	}
}

func probe(t *testing.T, ctx context.Context, path string) *ffmpeg.MediaInfo {
	t.Helper()
	info, err := ffmpeg.Probe(ctx, path, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	if info.Video == nil {
		t.Fatalf("probe %s: no video stream", path)
	}
	return info
}

// firstFrame decodes frame 0 of path into a packed RGB24 byte slice.
func firstFrame(t *testing.T, ctx context.Context, path string) []byte {
	t.Helper()
	return nthFrame(t, ctx, path, 0)
}

// nthFrame decodes frame idx of path into a packed RGB24 byte slice.
func nthFrame(t *testing.T, ctx context.Context, path string, idx int) []byte {
	t.Helper()
	info := probe(t, ctx, path)
	dec, err := videoio.OpenDecoder(ctx, path, videoio.DecoderOptions{Size: info.Video.Size, Rate: info.Video.Rate})
	if err != nil {
		t.Fatalf("open decoder %s: %v", path, err)
	}
	defer dec.Close()
	f := clip.NewFrame(info.Video.Size.W, info.Video.Size.H, clip.RGB24)
	if err := dec.SeekToFrame(idx); err != nil {
		t.Fatalf("seek %s: %v", path, err)
	}
	if err := dec.ReadInto(f); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := make([]byte, len(f.Pix))
	copy(out, f.Pix)
	return out
}

// meanLevel is the average byte value of a frame, used as a brightness proxy
// (a fully faded-in frame is bright; a faded-out/black frame is near zero).
func meanLevel(a []byte) float64 {
	if len(a) == 0 {
		return 0
	}
	var sum int64
	for _, v := range a {
		sum += int64(v)
	}
	return float64(sum) / float64(len(a))
}

func meanAbsDiff(a, b []byte) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 1 << 30
	}
	var sum int64
	for i := 0; i < n; i++ {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		sum += int64(d)
	}
	return float64(sum) / float64(n)
}
