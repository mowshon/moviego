package render

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// fakeSource is a file-like leaf that advertises itself as a fusion input
// without touching FFmpeg, so the fusion decision can be unit-tested offline.
type fakeSource struct {
	size clip.Size
	rate clip.Rate
	dur  clip.Time
	mask bool
}

func (f *fakeSource) Start() clip.Time                   { return 0 }
func (f *fakeSource) Duration() clip.Time                { return clip.DurationOr(f.dur, f.dur > 0) }
func (f *fakeSource) End() clip.Time                     { return clip.EndOr(0, f.dur, f.dur > 0) }
func (f *fakeSource) WithStart(clip.Time) clip.Clip      { c := *f; return &c }
func (f *fakeSource) WithDuration(d clip.Time) clip.Clip { c := *f; c.dur = d; return &c }
func (f *fakeSource) WithEnd(clip.Time, bool) clip.Clip  { c := *f; return &c }
func (f *fakeSource) Close() error                       { return nil }
func (f *fakeSource) Size() clip.Size                    { return f.size }
func (f *fakeSource) Rate() (clip.Rate, bool)            { return f.rate, f.rate.Num != 0 }
func (f *fakeSource) HasMask() bool                      { return f.mask }
func (f *fakeSource) Audio() audio.AudioClip             { return nil }
func (f *fakeSource) ParallelSafe() bool                 { return false }
func (f *fakeSource) SourceAccess() video.AccessClass    { return video.AccessLinear }
func (f *fakeSource) RenderInto(context.Context, clip.Time, *clip.Frame, *clip.Frame) (bool, error) {
	return false, nil
}
func (f *fakeSource) FrameInto(context.Context, clip.Time, *clip.Frame) error { return nil }
func (f *fakeSource) MaskInto(context.Context, clip.Time, *clip.Frame) (bool, error) {
	return false, nil
}

func (f *fakeSource) FilterInput(ffmpeg.FilterContext) (ffmpeg.Input, bool) {
	return ffmpeg.Input{Name: "src.mp4"}, true
}

type progressCounter struct {
	steps int
}

func (p *progressCounter) SetTotal(int) {}
func (p *progressCounter) Step()        { p.steps++ }

type errAfterReader struct {
	data string
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if r.data != "" {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	if r.err != nil {
		err := r.err
		r.err = nil
		return 0, err
	}
	return 0, io.EOF
}

func newFakeSource() *fakeSource {
	return &fakeSource{size: clip.Size{W: 64, H: 48}, rate: clip.Rate{Num: 25, Den: 1}, dur: 4 * time.Second}
}

// TestFuseLinearChainGolden asserts a forward subclip + crop over a file source
// fuses into the expected labeled filtergraph string.
func TestFuseLinearChainGolden(t *testing.T) {
	src := newFakeSource()
	clipped := video.Subclip(src, time.Second, 3*time.Second)
	cropped, err := effect.Crop{X: 4, Y: 4, W: 32, H: 24}.ApplyVideo(clipped)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}

	g, ok, reason := fuse(cropped, clip.Rate{Num: 25, Den: 1})
	if !ok {
		t.Fatalf("expected fusion, got fallback: %s", reason)
	}
	want := "[0:v]trim=start=1:duration=2,setpts=PTS-STARTPTS[v1];[v1]crop=32:24:4:4[v2];[v2]fps=25/1[v3]"
	if got := g.FilterComplex(); got != want {
		t.Errorf("filter_complex:\n got %q\nwant %q", got, want)
	}
	if g.OutVideo != "v3" {
		t.Errorf("OutVideo = %q, want v3", g.OutVideo)
	}
	if len(g.Inputs) != 1 || g.Inputs[0].Name != "src.mp4" {
		t.Errorf("inputs = %+v, want one src.mp4", g.Inputs)
	}
}

// TestFuseBareSourceTranscodes asserts a plain file (no effects) still fuses to
// an fps-normalizing transcode rather than falling back.
func TestFuseBareSourceTranscodes(t *testing.T) {
	g, ok, reason := fuse(newFakeSource(), clip.Rate{Num: 30, Den: 1})
	if !ok {
		t.Fatalf("expected fusion for a bare source, got: %s", reason)
	}
	if got := g.FilterComplex(); got != "[0:v]fps=30/1[v1]" {
		t.Errorf("filter_complex = %q", got)
	}
}

func TestReportFFmpegProgressReturnsScannerError(t *testing.T) {
	wantErr := errors.New("progress read failed")
	prog := &progressCounter{}

	last, err := reportFFmpegProgress(&errAfterReader{
		data: "frame=2\n",
		err:  wantErr,
	}, prog, 10)

	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if last != 2 {
		t.Errorf("last = %d, want 2", last)
	}
	if prog.steps != 2 {
		t.Errorf("steps = %d, want 2", prog.steps)
	}
}

// TestFuseRejectsTransparent is the explicit v1 rule: a masked (transparent)
// output is never fused.
func TestFuseRejectsTransparent(t *testing.T) {
	src := newFakeSource()
	src.mask = true
	if _, ok, reason := fuse(src, clip.Rate{Num: 25, Den: 1}); ok {
		t.Fatal("transparent output must not fuse")
	} else if !strings.Contains(reason, "transparent") {
		t.Errorf("reason = %q, want it to mention transparency", reason)
	}
}

// TestFuseRejectsComposite asserts a multi-layer composite falls back to Go
// rendering (single-source chains only in v1).
func TestFuseRejectsComposite(t *testing.T) {
	a, b := newFakeSource(), newFakeSource()
	comp := composite.New([]composite.CompositeChild{{Clip: a}, {Clip: b, Layer: 1}}, composite.Options{})
	if _, ok, reason := fuse(comp, clip.Rate{Num: 25, Den: 1}); ok {
		t.Fatal("composite must not fuse in v1")
	} else if !strings.Contains(reason, "not FFmpeg-expressible") {
		t.Errorf("reason = %q, want it to mention non-expressibility", reason)
	}
}

// TestFuseRejectsQuarterTurnRotate confirms a quarter-turn rotation (the
// lossless Go transpose, which advertises no filter) forces the Go path, even
// though the rest of the chain is expressible.
func TestFuseRejectsQuarterTurnRotate(t *testing.T) {
	rotated, err := effect.Rotate{Degrees: 90}.ApplyVideo(newFakeSource())
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, ok, _ := fuse(rotated, clip.Rate{Num: 25, Den: 1}); ok {
		t.Fatal("a quarter-turn rotated chain must not fuse in v1")
	}
}

// TestFuseAcceptsArbitraryRotate confirms a non-quarter rotation does advertise
// rotate and fuses, emitting the expanded (even) canvas as ow/oh — the path the
// filter string-builder test alone never exercises through fuse().
func TestFuseAcceptsArbitraryRotate(t *testing.T) {
	rotated, err := effect.Rotate{Degrees: 30, Expand: true}.ApplyVideo(newFakeSource())
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	g, ok, reason := fuse(rotated, clip.Rate{Num: 25, Den: 1})
	if !ok {
		t.Fatalf("expected arbitrary rotate to fuse, got fallback: %s", reason)
	}
	// 64x48 @30deg -> ceil(79.43)=80, ceil(73.57)=74, both already even.
	got := g.FilterComplex()
	if !strings.Contains(got, "rotate=a=") || !strings.Contains(got, "ow=80:oh=74") {
		t.Errorf("filter_complex = %q, want a rotate=a=...:ow=80:oh=74 stage", got)
	}
}

// TestFuseRejectsNonExpressibleEffect checks an effect with no filter
// advertisement (black-and-white) declines fusion for the whole chain.
func TestFuseRejectsNonExpressibleEffect(t *testing.T) {
	bw, err := effect.BlackAndWhite{}.ApplyVideo(newFakeSource())
	if err != nil {
		t.Fatalf("bw: %v", err)
	}
	if _, ok, reason := fuse(bw, clip.Rate{Num: 25, Den: 1}); ok {
		t.Fatal("a non-expressible effect must not fuse")
	} else if !strings.Contains(reason, "declined fusion") {
		t.Errorf("reason = %q, want it to mention the declined wrapper", reason)
	}
}
