package ffmpeg

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

// FPSSource selects which ffprobe field supplies a video stream's frame rate.
type FPSSource int

const (
	// FPSDefault uses r_frame_rate, the base/real frame rate (MoviePy's default).
	FPSDefault FPSSource = iota
	// FPSTbr requests the "tbr" reading; ffprobe JSON has no distinct tbr field,
	// so it resolves to r_frame_rate.
	FPSTbr
	// FPSAvg uses avg_frame_rate.
	FPSAvg
)

func (s FPSSource) String() string {
	switch s {
	case FPSAvg:
		return "avg"
	case FPSTbr:
		return "tbr"
	default:
		return "fps"
	}
}

// ProbeOptions tunes metadata extraction.
type ProbeOptions struct {
	FPSSource FPSSource
}

// VideoStream is the first video stream's decoded metadata.
type VideoStream struct {
	Index    int
	Codec    string
	Size     clip.Size // display size, after applying Rotation
	RawSize  clip.Size // stored size, before rotation
	Rate     clip.Rate
	Rotation int // normalized 0/90/180/270
	PixFmt   string
	NBFrames int64 // 0 when ffprobe does not report it
}

// AudioStream is the first audio stream's metadata.
type AudioStream struct {
	Index      int
	Codec      string
	SampleRate int
	Channels   int
	Layout     string
}

// MediaInfo is the structured result of probing a media file.
type MediaInfo struct {
	Video       *VideoStream
	Audio       *AudioStream
	Duration    clip.Time
	Format      string
	VariableFPS bool      // avg_frame_rate diverges meaningfully from r_frame_rate
	FPSSource   FPSSource // source that produced Video.Rate
}

// Probe runs ffprobe and returns structured metadata for the first video and
// audio stream. A corrupt or unreadable input surfaces as ErrVideoCorrupted
// wrapping the ffprobe stderr tail.
func Probe(ctx context.Context, path string, opts ProbeOptions) (*MediaInfo, error) {
	bin, err := FFprobePath()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		SafeInputPath(path),
	}
	proc, err := Start(ctx, Spec{Path: bin, Args: args, Stdout: true})
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(proc.Stdout)
	waitErr := proc.Wait()
	// A canceled context kills ffprobe; surface the context error rather than
	// misreporting it as corrupt media.
	if ctx.Err() != nil {
		return nil, clip.Wrap("probe "+path, ctx.Err())
	}
	if waitErr != nil {
		return nil, clip.Wrap("probe "+path, joinCorrupted(waitErr))
	}
	if readErr != nil {
		return nil, clip.Wrap("probe "+path, readErr)
	}

	var out probeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, clip.Wrap("probe "+path, err)
	}
	return out.toMediaInfo(opts)
}

// joinCorrupted maps an ffprobe failure to the typed corruption sentinel while
// keeping the ExitError's stderr tail in the message chain.
func joinCorrupted(err error) error {
	return clip.Wrap("corrupt: "+err.Error(), clip.ErrVideoCorrupted)
}

type probeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	PixFmt        string            `json:"pix_fmt"`
	RFrameRate    string            `json:"r_frame_rate"`
	AvgFrameRate  string            `json:"avg_frame_rate"`
	NBFrames      string            `json:"nb_frames"`
	SampleRate    string            `json:"sample_rate"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channel_layout"`
	Duration      string            `json:"duration"`
	Tags          map[string]string `json:"tags"`
	SideData      []probeSideData   `json:"side_data_list"`
}

type probeSideData struct {
	Type     string      `json:"side_data_type"`
	Rotation json.Number `json:"rotation"`
}

type probeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
}

func (o probeOutput) toMediaInfo(opts ProbeOptions) (*MediaInfo, error) {
	info := &MediaInfo{
		Format:    o.Format.FormatName,
		Duration:  parseDuration(o.Format.Duration),
		FPSSource: opts.FPSSource,
	}
	for i := range o.Streams {
		s := &o.Streams[i]
		switch s.CodecType {
		case "video":
			if info.Video == nil {
				info.Video, info.VariableFPS = s.toVideo(opts.FPSSource)
			}
		case "audio":
			if info.Audio == nil {
				info.Audio = s.toAudio()
			}
		}
	}
	if info.Duration == 0 {
		if info.Video != nil && o.streamDuration("video") > 0 {
			info.Duration = o.streamDuration("video")
		} else if info.Audio != nil && o.streamDuration("audio") > 0 {
			info.Duration = o.streamDuration("audio")
		}
	}
	if info.Video == nil && info.Audio == nil {
		return nil, clip.Wrap("probe", clip.ErrVideoCorrupted)
	}
	return info, nil
}

func (o probeOutput) streamDuration(codecType string) clip.Time {
	for i := range o.Streams {
		if o.Streams[i].CodecType == codecType {
			return parseDuration(o.Streams[i].Duration)
		}
	}
	return 0
}

func (s *probeStream) toVideo(src FPSSource) (*VideoStream, bool) {
	rRate, rOK := parseRate(s.RFrameRate)
	aRate, aOK := parseRate(s.AvgFrameRate)

	rate := rRate
	switch src {
	case FPSAvg:
		if aOK {
			rate = aRate
		}
	default: // FPSDefault, FPSTbr -> r_frame_rate
		if !rOK && aOK {
			rate = aRate
		}
	}

	rotation := s.rotation()
	raw := clip.Size{W: s.Width, H: s.Height}
	size := raw
	if rotation == 90 || rotation == 270 {
		size.W, size.H = raw.H, raw.W
	}

	v := &VideoStream{
		Index:    s.Index,
		Codec:    s.CodecName,
		Size:     size,
		RawSize:  raw,
		Rate:     rate,
		Rotation: rotation,
		PixFmt:   s.PixFmt,
		NBFrames: parseInt64(s.NBFrames),
	}
	return v, variableFPS(rRate, rOK, aRate, aOK)
}

func (s *probeStream) toAudio() *AudioStream {
	return &AudioStream{
		Index:      s.Index,
		Codec:      s.CodecName,
		SampleRate: int(parseInt64(s.SampleRate)),
		Channels:   s.Channels,
		Layout:     s.ChannelLayout,
	}
}

// rotation returns the normalized 0/90/180/270 display rotation, preferring a
// displaymatrix side-data entry over a legacy tags.rotate value.
func (s *probeStream) rotation() int {
	for i := range s.SideData {
		if s.SideData[i].Type == "Display Matrix" {
			if v, err := s.SideData[i].Rotation.Float64(); err == nil {
				return normalizeRotation(int(v))
			}
		}
	}
	if s.Tags != nil {
		if r, ok := s.Tags["rotate"]; ok {
			if v, err := strconv.Atoi(strings.TrimSpace(r)); err == nil {
				return normalizeRotation(v)
			}
		}
	}
	return 0
}

func normalizeRotation(deg int) int {
	deg = ((deg % 360) + 360) % 360
	// Snap to the cardinal step; only 90/270 swap dimensions.
	switch {
	case deg < 45 || deg >= 315:
		return 0
	case deg < 135:
		return 90
	case deg < 225:
		return 180
	default:
		return 270
	}
}

// variableFPS reports a meaningful divergence between the base and average
// frame rates, flagging files whose timing is not constant.
func variableFPS(r clip.Rate, rOK bool, a clip.Rate, aOK bool) bool {
	if !rOK || !aOK || a.Num == 0 {
		return false
	}
	rf, af := r.Float(), a.Float()
	if rf == 0 || af == 0 {
		return false
	}
	diff := rf - af
	if diff < 0 {
		diff = -diff
	}
	return diff/rf > 0.01
}

// parseRate parses an ffprobe "num/den" rate, reduces it, and applies the NTSC
// snap only when the value is genuinely close to a standard NTSC rate (an exact
// stored rational is otherwise preserved). Returns ok=false for "0/0".
func parseRate(s string) (clip.Rate, bool) {
	num, den, found := strings.Cut(s, "/")
	if !found {
		return clip.Rate{}, false
	}
	n, err1 := strconv.Atoi(strings.TrimSpace(num))
	d, err2 := strconv.Atoi(strings.TrimSpace(den))
	if err1 != nil || err2 != nil || d == 0 || n == 0 {
		return clip.Rate{}, false
	}
	r := reduce(clip.Rate{Num: n, Den: d})
	if snapped, ok := clip.SnapNTSCMatch(r.Float()); ok {
		return snapped, true
	}
	return r, true
}

func reduce(r clip.Rate) clip.Rate {
	g := gcd(abs(r.Num), abs(r.Den))
	if g == 0 {
		return r
	}
	return clip.Rate{Num: r.Num / g, Den: r.Den / g}
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// parseDuration parses ffprobe's seconds-with-fraction duration string.
func parseDuration(s string) clip.Time {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	secs, err := strconv.ParseFloat(s, 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return clip.Time(secs * float64(time.Second))
}

func parseInt64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
