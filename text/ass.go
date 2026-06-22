package text

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
)

// NewSubtitlesFromASS loads an .ass/.ssa file and builds a subtitles clip over
// opts.Size, mirroring the SRT/VTT/JSON constructors.
func NewSubtitlesFromASS(path string, opts SubtitleOptions) (*SubtitlesNode, error) {
	cues, err := ParseASSFile(path)
	if err != nil {
		return nil, err
	}
	return NewSubtitles(cues, opts)
}

// ParseASSFile reads and parses an .ass/.ssa subtitle file.
func ParseASSFile(path string) ([]Cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, clip.Wrap("open subtitles "+path, err)
	}
	defer f.Close()
	return ParseASS(f)
}

// ParseASS parses Advanced SubStation Alpha (ASS/SSA) events into cues. It
// covers the common 80% subset: the [Events] section's Dialogue lines, mapped
// through the section's Format declaration (so column order is honored), with
// the override tags stripped from the text. Styles, positioning, and karaoke
// timing are intentionally ignored — the text and its [Start, End) interval are
// what a burned-in caption needs.
func ParseASS(r io.Reader) ([]Cue, error) {
	var (
		cues                      []Cue
		inEvents                  bool
		startIdx, endIdx, textIdx = -1, -1, -1
		numFields                 int
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inEvents = strings.EqualFold(line, "[Events]")
			continue
		}
		if !inEvents {
			continue
		}
		key, val, ok := splitField(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "format":
			startIdx, endIdx, textIdx, numFields = eventColumns(val)
		case "dialogue":
			if textIdx < 0 {
				continue // a Dialogue before its Format: cannot map columns
			}
			if cue, ok := parseDialogue(val, startIdx, endIdx, textIdx, numFields); ok {
				cues = append(cues, cue)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, clip.Wrap("read subtitles", err)
	}
	return cues, nil
}

// splitField splits "Key: value" on the first colon.
func splitField(line string) (key, val string, ok bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// eventColumns reads a [Events] Format line and returns the column indices of
// Start, End, and Text plus the field count, so Dialogue lines map regardless of
// the declared column order.
func eventColumns(format string) (start, end, text, n int) {
	cols := strings.Split(format, ",")
	start, end, text = -1, -1, -1
	for i, c := range cols {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "start":
			start = i
		case "end":
			end = i
		case "text":
			text = i
		}
	}
	return start, end, text, len(cols)
}

// parseDialogue maps one Dialogue value to a Cue. Text is the last column and
// may itself contain commas, so the split stops before it.
func parseDialogue(val string, startIdx, endIdx, textIdx, numFields int) (Cue, bool) {
	fields := strings.SplitN(val, ",", numFields)
	if len(fields) < numFields {
		return Cue{}, false
	}
	start, ok1 := parseASSTime(fields[startIdx])
	end, ok2 := parseASSTime(fields[endIdx])
	if !ok1 || !ok2 {
		return Cue{}, false
	}
	return Cue{Start: start, End: end, Text: cleanASSText(fields[textIdx])}, true
}

// parseASSTime parses an ASS timestamp H:MM:SS.cc (centiseconds, single-digit
// hour) into a Time.
func parseASSTime(s string) (clip.Time, bool) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, ok1 := atoiTrim(parts[0])
	m, ok2 := atoiTrim(parts[1])
	secCenti := strings.SplitN(parts[2], ".", 2)
	sec, ok3 := atoiTrim(secCenti[0])
	if !ok1 || !ok2 || !ok3 {
		return 0, false
	}
	centi := 0
	if len(secCenti) == 2 {
		// Pad/truncate the fractional part to hundredths.
		frac := (secCenti[1] + "00")[:2]
		var ok bool
		if centi, ok = atoiTrim(frac); !ok {
			return 0, false
		}
	}
	total := clip.Time(h)*clip.Time(3600*1e9) +
		clip.Time(m)*clip.Time(60*1e9) +
		clip.Time(sec)*clip.Time(1e9) +
		clip.Time(centi)*clip.Time(10*1e6)
	return total, true
}

// cleanASSText strips override blocks ({\...}) and converts ASS escapes to
// plain text: \N and \n become newlines, \h becomes a hard space.
func cleanASSText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{': // skip an override block up to the next '}'
			if j := strings.IndexByte(s[i:], '}'); j >= 0 {
				i += j
				continue
			}
		case '\\':
			if i+1 < len(s) {
				switch s[i+1] {
				case 'N', 'n':
					b.WriteByte('\n')
					i++
					continue
				case 'h':
					b.WriteByte(' ')
					i++
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return strings.TrimSpace(b.String())
}

// atoiTrim parses a trimmed non-negative integer.
func atoiTrim(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
