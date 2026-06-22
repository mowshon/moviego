package text

import (
	"bytes"
	"encoding/json"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
)

// Styles holds the optional presentation hints carried by the rich JSON form.
// Font is a path to a .ttf/.otf file when it names a readable file, otherwise it
// is treated as a non-resolvable family hint and ignored (the default font is
// used). Color is a "#RGB"/"#RRGGBB"/"#RRGGBBAA" hex string.
type Styles struct {
	Font  string
	Color string
}

// jsonTime is a cue timestamp that accepts either a string ("HH:MM:SS,mmm" or
// "MM:SS.mmm") or a number of milliseconds, matching both JSON subtitle shapes.
type jsonTime clip.Time

func (j *jsonTime) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		t, err := parseTimestamp(strings.TrimSpace(s))
		if err != nil {
			return err
		}
		*j = jsonTime(t)
		return nil
	}
	ms, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return clip.Wrap("subtitles json", ErrBadTimestamp)
	}
	*j = jsonTime(clip.Time(ms * float64(clip.Time(1e6)))) // ms → ns
	return nil
}

// jsonWord and jsonCue mirror the rich object form; the simple array form uses
// the same jsonCue with only start/end/text populated.
type jsonWord struct {
	Word  string   `json:"word"`
	Start jsonTime `json:"start"`
	End   jsonTime `json:"end"`
}

type jsonCue struct {
	Start jsonTime   `json:"start"`
	End   jsonTime   `json:"end"`
	Text  string     `json:"text"`
	Words []jsonWord `json:"words"`
}

func (c jsonCue) toCue() Cue {
	cue := Cue{Start: clip.Time(c.Start), End: clip.Time(c.End), Text: c.Text}
	for _, w := range c.Words {
		cue.Words = append(cue.Words, Word{
			Text:  w.Word,
			Start: clip.Time(w.Start),
			End:   clip.Time(w.End),
		})
	}
	return cue
}

// jsonDoc is the rich object form: a styles block plus a cues array.
type jsonDoc struct {
	Styles Styles    `json:"styles"`
	Cues   []jsonCue `json:"cues"`
}

// ParseJSON parses cues from either JSON subtitle shape: a top-level array of
// {start,end,text[,words]} objects, or an object {styles,cues:[…]}. Timestamps
// may be strings or millisecond numbers. The styles block, if any, is ignored
// here; use ParseJSONStyled to read it.
func ParseJSON(r io.Reader) ([]Cue, error) {
	_, cues, err := parseJSON(r)
	return cues, err
}

// ParseJSONStyled is ParseJSON that also returns the styles block (zero-valued
// for the array form).
func ParseJSONStyled(r io.Reader) (Styles, []Cue, error) {
	return parseJSON(r)
}

// ParseJSONFile and ParseJSONStyledFile are the file-loading conveniences.
func ParseJSONFile(path string) ([]Cue, error) {
	_, cues, err := parseJSONFile(path)
	return cues, err
}

func ParseJSONStyledFile(path string) (Styles, []Cue, error) {
	return parseJSONFile(path)
}

func parseJSONFile(path string) (Styles, []Cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return Styles{}, nil, clip.Wrap("open subtitles "+path, err)
	}
	defer f.Close()
	return parseJSON(f)
}

// parseJSON dispatches on the first non-space byte: '[' → array form, '{' →
// object form.
func parseJSON(r io.Reader) (Styles, []Cue, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Styles{}, nil, clip.Wrap("read subtitles", err)
	}
	switch firstNonSpace(data) {
	case '[':
		var arr []jsonCue
		if err := json.Unmarshal(data, &arr); err != nil {
			return Styles{}, nil, clip.Wrap("subtitles json", err)
		}
		return Styles{}, toCues(arr), nil
	case '{':
		var doc jsonDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			return Styles{}, nil, clip.Wrap("subtitles json", err)
		}
		return doc.Styles, toCues(doc.Cues), nil
	default:
		return Styles{}, nil, clip.Wrap("subtitles json", ErrBadTimestamp)
	}
}

func toCues(in []jsonCue) []Cue {
	out := make([]Cue, len(in))
	for i, c := range in {
		out[i] = c.toCue()
	}
	return out
}

func firstNonSpace(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return c
		}
	}
	return 0
}

// applyStyles overlays the JSON styles onto base: a hex Color and a Font path
// fill the corresponding option only when set and not already provided by the
// caller. A Font that does not name a readable file is ignored (the default
// font is kept), so a family name like "Arial" degrades gracefully.
func applyStyles(base SubtitleOptions, s Styles) SubtitleOptions {
	if base.Color == nil && s.Color != "" {
		if c, ok := parseHexColor(s.Color); ok {
			base.Color = c
		}
	}
	if base.FontPath == "" && s.Font != "" {
		if isReadableFile(s.Font) {
			base.FontPath = s.Font
		}
	}
	return base
}

func isReadableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// parseHexColor parses "#RGB", "#RRGGBB", or "#RRGGBBAA" (the leading '#' is
// optional) into an opaque-by-default NRGBA.
func parseHexColor(s string) (color.Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 3: // RGB → RRGGBB
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6, 8:
	default:
		return nil, false
	}
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return nil, false
	}
	c := color.NRGBA{A: 0xff}
	if len(s) == 8 {
		c.R = byte(v >> 24)
		c.G = byte(v >> 16)
		c.B = byte(v >> 8)
		c.A = byte(v)
	} else {
		c.R = byte(v >> 16)
		c.G = byte(v >> 8)
		c.B = byte(v)
	}
	return c, true
}
