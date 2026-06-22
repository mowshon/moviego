package render

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// Defaults for the fused encode, matching the Go encoder so a fused export and
// a Go export of the same graph use the same codec/preset/pixfmt.
const (
	fuseDefaultCodec  = "libx264"
	fuseDefaultPreset = "medium"
	fuseDefaultPixFmt = "yuv420p"
)

// runFFmpegOnly executes a fused plan as a single FFmpeg invocation: the
// source(s) and the pre-rendered audio are inputs, the planner's filtergraph
// produces the video stream, and FFmpeg encodes and muxes in one pass. The
// output is capped to the scheduled frame count so a fused export and a Go
// export agree on length (and both stop early at source EOF). Canceling ctx
// kills the process; a failure removes any partial output.
func runFFmpegOnly(ctx context.Context, plan *Plan, out string, opts ExportOptions, audioFile string) error {
	g := plan.fusion

	codec := opts.Codec
	if codec == "" {
		if hw, ok := hardwareCodec(opts.HWAccel); ok {
			codec = hw
		} else {
			codec = fuseDefaultCodec
		}
	}
	preset := orFuseDefault(opts.Preset, fuseDefaultPreset)
	pixfmt := orFuseDefault(opts.PixFmt, fuseDefaultPixFmt)

	// Reuse the Go encoder's even-dimension guard so an odd frame with a
	// chroma-subsampled format fails fast with the same typed error rather than
	// as an opaque FFmpeg stderr tail.
	if videoio.RequiresEvenDims(pixfmt) && (plan.Size.W%2 != 0 || plan.Size.H%2 != 0) {
		return clip.Wrap("write "+out, videoio.ErrOddSize)
	}

	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		return err
	}

	args := []string{"-nostdin", "-loglevel", "error", "-y"}
	// Stream machine-readable progress to stdout so the per-frame Progress
	// contract is honored on the fused path too (FFmpeg writes periodic
	// "frame=N" blocks). -nostats keeps the human stats off stderr.
	args = append(args, "-progress", "pipe:1", "-nostats")
	args = append(args, g.InputArgs()...)
	if audioFile != "" {
		args = append(args, "-i", ffmpeg.SafeInputPath(audioFile))
	}
	args = append(args,
		"-filter_complex", g.FilterComplex(),
		"-map", "["+g.OutVideo+"]",
	)
	if audioFile != "" {
		// The audio is the input that follows the filtergraph sources.
		args = append(args, "-map", fmt.Sprintf("%d:a:0", len(g.Inputs)))
	} else {
		args = append(args, "-an")
	}
	// Cap the video to the scheduled frame count (floor policy); FFmpeg stops
	// earlier at source EOF, matching the Go engine's behavior exactly.
	args = append(args, "-frames:v", strconv.Itoa(plan.Frames))
	args = append(args, "-vcodec", codec)
	if ffmpeg.EncoderUsesPreset(codec) {
		args = append(args, "-preset", preset)
	}
	args = append(args, "-pix_fmt", pixfmt)
	// Honor the quality/thread knobs on the fused path too, matching the Go
	// encoder; without these, EnableFusion silently dropped them.
	if opts.CRF > 0 {
		args = append(args, "-crf", strconv.Itoa(opts.CRF))
	}
	if opts.Bitrate != "" {
		args = append(args, "-b:v", opts.Bitrate)
	}
	if opts.Threads > 0 {
		args = append(args, "-threads", strconv.Itoa(opts.Threads))
	}
	if audioFile != "" {
		args = append(args, "-c:a", "copy", "-shortest")
	}
	args = append(args, opts.ExtraOutputArgs...)
	args = append(args, ffmpeg.SafeInputPath(out))

	prog := opts.Progress
	if prog == nil {
		prog = NopProgress{}
	}
	prog.SetTotal(plan.Frames)

	proc, err := ffmpeg.Start(ctx, ffmpeg.Spec{Path: bin, Args: args, Stdout: true})
	if err != nil {
		return clip.Wrap("write "+out, err)
	}
	// Drain the progress stream to completion before Wait: os/exec closes the
	// stdout pipe once the process exits, so the reader must finish first. The
	// process runs to its own EOF (no stdin to close), so this never deadlocks.
	var lastFrame int
	var progressErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		lastFrame, progressErr = reportFFmpegProgress(proc.Stdout, prog, plan.Frames)
	}()
	<-done
	if err := proc.Wait(); err != nil {
		_ = os.Remove(out)
		// Prefer the context error so callers can use errors.Is(err, context.Canceled)
		// consistently across engines, matching the Go-engine behavior.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return clip.Wrap("write "+out, err)
	}
	if progressErr != nil {
		return clip.Wrap("read ffmpeg progress", progressErr)
	}
	// The scheduled count is an upper bound: a source that ends before its
	// declared duration writes fewer frames. Mirror the Go path and re-publish
	// the real total so a reporter can correct its denominator to 100%.
	if lastFrame > 0 && lastFrame != plan.Frames {
		prog.SetTotal(lastFrame)
	}
	return nil
}

// reportFFmpegProgress reads FFmpeg's -progress key=value stream and advances
// prog by the delta in the reported "frame=N" count, capped at total (the
// scheduled frame count). It returns the last frame count seen (the frames
// actually encoded) once the stream reaches EOF (process exit).
func reportFFmpegProgress(r io.Reader, prog Progress, total int) (int, error) {
	if r == nil {
		return 0, nil
	}
	last := 0
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "frame=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		if n > total {
			n = total
		}
		for ; last < n; last++ {
			prog.Step()
		}
	}
	err := sc.Err()
	// Drain any reader remainder so the pipe is fully consumed.
	_, _ = io.Copy(io.Discard, r)
	return last, err
}

// hardwareCodec maps a hardware-accel family to its H.264 encoder. It returns
// ok=false for an unset or unknown family, leaving codec selection to the
// default. Availability is verified by FFmpeg at launch; an unconfigured
// encoder surfaces as a normal FFmpeg error.
func hardwareCodec(hwaccel string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(hwaccel)) {
	case "nvenc", "cuda":
		return "h264_nvenc", true
	case "qsv":
		return "h264_qsv", true
	case "videotoolbox", "vt":
		return "h264_videotoolbox", true
	default:
		return "", false
	}
}

func orFuseDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
