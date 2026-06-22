package videoio

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
)

// Default encode settings used when EncoderOptions leaves a field empty.
const (
	defaultCodec  = "libx264"
	defaultPreset = "medium"
	defaultPixFmt = "yuv420p"
)

// alphaCodecPixFmt maps codecs that reliably round-trip alpha to the output
// pixel format that preserves it. An empty value means "let FFmpeg choose" (the
// codec has a single natural alpha format, e.g. qtrle -> argb). A transparent
// export whose codec is not in this table (and has no explicit PixFmt) is
// rejected, so alpha is never silently dropped by an opaque fallback.
//
// VPX (libvpx / libvpx-vp9) is deliberately NOT here: on some FFmpeg builds it
// accepts yuva420p but drops alpha on decode, so it must not be advertised as
// safe. A caller who has verified VPX alpha on their build can still reach it
// via an explicit Codec + PixFmt (the escape hatch).
//
// hevc_videotoolbox is the opt-in web-friendly transparent-MP4 path (HEVC with
// alpha, `hvc1`, played by Safari/QuickTime). It is VideoToolbox-only (macOS),
// so it is never a default — the caller must name it — and OpenEncoder
// capability-checks it before launch so it fails clearly where unavailable.
var alphaCodecPixFmt = map[string]string{
	"qtrle":             "",             // lossless RGB+alpha (.mov); FFmpeg picks argb
	"png":               "rgba",         // lossless (.mov/.mkv)
	"apng":              "rgba",         // lossless (.apng)
	"ffv1":              "bgra",         // lossless (.mkv/.nut); ffv1's 8-bit RGB-alpha format
	"prores_ks":         "yuva444p10le", // ProRes 4444 (.mov)
	"hevc_videotoolbox": "bgra",         // HEVC+alpha for .mp4/.mov (macOS, opt-in)
}

// transparentCodecByExt maps an output container extension to the default alpha
// codec for that container. Only containers known to carry alpha appear here; a
// missing extension (e.g. .mp4, .webm) has no safe default and is rejected.
var transparentCodecByExt = map[string]string{
	".mov": "qtrle", // QuickTime Animation, lossless RGB+alpha
	".mkv": "ffv1",  // Matroska + lossless FFV1 RGBA
	".nut": "ffv1",
}

// ErrOddSize reports an odd width or height with a chroma-subsampled output
// pixel format (e.g. yuv420p), which FFmpeg cannot encode.
var ErrOddSize = errors.New("odd frame dimensions for chroma-subsampled pixel format")

// ErrAlphaUnsupported reports a transparent export whose explicitly named codec
// is not known to carry alpha. Emitting it anyway would let FFmpeg auto-select
// an opaque format and silently discard the mask. Use a known alpha codec
// (qtrle, png, apng, ffv1, prores_ks; hevc_videotoolbox for .mp4 on macOS) or
// set PixFmt explicitly.
var ErrAlphaUnsupported = errors.New("codec does not carry an alpha channel; use an alpha codec (qtrle, png, apng, ffv1, prores_ks; hevc_videotoolbox for .mp4 on macOS) or set PixFmt explicitly")

// ErrAlphaContainer reports a transparent export to a container that has no safe
// alpha default (e.g. .mp4, .webm) with no codec named. Rather than auto-pick a
// codec the container cannot mux (or one whose alpha silently drops), the export
// fails early so the caller can choose an alpha container or flatten the clip.
var ErrAlphaContainer = errors.New("output container does not support alpha by default; export to .mov (qtrle) or .mkv (ffv1), set an explicit alpha Codec+PixFmt, or flatten onto an opaque background")

// ErrPixFmtNoAlpha reports an explicit transparent PixFmt that is not an alpha
// pixel format. The escape hatch still must request a format that carries alpha,
// or the export would discard the mask.
var ErrPixFmtNoAlpha = errors.New("explicit PixFmt does not carry an alpha channel")

// ErrCodecPixFmt reports that the chosen codec's encoder does not list the
// requested (alpha) pixel format. Without this check FFmpeg would auto-select a
// nearby opaque format (e.g. libx264 + yuva420p -> yuv420p) and silently drop
// alpha behind -loglevel error.
var ErrCodecPixFmt = errors.New("codec does not support the requested alpha pixel format")

// EncoderOptions configures OpenEncoder. Size and Rate are required; the codec,
// preset, and output pixel format fall back to libx264/medium/yuv420p. When
// Transparent is set the encoder consumes RGBA frames and emits an alpha-capable
// output: with no codec it picks one from the output container extension
// (.mov->qtrle, .mkv->ffv1; other containers are rejected), and a named codec
// must be alpha-capable or carry an explicit PixFmt — which is then validated
// against the encoder so alpha is never silently dropped. For a web-friendly
// transparent .mp4, set Codec to "hevc_videotoolbox" (HEVC with alpha, macOS
// VideoToolbox only; capability-checked before launch).
type EncoderOptions struct {
	Size        clip.Size
	Rate        clip.Rate
	Codec       string
	Preset      string
	PixFmt      string
	Transparent bool
	// CRF sets a constant-quality target (-crf), Bitrate a target bitrate (-b:v,
	// e.g. "4M"), and Threads the encoder thread count (-threads). Each is emitted
	// only when set; CRF and Bitrate are codec-dependent (libx264/libx265 honor
	// CRF, VideoToolbox honors Bitrate).
	CRF     int
	Bitrate string
	Threads int
	// OutputArgs are appended immediately before the output path. They are for
	// advanced output-side FFmpeg options that do not have dedicated fields.
	OutputArgs []string
	// AudioFile, when non-empty, is muxed in as a second input and stream-copied
	// (-c:a copy) into the output. It is rendered ahead of time to a container-
	// compatible codec, so the encode just multiplexes the elementary stream.
	AudioFile string
}

// Encoder feeds packed rgb24 (or rgba) frames to FFmpeg over stdin and writes
// an encoded, audio-less file. It is not safe for concurrent use.
type Encoder struct {
	ctx        context.Context
	proc       *ffmpeg.Proc
	frameBytes int
	size       clip.Size
	format     clip.PixelFormat
	out        string
	closed     bool
}

// OpenEncoder starts FFmpeg to encode frames written via WriteFrame into out.
// The caller must call Close to flush and finalize the file.
func OpenEncoder(ctx context.Context, out string, opts EncoderOptions) (*Encoder, error) {
	codec := orDefault(opts.Codec, defaultCodec)
	preset := orDefault(opts.Preset, defaultPreset)

	inFormat := clip.RGB24
	inPixFmt := "rgb24"
	pixfmt := orDefault(opts.PixFmt, defaultPixFmt)
	var extra []string
	if opts.Transparent {
		inFormat = clip.RGBA
		inPixFmt = "rgba"
		// A transparent export must not fall back to an opaque codec, nor pick a
		// codec the container cannot mux. With no codec named, choose one from
		// the output container; reject containers (e.g. .mp4, .webm) that have no
		// safe alpha default rather than guessing.
		if opts.Codec == "" {
			c, ok := transparentCodecByExt[strings.ToLower(filepath.Ext(out))]
			if !ok {
				return nil, clip.Wrap("encode "+out, ErrAlphaContainer)
			}
			codec = c
		}
		if opts.PixFmt != "" {
			// An explicit pixel format is the advanced escape hatch, but it is
			// still validated (here, and against the encoder below) so it cannot
			// silently fall back to opaque.
			if !isAlphaPixFmt(opts.PixFmt) {
				return nil, clip.Wrap("encode "+out, ErrPixFmtNoAlpha)
			}
			pixfmt = opts.PixFmt
		} else if p, ok := alphaCodecPixFmt[codec]; ok {
			pixfmt = p // may be "" => let FFmpeg pick the codec's alpha format
		} else {
			return nil, clip.Wrap("encode "+out, ErrAlphaUnsupported)
		}
		extra = append(extra, alphaCodecExtraArgs(codec)...)
	}
	if RequiresEvenDims(pixfmt) && (opts.Size.W%2 != 0 || opts.Size.H%2 != 0) {
		return nil, clip.Wrap("encode "+out, ErrOddSize)
	}
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		return nil, err
	}
	// Verify the encoder actually supports the alpha pixel format before
	// launching: otherwise FFmpeg auto-selects a nearby opaque format and drops
	// the mask with only a warning (hidden by -loglevel error). Skipped when
	// pixfmt is "" (codec picks its own alpha format, e.g. qtrle->argb).
	if opts.Transparent && pixfmt != "" && !encoderSupportsPixFmt(ctx, bin, codec, pixfmt) {
		return nil, clip.Wrap("encode "+out, fmt.Errorf("%w: %s / %s", ErrCodecPixFmt, codec, pixfmt))
	}

	args := []string{
		"-nostdin", "-loglevel", "error", "-y",
		"-f", "rawvideo",
		"-s", sizeArg(opts.Size),
		"-pix_fmt", inPixFmt,
		"-r", rateArg(opts.Rate),
		"-i", "-", // input 0: rawvideo over stdin
	}
	if opts.AudioFile != "" {
		// input 1: the pre-rendered audio; map both streams explicitly and
		// stream-copy the audio so the encode only multiplexes it.
		args = append(args, "-i", ffmpeg.SafeInputPath(opts.AudioFile),
			"-map", "0:v:0", "-map", "1:a:0")
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-vcodec", codec)
	if ffmpeg.EncoderUsesPreset(codec) {
		args = append(args, "-preset", preset)
	}
	if pixfmt != "" {
		args = append(args, "-pix_fmt", pixfmt)
	}
	if opts.CRF > 0 {
		args = append(args, "-crf", strconv.Itoa(opts.CRF))
	}
	if opts.Bitrate != "" {
		args = append(args, "-b:v", opts.Bitrate)
	}
	if opts.Threads > 0 {
		args = append(args, "-threads", strconv.Itoa(opts.Threads))
	}
	args = append(args, extra...)
	if opts.AudioFile != "" {
		// Stream-copy the audio, and -shortest so the mux ends with the shorter
		// stream: the audio is rendered to the clip's declared duration, but a
		// source that ends early writes fewer video frames, and without
		// -shortest that audio would overrun the video tail.
		args = append(args, "-c:a", "copy", "-shortest")
	}
	args = append(args, opts.OutputArgs...)
	args = append(args, ffmpeg.SafeInputPath(out))
	proc, err := ffmpeg.Start(ctx, ffmpeg.Spec{Path: bin, Args: args, Stdin: true})
	if err != nil {
		return nil, err
	}
	return &Encoder{
		ctx:        ctx,
		proc:       proc,
		frameBytes: opts.Size.W * opts.Size.H * inFormat.BytesPerPixel(),
		size:       opts.Size,
		format:     inFormat,
		out:        out,
	}, nil
}

// WriteFrame writes one frame to FFmpeg's stdin. Its format must match the one
// chosen at open (rgb24, or rgba for a transparent encode). A broken pipe means
// FFmpeg has exited; Close surfaces the stderr tail.
func (e *Encoder) WriteFrame(f *clip.Frame) error {
	if e.closed {
		return clip.ErrClosed
	}
	if f.Format != e.format || f.W != e.size.W || f.H != e.size.H || len(f.Pix) < e.frameBytes {
		return clip.Wrap("encode "+e.out, clip.ErrVideoCorrupted)
	}
	if _, err := e.proc.Stdin.Write(f.Pix[:e.frameBytes]); err != nil {
		if e.ctx.Err() != nil {
			return clip.Wrap("encode "+e.out, e.ctx.Err())
		}
		return clip.Wrap("encode "+e.out, err)
	}
	return nil
}

// Close finishes the input stream, waits for FFmpeg, and returns any non-zero
// exit (with the captured stderr tail). It is idempotent.
func (e *Encoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	_ = e.proc.Stdin.Close()
	if err := e.proc.Wait(); err != nil {
		return clip.Wrap("encode "+e.out, err)
	}
	return nil
}

// RequiresEvenDims reports whether a pixel format is chroma-subsampled in a way
// that needs even width and height (the 4:2:0 family). It is the single guard
// shared by the rawvideo encoder and the fused FFmpeg-only path so both reject
// an odd 4:2:0 export with the same typed ErrOddSize instead of an opaque
// FFmpeg stderr tail. (Even-width-only formats like 4:2:2 are not covered yet.)
func RequiresEvenDims(pixfmt string) bool {
	return strings.Contains(pixfmt, "420")
}

// isVPX reports whether the codec is in the libvpx family (VP8/VP9), whose
// alpha output drops without -auto-alt-ref 0.
func isVPX(codec string) bool {
	return strings.Contains(codec, "vpx") || strings.Contains(codec, "vp8") || strings.Contains(codec, "vp9")
}

// alphaCodecExtraArgs returns codec-specific flags needed to actually carry
// alpha through to the output.
func alphaCodecExtraArgs(codec string) []string {
	switch {
	case isVPX(codec):
		// libvpx alpha needs alt-ref frames disabled or alpha is dropped (only
		// reachable via an explicit VPX Codec + PixFmt).
		return []string{"-auto-alt-ref", "0"}
	case codec == "hevc_videotoolbox":
		// VideoToolbox HEVC-with-alpha: enable the alpha plane and tag the track
		// hvc1 so Safari/QuickTime recognize the transparent MP4.
		return []string{"-alpha_quality", "1", "-tag:v", "hvc1"}
	}
	return nil
}

// IsAlphaPixFmt reports whether a pixel-format name carries an alpha channel,
// the exported form of the name heuristic used to derive transparency from a
// probe's pix_fmt (ffprobe does not report a hasAlpha flag).
func IsAlphaPixFmt(p string) bool { return isAlphaPixFmt(p) }

// isAlphaPixFmt reports whether a pixel-format name carries an alpha channel.
// It is a name heuristic over the realistic alpha formats (rgba/bgra/argb/abgr
// and their 64-bit variants, the yuva* and gbra* planar families, ya8/ya16,
// ayuv), used to reject an explicit non-alpha PixFmt on the transparent path.
func isAlphaPixFmt(p string) bool {
	for _, marker := range []string{"rgba", "bgra", "argb", "abgr", "yuva", "gbra", "ya8", "ya16", "ayuv"} {
		if strings.Contains(p, marker) {
			return true
		}
	}
	return false
}

// encoderSupportsPixFmt asks FFmpeg whether codec's encoder lists pixfmt among
// its supported pixel formats (`ffmpeg -h encoder=<codec>`). It is deliberately
// conservative for the transparent path: it returns true ONLY when support is
// confirmed, and false in every ambiguous case — an unavailable encoder ("Codec
// '…' is not recognized by FFmpeg.", which FFmpeg prints to stdout with exit 0),
// a missing pixel-format line, or a failed query. That way a build without the
// requested encoder (e.g. Linux without hevc_videotoolbox) fails clearly before
// launch instead of being treated as supported. All codecs we validate print
// the pixel-format line; codecs that pick their own alpha format (pixfmt "",
// e.g. qtrle) skip this check entirely.
func encoderSupportsPixFmt(ctx context.Context, bin, codec, pixfmt string) bool {
	out, err := exec.CommandContext(ctx, bin, "-hide_banner", "-h", "encoder="+codec).CombinedOutput()
	if err != nil {
		return false // cannot confirm support -> reject conservatively
	}
	text := string(out)
	if strings.Contains(text, "is not recognized") {
		return false // encoder is not built into this FFmpeg
	}
	const marker = "Supported pixel formats:"
	for _, line := range strings.Split(text, "\n") {
		i := strings.Index(line, marker)
		if i < 0 {
			continue
		}
		for _, f := range strings.Fields(line[i+len(marker):]) {
			if f == pixfmt {
				return true
			}
		}
		return false // the encoder lists formats, and pixfmt is not among them
	}
	return false // no pixel-format line -> support unconfirmed
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func sizeArg(s clip.Size) string { return strconv.Itoa(s.W) + "x" + strconv.Itoa(s.H) }

func rateArg(r clip.Rate) string {
	return strconv.Itoa(r.Num) + "/" + strconv.Itoa(r.Den)
}
