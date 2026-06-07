package render

import (
	"fmt"
	"io"
	"os"
)

// BarProgress is a built-in Progress that draws a single-line percentage bar to
// os.Stderr as frames are encoded, replacing the bar copy-pasted into the
// examples. The zero value is usable (an unlabeled bar to stderr); set Label to
// title it. Like every Progress implementation it must be driven by the single
// encode-feed goroutine and is not safe for concurrent use.
type BarProgress struct {
	// Label titles the bar (e.g. "render"). It may be empty.
	Label string

	w     io.Writer // defaults to os.Stderr; overridable in tests
	total int
	done  int
}

const barWidth = 20

// SetTotal records the denominator. It is called once up front and again, with
// the real count, when a source ends before its scheduled duration; redrawing on
// that revision lets the bar reach 100% on an early EOF instead of sticking at
// the stale total.
func (p *BarProgress) SetTotal(frames int) {
	p.total = frames
	if p.done > 0 {
		p.render()
	}
}

// Step advances the bar by one encoded frame.
func (p *BarProgress) Step() {
	p.done++
	p.render()
}

func (p *BarProgress) out() io.Writer {
	if p.w != nil {
		return p.w
	}
	return os.Stderr
}

func (p *BarProgress) render() {
	if p.total <= 0 {
		return
	}
	pct := p.done * 100 / p.total
	fmt.Fprintf(p.out(), "\r  %-12s [%-*s] %3d%% (%d/%d)", p.Label, barWidth, bar(pct), pct, p.done, p.total)
	if p.done >= p.total {
		fmt.Fprintln(p.out())
	}
}

// bar renders the filled portion of a barWidth-wide bar for pct in [0,100].
func bar(pct int) string {
	n := pct * barWidth / 100
	if n > barWidth {
		n = barWidth
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = '='
	}
	return string(out)
}
