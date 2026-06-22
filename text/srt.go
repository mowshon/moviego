package text

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
)

// ErrBadTimestamp reports a cue timestamp that does not match HH:MM:SS,mmm
// (SRT) or HH:MM:SS.mmm / MM:SS.mmm (VTT).
var ErrBadTimestamp = errors.New("subtitles: malformed timestamp")

// Cue is a timed subtitle: the half-open interval [Start, End) and its text
// (newlines preserved for multi-line cues). Words is optional per-word timing
// (populated from the word-level JSON form); it drives the word-centered layout
// and is empty for SRT/VTT.
type Cue struct {
	Start clip.Time
	End   clip.Time
	Text  string
	Words []Word
}

// Word is a single token with its own [Start, End) timing, used by the
// word-centered subtitle layout for social/vertical videos.
type Word struct {
	Text  string
	Start clip.Time
	End   clip.Time
}

// ParseSRTFile reads and parses an .srt file.
func ParseSRTFile(path string) ([]Cue, error) {
	return parseFile(path, ParseSRT)
}

// ParseVTTFile reads and parses a .vtt file.
func ParseVTTFile(path string) ([]Cue, error) {
	return parseFile(path, ParseVTT)
}

func parseFile(path string, parse func(io.Reader) ([]Cue, error)) ([]Cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, clip.Wrap("open subtitles "+path, err)
	}
	defer f.Close()
	return parse(f)
}

// ParseSRT parses SubRip cues. Blank lines separate records; the optional index
// line is ignored; the second line carries "start --> end" with comma decimals.
func ParseSRT(r io.Reader) ([]Cue, error) {
	return parseBlocks(r, false)
}

// ParseVTT parses WebVTT cues. The leading "WEBVTT" header, NOTE/STYLE/REGION
// blocks, and cue identifiers are skipped; the timing line uses dot decimals
// and may carry trailing positioning settings, which are ignored.
func ParseVTT(r io.Reader) ([]Cue, error) {
	return parseBlocks(r, true)
}

// parseBlocks splits the stream into blank-line-separated blocks and turns each
// timing block into a cue. It tolerates either decimal separator regardless of
// the vtt flag, so a mislabeled file still parses.
func parseBlocks(r io.Reader, vtt bool) ([]Cue, error) {
	var cues []Cue
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var block []string
	flush := func() error {
		defer func() { block = block[:0] }()
		cue, ok, err := blockToCue(block)
		if err != nil {
			return err
		}
		if ok {
			cues = append(cues, cue)
		}
		return nil
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if vtt {
			line = strings.TrimPrefix(line, "\ufeff") // strip a leading BOM
		}
		if strings.TrimSpace(line) == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		block = append(block, line)
	}
	if err := sc.Err(); err != nil {
		return nil, clip.Wrap("read subtitles", err)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return cues, nil
}

// blockToCue turns one block into a cue. A block whose lines contain no "-->"
// timing (a WEBVTT header or a NOTE block) yields ok=false.
func blockToCue(block []string) (Cue, bool, error) {
	for i, line := range block {
		if !strings.Contains(line, "-->") {
			continue
		}
		start, end, err := parseTiming(line)
		if err != nil {
			return Cue{}, false, err
		}
		text := strings.Join(block[i+1:], "\n")
		return Cue{Start: start, End: end, Text: text}, true, nil
	}
	return Cue{}, false, nil
}

// parseTiming parses a "start --> end [settings]" line.
func parseTiming(line string) (start, end clip.Time, err error) {
	left, right, ok := strings.Cut(line, "-->")
	if !ok {
		return 0, 0, clip.Wrap("subtitles", ErrBadTimestamp)
	}
	start, err = parseTimestamp(strings.TrimSpace(left))
	if err != nil {
		return 0, 0, err
	}
	// The end may be followed by VTT cue settings (e.g. "line:90% align:center").
	rightField := strings.Fields(strings.TrimSpace(right))
	if len(rightField) == 0 {
		return 0, 0, clip.Wrap("subtitles", ErrBadTimestamp)
	}
	end, err = parseTimestamp(rightField[0])
	return start, end, err
}

// parseTimestamp accepts HH:MM:SS, MM:SS, with ',' or '.' before milliseconds.
func parseTimestamp(s string) (clip.Time, error) {
	s = strings.Replace(s, ",", ".", 1)
	secPart := s
	var ms int
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		secPart = s[:dot]
		frac := s[dot+1:]
		for len(frac) < 3 { // pad "5" → "500"
			frac += "0"
		}
		frac = frac[:3]
		v, err := strconv.Atoi(frac)
		if err != nil {
			return 0, clip.Wrap("subtitles", ErrBadTimestamp)
		}
		ms = v
	}
	fields := strings.Split(secPart, ":")
	if len(fields) < 2 || len(fields) > 3 {
		return 0, clip.Wrap("subtitles", ErrBadTimestamp)
	}
	var h, m, sec int
	var err error
	switch len(fields) {
	case 3:
		if h, err = atoi(fields[0]); err != nil {
			return 0, err
		}
		if m, err = atoi(fields[1]); err != nil {
			return 0, err
		}
		if sec, err = atoi(fields[2]); err != nil {
			return 0, err
		}
	case 2:
		if m, err = atoi(fields[0]); err != nil {
			return 0, err
		}
		if sec, err = atoi(fields[1]); err != nil {
			return 0, err
		}
	}
	total := clip.Time(h)*clip.Time(3600e9) +
		clip.Time(m)*clip.Time(60e9) +
		clip.Time(sec)*clip.Time(1e9) +
		clip.Time(ms)*clip.Time(1e6)
	return total, nil
}

func atoi(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, clip.Wrap("subtitles", ErrBadTimestamp)
	}
	return n, nil
}
