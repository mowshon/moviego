package text

import (
	"context"
	"image/color"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// opaqueCount counts pixels with a non-zero alpha in an RGBA frame.
func opaqueCount(t *testing.T, f *clip.Frame) int {
	t.Helper()
	if f.Format != clip.RGBA {
		t.Fatalf("want RGBA, got %v", f.Format)
	}
	n := 0
	for y := 0; y < f.H; y++ {
		row := f.Pix[y*f.Stride:]
		for x := 0; x < f.W; x++ {
			if row[x*4+3] != 0 {
				n++
			}
		}
	}
	return n
}

func TestRenderTransparentIsRGBAWithGlyphs(t *testing.T) {
	f, err := Render(Options{Text: "Hi", FontSize: 48})
	if err != nil {
		t.Fatal(err)
	}
	if f.Format != clip.RGBA {
		t.Fatalf("transparent text must be RGBA, got %v", f.Format)
	}
	if opaqueCount(t, f) == 0 {
		t.Fatal("expected some painted glyph pixels")
	}
}

func TestRenderOpaqueBackgroundIsRGB24(t *testing.T) {
	f, err := Render(Options{Text: "Hi", FontSize: 48, BG: color.Black})
	if err != nil {
		t.Fatal(err)
	}
	if f.Format != clip.RGB24 {
		t.Fatalf("opaque background must yield RGB24, got %v", f.Format)
	}
}

func TestEmptyStringRendersValidFrame(t *testing.T) {
	f, err := Render(Options{Text: "", FontSize: 32})
	if err != nil {
		t.Fatal(err)
	}
	if f.W < 1 || f.H < 1 {
		t.Fatalf("empty text must still produce a >=1px frame, got %dx%d", f.W, f.H)
	}
}

func TestLabelRequiresFontSize(t *testing.T) {
	if _, err := Render(Options{Text: "x"}); err == nil {
		t.Fatal("label mode with no font size must error")
	}
}

func TestMissingFontErrors(t *testing.T) {
	if _, err := Render(Options{Text: "x", FontSize: 20, FontPath: "/no/such/font.ttf"}); err == nil {
		t.Fatal("missing font file must error")
	}
}

func TestHeightGrowsWithLines(t *testing.T) {
	one, err := buildLayout(Options{Text: "One", FontSize: 40})
	if err != nil {
		t.Fatal(err)
	}
	three, err := buildLayout(Options{Text: "One\nTwo\nThree", FontSize: 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(three.lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(three.lines))
	}
	if three.height <= one.height {
		t.Fatalf("multi-line height %d must exceed single-line %d", three.height, one.height)
	}
	// Height math: band + (n-1)*gap (no stroke here).
	want := one.ascent + one.descent + 2*one.lineGap
	if three.height != want {
		t.Fatalf("3-line height = %d, want %d (ascent %d descent %d gap %d)",
			three.height, want, one.ascent, one.descent, one.lineGap)
	}
}

func TestHeightGrowsWithFontSize(t *testing.T) {
	small, _ := buildLayout(Options{Text: "Ag", FontSize: 20})
	big, _ := buildLayout(Options{Text: "Ag", FontSize: 60})
	if big.height <= small.height || big.width <= small.width {
		t.Fatalf("larger font must produce a larger frame: small %dx%d big %dx%d",
			small.width, small.height, big.width, big.height)
	}
}

func TestStrokeAddsToHeightAndWidth(t *testing.T) {
	plain, _ := buildLayout(Options{Text: "Ag", FontSize: 40})
	outlined, _ := buildLayout(Options{Text: "Ag", FontSize: 40, StrokeWidth: 3})
	if outlined.height != plain.height+6 || outlined.width != plain.width+6 {
		t.Fatalf("stroke 3 must pad both axes by 6: plain %dx%d outlined %dx%d",
			plain.width, plain.height, outlined.width, outlined.height)
	}
}

func TestCaptionWrapsToWidth(t *testing.T) {
	long := "the quick brown fox jumps over the lazy dog"
	narrow, err := buildLayout(Options{Text: long, FontSize: 30, Caption: true, Size: clip.Size{W: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if len(narrow.lines) < 2 {
		t.Fatalf("a long caption in a narrow box must wrap, got %d lines", len(narrow.lines))
	}
	for i, w := range narrow.lineW {
		if w > 200 {
			t.Fatalf("line %d width %d exceeds caption width 200: %q", i, w, narrow.lines[i])
		}
	}
}

func TestCaptionFramedToExactSize(t *testing.T) {
	f, err := Render(Options{Text: "boxed", FontSize: 24, Caption: true, Size: clip.Size{W: 320, H: 120}})
	if err != nil {
		t.Fatal(err)
	}
	if f.W != 320 || f.H != 120 {
		t.Fatalf("caption with fixed size must be 320x120, got %dx%d", f.W, f.H)
	}
}

func TestCaptionBisectionPicksLargerFontForLargerBox(t *testing.T) {
	const word = "FIT"
	small, err := Render(Options{Text: word, Caption: true, Size: clip.Size{W: 400, H: 80}})
	if err != nil {
		t.Fatal(err)
	}
	big, err := Render(Options{Text: word, Caption: true, Size: clip.Size{W: 400, H: 240}})
	if err != nil {
		t.Fatal(err)
	}
	// Auto-fit grows the font with the box, so the taller box paints more ink.
	if opaqueCount(t, big) <= opaqueCount(t, small) {
		t.Fatalf("bisection should choose a larger font for a taller box: small=%d big=%d",
			opaqueCount(t, small), opaqueCount(t, big))
	}
}

func TestBreakWordSplitsUnbreakableToken(t *testing.T) {
	fnt, err := DefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	face, err := fnt.faceAt(40)
	if err != nil {
		t.Fatal(err)
	}
	lines := wrapText(face, "supercalifragilistic", 80)
	if len(lines) < 2 {
		t.Fatalf("an over-wide token must break across lines, got %d", len(lines))
	}
	for _, ln := range lines {
		if !fitsWidth(face, ln, 80) {
			t.Fatalf("broken piece %q exceeds width 80", ln)
		}
	}
}

func TestNewProducesStaticParallelSafeClip(t *testing.T) {
	n, err := New(Options{Text: "static", FontSize: 36})
	if err != nil {
		t.Fatal(err)
	}
	var vc video.VideoClip = n
	if !vc.ParallelSafe() || vc.SourceAccess() != video.AccessStatic {
		t.Fatal("text clip must be parallel-safe and AccessStatic")
	}
	if !vc.HasMask() {
		t.Fatal("transparent text clip must carry a mask")
	}
	// Rendering at different times yields identical pixels (static).
	a := clip.NewFrame(n.Size().W, n.Size().H, clip.RGB24)
	b := clip.NewFrame(n.Size().W, n.Size().H, clip.RGB24)
	if err := vc.FrameInto(context.Background(), 0, a); err != nil {
		t.Fatal(err)
	}
	if err := vc.FrameInto(context.Background(), clip.Time(5e9), b); err != nil {
		t.Fatal(err)
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatal("static text must render identically across time")
		}
	}
}
