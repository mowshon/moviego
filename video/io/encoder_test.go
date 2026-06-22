package videoio

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
}

// fillFrame paints a frame a solid color derived from i so successive frames
// differ slightly.
func fillFrame(f *clip.Frame, i int) {
	for p := 0; p < len(f.Pix); p += 3 {
		f.Pix[p] = byte(i)
		f.Pix[p+1] = byte(i * 2)
		f.Pix[p+2] = byte(255 - i)
	}
}

func TestEncodeProducesPlayableFile(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mp4")
	size := clip.Size{W: 32, H: 32}
	rate := clip.Rate{Num: 30, Den: 1}

	enc, err := OpenEncoder(context.Background(), out, EncoderOptions{Size: size, Rate: rate})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	for i := 0; i < 30; i++ {
		fillFrame(f, i)
		if err := enc.WriteFrame(f); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil || info.Video.Size != size {
		t.Fatalf("video = %+v, want %v", info.Video, size)
	}
	if d := info.Duration; d < 900*time.Millisecond || d > 1100*time.Millisecond {
		t.Errorf("duration = %v, want ~1s", d)
	}
}

// TestEncodeWithQualityFlags exercises the CRF/Threads passthrough: a
// constant-quality encode with a bounded thread count still produces a valid
// file (FFmpeg rejects an unknown flag, so a clean probe confirms the flags were
// accepted).
func TestEncodeWithQualityFlags(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mp4")
	size := clip.Size{W: 32, H: 32}
	rate := clip.Rate{Num: 30, Den: 1}

	enc, err := OpenEncoder(context.Background(), out, EncoderOptions{
		Size: size, Rate: rate, CRF: 28, Threads: 2,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	for i := 0; i < 30; i++ {
		fillFrame(f, i)
		if err := enc.WriteFrame(f); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil || info.Video.Size != size {
		t.Fatalf("video = %+v, want %v", info.Video, size)
	}
}

func TestEncoderStderrError(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mp4")
	size := clip.Size{W: 16, H: 16}

	enc, err := OpenEncoder(context.Background(), out, EncoderOptions{
		Size:  size,
		Rate:  clip.Rate{Num: 30, Den: 1},
		Codec: "definitely_not_a_codec",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	// FFmpeg rejects the codec and exits; writes may fail on a broken pipe and
	// Close must surface the failure.
	for i := 0; i < 5; i++ {
		fillFrame(f, i)
		if enc.WriteFrame(f) != nil {
			break
		}
	}
	if err := enc.Close(); err == nil {
		t.Fatal("expected error from bad codec")
	}
}

func TestEncoderOddDimensions(t *testing.T) {
	// An odd size with the default yuv420p must fail fast with a typed error,
	// before FFmpeg is launched (so this needs no toolchain).
	_, err := OpenEncoder(context.Background(), "out.mp4", EncoderOptions{
		Size: clip.Size{W: 15, H: 16},
		Rate: clip.Rate{Num: 30, Den: 1},
	})
	if !errors.Is(err, ErrOddSize) {
		t.Errorf("error = %v, want ErrOddSize", err)
	}
}

// TestEncodeTransparentDefaultRoundTripsAlpha encodes an RGBA stream with the
// default transparent codec, then decodes it back and asserts the alpha values
// actually survived — proving the mask is not silently dropped.
func TestEncodeTransparentDefaultRoundTripsAlpha(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mov") // qtrle default lives in .mov
	size := clip.Size{W: 32, H: 32}
	enc, err := OpenEncoder(context.Background(), out, EncoderOptions{
		Size:        size,
		Rate:        clip.Rate{Num: 30, Den: 1},
		Transparent: true, // no codec => qtrle, the alpha default
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f := clip.NewFrame(size.W, size.H, clip.RGBA)
	for p := 0; p < len(f.Pix); p += 4 {
		f.Pix[p], f.Pix[p+3] = 200, 100 // red 200, alpha 100
	}
	for i := 0; i < 5; i++ {
		if err := enc.WriteFrame(f); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	alphas := decodeFirstFrameAlpha(t, out, size)
	for _, a := range alphas {
		if a != 100 {
			t.Fatalf("decoded alpha = %d, want 100 (alpha was dropped on round-trip)", a)
		}
	}
}

// decodeFirstFrameAlpha decodes path's first frame back to RGBA and returns its
// alpha channel, so a test can prove alpha survived an encode round-trip.
func decodeFirstFrameAlpha(t *testing.T, path string, size clip.Size) []byte {
	t.Helper()
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		t.Fatalf("ffmpeg path: %v", err)
	}
	cmd := exec.Command(bin, "-v", "error", "-i", path, "-f", "rawvideo", "-pix_fmt", "rgba", "-")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	frameBytes := size.W * size.H * 4
	if len(raw) < frameBytes {
		t.Fatalf("decoded %d bytes, want at least one %d-byte frame", len(raw), frameBytes)
	}
	alphas := make([]byte, 0, size.W*size.H)
	for i := 3; i < frameBytes; i += 4 {
		alphas = append(alphas, raw[i])
	}
	return alphas
}

// TestEncoderTransparentRejectsOpaqueCodec: a transparent export with a codec
// not known to carry alpha is rejected rather than silently dropping the mask.
func TestEncoderTransparentRejectsOpaqueCodec(t *testing.T) {
	_, err := OpenEncoder(context.Background(), "out.mp4", EncoderOptions{
		Size:        clip.Size{W: 16, H: 16},
		Rate:        clip.Rate{Num: 30, Den: 1},
		Codec:       "libx264",
		Transparent: true,
	})
	if !errors.Is(err, ErrAlphaUnsupported) {
		t.Errorf("error = %v, want ErrAlphaUnsupported", err)
	}
}

// TestEncoderTransparentExplicitPixFmtTrusted: an explicit PixFmt is the escape
// hatch — it is accepted even with a non-VPX codec (no error before launch).
func TestEncoderTransparentExplicitPixFmtAllowed(t *testing.T) {
	enc, err := OpenEncoder(context.Background(), filepath.Join(t.TempDir(), "out.mov"), EncoderOptions{
		Size:        clip.Size{W: 16, H: 16},
		Rate:        clip.Rate{Num: 30, Den: 1},
		Codec:       "prores_ks",
		PixFmt:      "yuva444p10le",
		Transparent: true,
	})
	if err != nil {
		// Only the alpha-capability gate is under test; tolerate a missing codec
		// in this FFmpeg build, but never the capability rejection.
		if errors.Is(err, ErrAlphaUnsupported) {
			t.Fatalf("explicit PixFmt should bypass the capability gate, got %v", err)
		}
		return
	}
	_ = enc.Close()
}

// TestEncoderExplicitNonAlphaPixFmtRejected: the escape hatch must still request
// an alpha format — a non-alpha PixFmt is rejected before launch (no toolchain).
func TestEncoderExplicitNonAlphaPixFmtRejected(t *testing.T) {
	_, err := OpenEncoder(context.Background(), "out.mov", EncoderOptions{
		Size:        clip.Size{W: 16, H: 16},
		Rate:        clip.Rate{Num: 30, Den: 1},
		Codec:       "qtrle",
		PixFmt:      "yuv420p", // opaque
		Transparent: true,
	})
	if !errors.Is(err, ErrPixFmtNoAlpha) {
		t.Errorf("error = %v, want ErrPixFmtNoAlpha", err)
	}
}

// TestEncoderExplicitPixFmtUnsupportedByCodecRejected: the documented silent
// drop — Codec libx264 + PixFmt yuva420p — must be rejected by the encoder
// capability check, since libx264 does not list yuva420p and FFmpeg would
// auto-select opaque yuv420p.
func TestEncoderExplicitPixFmtUnsupportedByCodecRejected(t *testing.T) {
	requireFFmpeg(t) // capability check shells out to ffmpeg -h encoder
	_, err := OpenEncoder(context.Background(), filepath.Join(t.TempDir(), "out.mp4"), EncoderOptions{
		Size:        clip.Size{W: 16, H: 16},
		Rate:        clip.Rate{Num: 30, Den: 1},
		Codec:       "libx264",
		PixFmt:      "yuva420p",
		Transparent: true,
	})
	if !errors.Is(err, ErrCodecPixFmt) {
		t.Errorf("error = %v, want ErrCodecPixFmt", err)
	}
}

// TestEncoderSupportsPixFmtConservative locks in the missing-encoder fix: an
// unavailable encoder (FFmpeg prints "not recognized" with exit 0) and an
// unsupported pixfmt must both report unsupported, not "assume true".
func TestEncoderSupportsPixFmtConservative(t *testing.T) {
	requireFFmpeg(t)
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ctx := context.Background()
	if !encoderSupportsPixFmt(ctx, bin, "qtrle", "argb") {
		t.Error("qtrle/argb should be confirmed supported")
	}
	if encoderSupportsPixFmt(ctx, bin, "libx264", "yuva420p") {
		t.Error("libx264/yuva420p must be unsupported (FFmpeg would drop alpha)")
	}
	if encoderSupportsPixFmt(ctx, bin, "not_a_real_encoder", "rgba") {
		t.Error("an unrecognized encoder must report unsupported, not assumed true")
	}
}

// TestEncoderUnknownAlphaEncoderRejected: a transparent export naming an encoder
// this FFmpeg build does not have (reached via an explicit alpha PixFmt) fails
// before launch instead of being treated as supported.
func TestEncoderUnknownAlphaEncoderRejected(t *testing.T) {
	requireFFmpeg(t)
	_, err := OpenEncoder(context.Background(), filepath.Join(t.TempDir(), "out.mp4"), EncoderOptions{
		Size:        clip.Size{W: 16, H: 16},
		Rate:        clip.Rate{Num: 30, Den: 1},
		Codec:       "not_a_real_encoder",
		PixFmt:      "rgba",
		Transparent: true,
	})
	if !errors.Is(err, ErrCodecPixFmt) {
		t.Errorf("error = %v, want ErrCodecPixFmt", err)
	}
}

// TestEncoderHEVCAlphaMP4RoundTrip: the opt-in web-friendly transparent .mp4
// path (HEVC + alpha via VideoToolbox) preserves alpha. Skipped on builds where
// the encoder is unavailable (e.g. Linux without VideoToolbox).
func TestEncoderHEVCAlphaMP4RoundTrip(t *testing.T) {
	requireFFmpeg(t)
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	if !encoderSupportsPixFmt(context.Background(), bin, "hevc_videotoolbox", "bgra") {
		t.Skip("hevc_videotoolbox alpha not available on this build")
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	size := clip.Size{W: 32, H: 32}
	enc, err := OpenEncoder(context.Background(), out, EncoderOptions{
		Size:        size,
		Rate:        clip.Rate{Num: 30, Den: 1},
		Codec:       "hevc_videotoolbox", // opt-in transparent MP4
		Transparent: true,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f := clip.NewFrame(size.W, size.H, clip.RGBA)
	for p := 0; p < len(f.Pix); p += 4 {
		f.Pix[p], f.Pix[p+3] = 200, 100
	}
	for i := 0; i < 8; i++ {
		if err := enc.WriteFrame(f); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for _, a := range decodeFirstFrameAlpha(t, out, size) {
		if a < 95 || a > 105 { // HEVC alpha at quality 1 is near-lossless
			t.Fatalf("decoded alpha = %d, want ~100", a)
		}
	}
}

// TestEncoderTransparentOddDims confirms the even-dimension gate also applies to
// a chroma-subsampled alpha (yuva420p) path, failing fast before launching
// FFmpeg. yuva420p is now only reachable via an explicit Codec+PixFmt.
func TestEncoderTransparentOddDims(t *testing.T) {
	_, err := OpenEncoder(context.Background(), "out.webm", EncoderOptions{
		Size:        clip.Size{W: 33, H: 32},
		Rate:        clip.Rate{Num: 30, Den: 1},
		Codec:       "libvpx-vp9",
		PixFmt:      "yuva420p",
		Transparent: true,
	})
	if !errors.Is(err, ErrOddSize) {
		t.Errorf("error = %v, want ErrOddSize", err)
	}
}

// TestEncoderTransparentMP4Rejected: a transparent export to a container with no
// safe alpha default (and no codec named) fails early instead of auto-picking a
// codec the container cannot mux (qtrle-in-mp4) or one that drops alpha.
func TestEncoderTransparentMP4Rejected(t *testing.T) {
	for _, ext := range []string{"out.mp4", "out.webm", "out.avi"} {
		_, err := OpenEncoder(context.Background(), ext, EncoderOptions{
			Size:        clip.Size{W: 16, H: 16},
			Rate:        clip.Rate{Num: 30, Den: 1},
			Transparent: true, // no codec
		})
		if !errors.Is(err, ErrAlphaContainer) {
			t.Errorf("%s: error = %v, want ErrAlphaContainer", ext, err)
		}
	}
}

// TestEncoderTransparentContainerDefaults: alpha containers resolve to their
// known-good codec without an explicit Codec.
func TestEncoderTransparentContainerDefaults(t *testing.T) {
	requireFFmpeg(t)
	for ext, want := range map[string]clip.Size{"out.mov": {W: 16, H: 16}, "out.mkv": {W: 16, H: 16}} {
		out := filepath.Join(t.TempDir(), ext)
		enc, err := OpenEncoder(context.Background(), out, EncoderOptions{
			Size:        want,
			Rate:        clip.Rate{Num: 30, Den: 1},
			Transparent: true,
		})
		if err != nil {
			t.Fatalf("%s open: %v", ext, err)
		}
		f := clip.NewFrame(want.W, want.H, clip.RGBA)
		if err := enc.WriteFrame(f); err != nil {
			t.Fatalf("%s write: %v", ext, err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("%s close: %v", ext, err)
		}
	}
}

// TestEncoderRejectsWrongFormat: a transparent encoder rejects an RGB24 frame
// (it expects RGBA).
func TestEncoderRejectsWrongFormat(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mov")
	enc, err := OpenEncoder(context.Background(), out, EncoderOptions{
		Size:        clip.Size{W: 16, H: 16},
		Rate:        clip.Rate{Num: 30, Den: 1},
		Transparent: true, // qtrle default
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer enc.Close()
	if err := enc.WriteFrame(clip.NewFrame(16, 16, clip.RGB24)); err == nil {
		t.Error("expected a format-mismatch error feeding RGB24 to a transparent encoder")
	}
}

func TestEncoderCancellation(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mp4")
	size := clip.Size{W: 64, H: 64}
	ctx, cancel := context.WithCancel(context.Background())

	enc, err := OpenEncoder(ctx, out, EncoderOptions{Size: size, Rate: clip.Rate{Num: 30, Den: 1}})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	cancel()

	// After cancellation, writing then closing must report an error rather than
	// silently succeeding.
	var writeErr error
	for i := 0; i < 1000 && writeErr == nil; i++ {
		fillFrame(f, i)
		writeErr = enc.WriteFrame(f)
	}
	closeErr := enc.Close()
	if writeErr == nil && closeErr == nil {
		t.Fatal("expected error after cancellation")
	}
}
