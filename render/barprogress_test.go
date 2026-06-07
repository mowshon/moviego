package render

import (
	"bytes"
	"strings"
	"testing"
)

// TestBarProgressReaches100 drives the bar to completion and checks it writes a
// final 100% line.
func TestBarProgressReaches100(t *testing.T) {
	var buf bytes.Buffer
	p := &BarProgress{Label: "render", w: &buf}
	p.SetTotal(4)
	for i := 0; i < 4; i++ {
		p.Step()
	}
	out := buf.String()
	if !strings.Contains(out, "100%") {
		t.Errorf("output missing 100%%: %q", out)
	}
	if !strings.Contains(out, "render") {
		t.Errorf("output missing label: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("final line not terminated with newline: %q", out)
	}
}

// TestBarProgressRevisedTotalRedraws checks a revised (lower) total after some
// steps redraws so an early EOF can still reach 100%.
func TestBarProgressRevisedTotalRedraws(t *testing.T) {
	var buf bytes.Buffer
	p := &BarProgress{w: &buf}
	p.SetTotal(10)
	p.Step()
	p.Step()
	buf.Reset()
	p.SetTotal(2) // source ended early at 2 frames
	if !strings.Contains(buf.String(), "100%") {
		t.Errorf("revised total did not redraw to 100%%: %q", buf.String())
	}
}

// TestBarProgressZeroTotalSilent checks a zero total never panics or draws.
func TestBarProgressZeroTotalSilent(t *testing.T) {
	var buf bytes.Buffer
	p := &BarProgress{w: &buf}
	p.SetTotal(0)
	p.Step()
	if buf.Len() != 0 {
		t.Errorf("zero total wrote output: %q", buf.String())
	}
}
