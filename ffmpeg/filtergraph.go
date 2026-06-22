package ffmpeg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
)

// This file is the structured filtergraph layer the fusion planner builds on.
// Nodes advertise a FilterFragment describing the FFmpeg filter they are
// equivalent to; the planner stitches the fragments into one labeled
// -filter_complex graph and runs a single FFmpeg invocation, skipping the
// rawvideo round-trip entirely. Everything here is pure string assembly so it is
// unit-testable without launching FFmpeg.

// Pad is a labeled filtergraph stream together with the rate and pixel format
// flowing through it. Labels are the [name] tokens in a filter_complex graph
// (e.g. "0:v" for the first input's video, or an intermediate "v1").
type Pad struct {
	Label  string
	Rate   clip.Rate
	PixFmt clip.PixelFormat
}

// Constraint is a requirement a fragment imposes on the fused graph. The
// planner uses it to reject graphs it cannot satisfy (e.g. an alpha-carrying
// chain bound for an opaque-only container).
type Constraint int

const (
	// ConstraintAlpha marks a fragment that requires an alpha-carrying pixel
	// format end to end (transparent chains). v1 fusion rejects these.
	ConstraintAlpha Constraint = iota
	// ConstraintEvenDims marks a fragment that requires even output dimensions
	// (a chroma-subsampled output pixel format).
	ConstraintEvenDims
)

// FilterContext is handed to a node's Filter method. Inputs are the pads
// feeding this node (one for a unary filter, several for concat/overlay),
// OutLabel is the unique label the node must emit, and OutputRate is the
// export rate the planner injects so static sources and an end-of-chain fps
// normalization use the right value.
type FilterContext struct {
	Inputs     []Pad
	OutLabel   string
	OutputRate clip.Rate
	PixFmt     clip.PixelFormat
}

// FilterFragment is a node's contribution to the fused graph: the filtergraph
// lines it adds (using its input pads' labels and emitting OutLabel), the pad
// it produces, and any constraints it imposes.
type FilterFragment struct {
	Lines       []string
	Out         Pad
	Constraints []Constraint
}

// Input is one "-i" input to a fused command: the options that precede -i
// (e.g. "-f lavfi" for a generated source) and the URL/path/descriptor.
type Input struct {
	Args []string
	Name string
}

// FilterGraph is the assembled fused command description: the ordered input
// list, the filtergraph lines, and the label carrying the final video stream.
// It is codec-independent; encode settings (codec/preset/pixfmt/audio) are
// applied by the render layer when it builds the actual command.
type FilterGraph struct {
	Inputs   []Input
	Lines    []string
	OutVideo string
}

// FilterComplex joins the graph lines into a single -filter_complex argument.
func (g *FilterGraph) FilterComplex() string {
	return strings.Join(g.Lines, ";")
}

// InputArgs returns the "-i" argument list for every input in order, applying
// SafeInputPath to each name so a leading '-' is never parsed as a flag.
func (g *FilterGraph) InputArgs() []string {
	args := make([]string, 0, len(g.Inputs)*3)
	for _, in := range g.Inputs {
		args = append(args, in.Args...)
		args = append(args, "-i", SafeInputPath(in.Name))
	}
	return args
}

// link wraps a filter body in its input/output pad labels:
// "[in]body[out]". A multi-input filter passes several labels in in.
func link(in []string, body, out string) string {
	var b strings.Builder
	for _, l := range in {
		b.WriteByte('[')
		b.WriteString(l)
		b.WriteByte(']')
	}
	b.WriteString(body)
	b.WriteByte('[')
	b.WriteString(out)
	b.WriteByte(']')
	return b.String()
}

// Scale emits a scale filter line resizing the in pad to w x h. FFmpeg's scale
// defaults to bicubic, which differs from the Go path's CatmullRom kernel; the
// divergence is documented in docs/compatibility.md and tolerated in goldens.
func Scale(in, out string, w, h int) string {
	return link([]string{in}, fmt.Sprintf("scale=%d:%d", w, h), out)
}

// Crop emits a crop filter line extracting the w x h window at (x, y). This is
// an exact pixel copy, identical to the Go crop.
func Crop(in, out string, w, h, x, y int) string {
	return link([]string{in}, fmt.Sprintf("crop=%d:%d:%d:%d", w, h, x, y), out)
}

// Transpose emits the transpose chain for a clockwise quarter-turn rotation:
// quarter 1 -> transpose=1 (90° CW), 2 -> two turns (180°), 3 -> transpose=2
// (90° CCW). quarter 0 is a no-op pass-through (null). The direction matches
// imagex.RotateInto.
func Transpose(in, out string, quarter int) string {
	q := ((quarter % 4) + 4) % 4
	var body string
	switch q {
	case 1:
		body = "transpose=1"
	case 2:
		body = "transpose=1,transpose=1"
	case 3:
		body = "transpose=2"
	default:
		body = "null"
	}
	return link([]string{in}, body, out)
}

// RotateAngle emits an arbitrary-angle rotation. angle is clockwise radians (to
// match imagex.RotateAngleInto and the Go path); ow/oh set the output canvas,
// and col fills the exposed corners. FFmpeg's rotate measures counter-clockwise,
// so the angle is negated. The bilinear kernel differs from the Go sampler, a
// divergence documented in docs/compatibility.md.
func RotateAngle(in, out string, angle float64, ow, oh int, col [3]byte) string {
	body := fmt.Sprintf("rotate=a=%s:ow=%d:oh=%d:c=0x%02x%02x%02x",
		formatFloat(-angle), ow, oh, col[0], col[1], col[2])
	return link([]string{in}, body, out)
}

// EqColor emits an eq filter with the supplied tonal adjustments. brightness is
// additive in [-1, 1], the rest are multipliers around 1.0 (1.0 = identity).
// Only non-identity terms are emitted so a single-knob effect stays a minimal
// fragment.
func EqColor(in, out string, brightness, contrast, saturation, gamma float64) string {
	var parts []string
	if brightness != 0 {
		parts = append(parts, "brightness="+formatFloat(brightness))
	}
	if contrast != 1 {
		parts = append(parts, "contrast="+formatFloat(contrast))
	}
	if saturation != 1 {
		parts = append(parts, "saturation="+formatFloat(saturation))
	}
	if gamma != 1 {
		parts = append(parts, "gamma="+formatFloat(gamma))
	}
	if len(parts) == 0 {
		return link([]string{in}, "null", out)
	}
	return link([]string{in}, "eq="+strings.Join(parts, ":"), out)
}

// ColorBalance emits a colorbalance filter. shadows, mids and highlights each
// carry per-channel [r, g, b] adjustments in [-1, 1].
func ColorBalance(in, out string, shadows, mids, highlights [3]float64) string {
	body := fmt.Sprintf("colorbalance=rs=%s:gs=%s:bs=%s:rm=%s:gm=%s:bm=%s:rh=%s:gh=%s:bh=%s",
		formatFloat(shadows[0]), formatFloat(shadows[1]), formatFloat(shadows[2]),
		formatFloat(mids[0]), formatFloat(mids[1]), formatFloat(mids[2]),
		formatFloat(highlights[0]), formatFloat(highlights[1]), formatFloat(highlights[2]))
	return link([]string{in}, body, out)
}

// Hue emits a hue filter rotating by degrees and scaling saturation by sat
// (1.0 = unchanged).
func Hue(in, out string, degrees, sat float64) string {
	body := fmt.Sprintf("hue=h=%s:s=%s", formatFloat(degrees), formatFloat(sat))
	return link([]string{in}, body, out)
}

// Lut3D emits a lut3d filter applying the 3D LUT at path (a .cube file). The
// path is filtergraph-escaped, so a Windows drive ("C:\look.cube") or a path
// with spaces, commas or quotes does not break the graph or get misread as
// extra filter options.
func Lut3D(in, out, path string) string {
	return link([]string{in}, "lut3d=file="+escapeFilterValue(path), out)
}

// GBlur emits a gblur Gaussian blur with the given sigma.
func GBlur(in, out string, sigma float64) string {
	return link([]string{in}, "gblur=sigma="+formatFloat(sigma), out)
}

// HFlip emits a horizontal mirror filter line. It is an exact pixel copy,
// identical to the Go path.
func HFlip(in, out string) string {
	return link([]string{in}, "hflip", out)
}

// VFlip emits a vertical mirror filter line. It is an exact pixel copy,
// identical to the Go path.
func VFlip(in, out string) string {
	return link([]string{in}, "vflip", out)
}

// Trim emits a trim + PTS-reset chain selecting [start, start+dur) of the in
// pad and rebasing its timestamps to zero, the filtergraph equivalent of a
// forward Subclip.
func Trim(in, out string, start, dur clip.Time) string {
	body := fmt.Sprintf("trim=start=%s:duration=%s,setpts=PTS-STARTPTS", secs(start), secs(dur))
	return link([]string{in}, body, out)
}

// SetPTS emits a setpts speed change: output PTS = PTS / factor, so factor > 1
// plays faster. It is the filtergraph equivalent of a positive MultiplySpeed.
func SetPTS(in, out string, factor float64) string {
	return link([]string{in}, "setpts=PTS/"+formatFloat(factor), out)
}

// FadeKind selects a fade direction.
type FadeKind string

const (
	// FadeInKind ramps up from col over [st, st+d).
	FadeInKind FadeKind = "in"
	// FadeOutKind ramps down to col over [st, st+d).
	FadeOutKind FadeKind = "out"
)

// Fade emits an fade filter line of the given kind starting at st for duration
// d, fading to/from col.
func Fade(in, out string, kind FadeKind, st, d clip.Time, col [3]byte) string {
	body := fmt.Sprintf("fade=t=%s:st=%s:d=%s:color=0x%02x%02x%02x",
		string(kind), secs(st), secs(d), col[0], col[1], col[2])
	return link([]string{in}, body, out)
}

// FPS emits an fps filter normalizing the in pad to rate r. The planner appends
// it as the last video stage so the fused output matches the export schedule's
// frame rate (the same rate the Go engine samples at).
func FPS(in, out string, r clip.Rate) string {
	return link([]string{in}, "fps="+rateArg(r), out)
}

// rateArg renders a rational rate as FFmpeg's "Num/Den".
func rateArg(r clip.Rate) string {
	return strconv.Itoa(r.Num) + "/" + strconv.Itoa(r.Den)
}

// Concat emits a concat filter joining the in pads end to end into one video
// stream (video only).
func Concat(in []string, out string) string {
	body := fmt.Sprintf("concat=n=%d:v=1:a=0", len(in))
	return link(in, body, out)
}

// Overlay emits an overlay filter compositing the second pad (ovl) onto the
// first (base) at (x, y).
func Overlay(base, ovl, out string, x, y int) string {
	return link([]string{base, ovl}, fmt.Sprintf("overlay=%d:%d", x, y), out)
}

// Volume emits an audio volume filter scaling amplitude by factor.
func Volume(in, out string, factor float64) string {
	return link([]string{in}, "volume="+formatFloat(factor), out)
}

// AFade emits an audio fade of the given kind starting at st for duration d.
func AFade(in, out string, kind FadeKind, st, d clip.Time) string {
	body := fmt.Sprintf("afade=t=%s:st=%s:d=%s", string(kind), secs(st), secs(d))
	return link([]string{in}, body, out)
}

// secs renders a clip.Time as a decimal-seconds string for FFmpeg time options.
// It uses the shortest round-tripping form so an exact input stays exact.
func secs(t clip.Time) string {
	return formatFloat(t.Seconds())
}

// formatFloat renders a float64 in the shortest round-tripping decimal form
// (never scientific notation), keeping filter strings stable and readable.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
