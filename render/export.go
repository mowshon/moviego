package render

import (
	"context"
	"errors"
	"os"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// ExportOptions controls a video export. A zero Rate uses the clip's own rate;
// codec, preset, and pixel format fall back to the encoder defaults. Workers
// caps the render worker pool (0 = automatic from GOMAXPROCS); Progress, when
// set, receives per-frame callbacks.
type ExportOptions struct {
	Rate     clip.Rate
	Codec    string
	Preset   string
	PixFmt   string
	Workers  int
	Progress Progress

	// CRF sets a constant-quality target (lower = better, e.g. 18–28 for x264);
	// Bitrate sets a target video bitrate (e.g. "4M"); Threads caps the FFmpeg
	// encoder threads (0 = FFmpeg default). Each is forwarded to the encoder only
	// when set, and CRF/Bitrate are codec-dependent.
	CRF     int
	Bitrate string
	Threads int

	// ExtraOutputArgs are appended to the FFmpeg command immediately before the
	// output path. Use them for advanced output-side FFmpeg options not modeled
	// above, such as muxer/container flags (for example: []string{"-f", "hls",
	// "-hls_time", "3"}). They are not for input, global, or filtergraph args.
	ExtraOutputArgs []string

	// DisableAudio skips audio rendering and mux even when the clip has an audio
	// sidecar. AudioCodec overrides the temp audio codec (default chosen from the
	// output container); AudioBitrate sets the temp audio bitrate (e.g. "192k").
	DisableAudio bool
	AudioCodec   string
	AudioBitrate string

	// EnableFusion opts into the FFmpeg-only fast path: when set and the whole
	// graph is FFmpeg-expressible (a single linear file source through
	// trim/speed/scale/crop/fade), the export runs as one FFmpeg filtergraph
	// invocation instead of the Go decode/encode pipeline, skipping the rawvideo
	// round-trip. It is OFF by default — a plain export always uses the Go
	// engines (the correctness oracle), which keeps the documented behavior
	// (e.g. the CatmullRom resize kernel) and avoids the fused path's
	// divergences. A graph that cannot be fused falls back to the Go engine even
	// when this is set, with the reason reported via Debugf. See
	// docs/compatibility.md for the fusion scope and divergences.
	EnableFusion bool

	// HWAccel names a hardware encoder family (nvenc, qsv, videotoolbox); when
	// set and no explicit Codec is given, the planner selects the matching
	// hardware H.264 encoder. It is opt-in and validated before launch.
	HWAccel string

	// Debugf, when set, receives the planner's engine and fusion decision, so a
	// caller can see whether an export used Go rendering or FFmpeg-only and why.
	Debugf func(format string, args ...any)
}

// WriteVideo renders v and encodes it to out (no audio). The planner validates
// the timeline, classifies the graph, and picks the sequential or parallel
// engine; the output rate comes from opts.Rate, or the clip's rate when unset.
// Canceling ctx stops the render and kills FFmpeg.
func WriteVideo(ctx context.Context, v video.VideoClip, out string, opts ExportOptions) error {
	plan, err := BuildPlan(v, planOptions{
		rate:         opts.Rate,
		workers:      opts.Workers,
		enableFusion: opts.EnableFusion,
		debugf:       opts.Debugf,
	})
	if err != nil {
		// Validation failed before any file was touched (duration/rate).
		return clip.Wrap("write "+out, err)
	}

	// Render audio to a temp file first (MoviePy order), then mux it during the
	// video encode. A failure here aborts before any output file is created.
	audioFile, cleanupAudio, err := renderAudio(ctx, v, out, opts)
	if err != nil {
		return clip.Wrap("write "+out, err)
	}
	defer cleanupAudio()

	// FFmpeg-only fast path: one invocation runs the filtergraph and muxes the
	// pre-rendered audio, with no Go decode/encode pipeline at all.
	if plan.Engine == EngineFFmpegOnly {
		return runFFmpegOnly(ctx, plan, out, opts, audioFile)
	}

	// Resolve a hardware encoder for the Go path too, so HWAccel works whether or
	// not the graph fused. Only when no explicit codec is set and the output is
	// opaque (hardware alpha is a separate, codec-specific concern).
	codec := opts.Codec
	if codec == "" && !plan.Transparent {
		if hw, ok := hardwareCodec(opts.HWAccel); ok {
			codec = hw
		}
	}

	enc, err := videoio.OpenEncoder(ctx, out, videoio.EncoderOptions{
		Size:        plan.Size,
		Rate:        plan.Rate,
		Codec:       codec,
		Preset:      opts.Preset,
		PixFmt:      opts.PixFmt,
		Transparent: plan.Transparent,
		CRF:         opts.CRF,
		Bitrate:     opts.Bitrate,
		Threads:     opts.Threads,
		OutputArgs:  opts.ExtraOutputArgs,
		AudioFile:   audioFile,
	})
	if err != nil {
		return err
	}

	prog := opts.Progress
	if prog == nil {
		prog = NopProgress{}
	}
	prog.SetTotal(plan.Frames)
	sink := newEncoderSink(enc, plan, prog)

	var runErr error
	switch plan.Engine {
	case EnginePipeline:
		runErr = runPipeline(ctx, v, plan, sink)
	default:
		runErr = runSequential(ctx, v, plan, sink)
	}

	// The scheduled count is an upper bound: a source that ends before its
	// declared duration stops the render early. Re-publish the real total so a
	// reporter can correct its denominator. SetTotal is the only revision hook
	// the interface offers, so whether this becomes a visible 100% is up to the
	// implementation (it must redraw on a revised total); the engine guarantees
	// only that the final total reflects the frames actually written.
	if runErr == nil && sink.written != plan.Frames {
		prog.SetTotal(sink.written)
	}

	// Always close the encoder, and keep both errors: when a write fails on a
	// broken pipe, Close carries the FFmpeg stderr tail that explains why.
	closeErr := enc.Close()
	if err := errors.Join(runErr, closeErr); err != nil {
		// A failed export must not leave a misleading partial file behind.
		_ = os.Remove(out)
		return err
	}
	return nil
}

// encoderSink writes rendered frames to the FFmpeg encoder in output order. A
// transparent clip is fed as RGBA: the RGB frame and its alpha sidecar are
// stacked into one reused scratch buffer so no per-frame allocation happens. A
// second reused buffer holds an all-opaque alpha for frames a transparent clip
// reports as fully opaque, keeping even the fallback path allocation-free.
type encoderSink struct {
	enc         *videoio.Encoder
	transparent bool
	rgba        *clip.Frame
	opaqueAlpha *clip.Frame
	progress    Progress
	written     int // frames successfully encoded (for the final total revision)
}

func newEncoderSink(enc *videoio.Encoder, plan *Plan, prog Progress) *encoderSink {
	s := &encoderSink{enc: enc, transparent: plan.Transparent, progress: prog}
	if plan.Transparent {
		s.rgba = clip.NewFrame(plan.Size.W, plan.Size.H, clip.RGBA)
		s.opaqueAlpha = clip.NewFrame(plan.Size.W, plan.Size.H, clip.Gray8)
		for i := range s.opaqueAlpha.Pix {
			s.opaqueAlpha.Pix[i] = 255
		}
	}
	return s
}

func (s *encoderSink) writeFrame(rf clip.RenderedFrame) error {
	if !s.transparent {
		if err := s.enc.WriteFrame(rf.RGB); err != nil {
			return err
		}
		s.written++
		s.progress.Step()
		return nil
	}
	alpha := rf.Alpha
	if alpha == nil {
		alpha = s.opaqueAlpha
	}
	if err := imagex.StackAlphaInto(s.rgba, rf.RGB, alpha); err != nil {
		return err
	}
	if err := s.enc.WriteFrame(s.rgba); err != nil {
		return err
	}
	s.written++
	s.progress.Step()
	return nil
}
