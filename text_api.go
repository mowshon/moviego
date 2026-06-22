package mgo

import "github.com/mowshon/moviego/v2/text"

// TextOptions configures a rendered text clip. See text.Options for the field
// semantics (label vs caption mode, font, color, stroke, alignment).
type TextOptions = text.Options

// SubtitleOptions configures a subtitles clip. See text.SubtitleOptions.
type SubtitleOptions = text.SubtitleOptions

// Cue is a single timed subtitle. See text.Cue.
type Cue = text.Cue

// Word is a single timed token within a cue (word-level JSON). See text.Word.
type Word = text.Word

// SubtitleLayout selects caption (bottom-anchored, wrapped) vs word-centered
// placement. See text.SubtitleLayout.
type SubtitleLayout = text.SubtitleLayout

// Align selects the horizontal alignment for multi-line text. See text.Align.
type Align = text.Align

// Text alignment for multi-line and caption text.
const (
	AlignLeft   = text.AlignLeft
	AlignCenter = text.AlignCenter
	AlignRight  = text.AlignRight
)

// Subtitle layout modes.
const (
	LayoutCaption    = text.LayoutCaption
	LayoutWordCenter = text.LayoutWordCenter
)

// Text renders content into a static image clip using opts (content overrides
// opts.Text). Give it a duration with WithDuration and a rate via ExportOptions,
// or place it in a Composite. A transparent result carries a mask.
func Text(content string, opts TextOptions) (*Video, error) {
	opts.Text = content
	n, err := text.New(opts)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// Subtitles builds a transparent subtitles clip from pre-parsed cues over a
// canvas of opts.Size. Composite it over a video to burn captions in.
func Subtitles(cues []Cue, opts SubtitleOptions) (*Video, error) {
	n, err := text.NewSubtitles(cues, opts)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// SubtitlesSRT loads an .srt file and builds a subtitles clip over opts.Size.
func SubtitlesSRT(path string, opts SubtitleOptions) (*Video, error) {
	n, err := text.NewSubtitlesFromSRT(path, opts)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// SubtitlesVTT loads a .vtt file and builds a subtitles clip over opts.Size.
func SubtitlesVTT(path string, opts SubtitleOptions) (*Video, error) {
	n, err := text.NewSubtitlesFromVTT(path, opts)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// SubtitlesJSON loads a JSON subtitle file — either a [{start,end,text}] array
// or a {styles,cues:[{…,words:[…]}]} object with per-word timing — and builds a
// subtitles clip. Styles (hex color, font path) fill any option left unset. Set
// opts.Layout to LayoutWordCenter to center one timed word at a time.
func SubtitlesJSON(path string, opts SubtitleOptions) (*Video, error) {
	n, err := text.NewSubtitlesFromJSON(path, opts)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}
