package mgo_test

import (
	"context"
	"fmt"
	"image/color"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
)

func ExampleParseTime() {
	d, _ := mgo.ParseTime("01:30.5")
	fmt.Println(d)
	// Output: 1m30.5s
}

func ExampleEaseInOut() {
	fmt.Println(mgo.EaseInOut(0.5))
	// Output: 0.5
}

// ExampleVideo_FlipH mirrors a clip left-to-right.
func ExampleVideo_FlipH() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.FlipH()
}

// ExampleVideo_FlipV mirrors a clip top-to-bottom.
func ExampleVideo_FlipV() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.FlipV()
}

// ExampleVideo_Pad letterboxes a clip onto a larger canvas with colored bars.
func ExampleVideo_Pad() {
	v := mgo.Color(640, 360, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Pad(640, 640, [3]byte{0, 0, 0}) // pillarbox to a square
}

// ExampleVideo_FitTo scales a clip to fit a target frame, preserving aspect.
func ExampleVideo_FitTo() {
	v := mgo.Color(1920, 1080, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.FitTo(1080, 1920) // landscape -> vertical with letterbox bars
}

// ExampleVideo_Blur softens a clip; larger radius is softer.
func ExampleVideo_Blur() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Blur(6)
}

// ExampleVideo_Sharpen enhances edges with an unsharp mask.
func ExampleVideo_Sharpen() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Sharpen(1.5)
}

// ExampleVideo_PositionFunc animates a clip's placement within a composite.
func ExampleVideo_PositionFunc() {
	bg := mgo.Color(1280, 720, [3]byte{0, 0, 0}).WithDuration(2 * time.Second)
	logo := mgo.Color(120, 120, [3]byte{255, 255, 255}).WithDuration(2 * time.Second).
		PositionFunc(func(t mgo.Time) mgo.Position {
			x := int(float64(t) / float64(time.Second) * 100)
			return mgo.At(x, 40)
		})
	_ = mgo.Composite(bg, logo)
}

// ExampleVideo_Fx applies a configured effect that has no named facade method.
func ExampleVideo_Fx() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Fx(effect.ChromaKey{Hex: "#00FF00", Similarity: 0.3})
}

// ExamplePixelFn writes a one-function custom effect that boosts the red channel.
func ExamplePixelFn() {
	tint := effect.PixelFn{
		Name: "tint-red",
		Fn: func(_ clip.Time, dst, src *clip.Frame) error {
			copy(dst.Pix, src.Pix)
			for i := 0; i < len(dst.Pix); i += 3 {
				dst.Pix[i] = 255
			}
			return nil
		},
	}
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Fx(tint)
}

// ExampleDescribe reports the planner's engine choice without rendering.
func ExampleDescribe() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	rep, _ := mgo.Describe(v, mgo.ExportOptions{Rate: mgo.Rate{Num: 24, Den: 1}, Workers: 2})
	fmt.Println(rep.Engine)
	// Output: pipeline
}

// ExampleValidate checks a clip is schedulable before exporting.
func ExampleValidate() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	if err := mgo.Validate(v, mgo.ExportOptions{Rate: mgo.Rate{Num: 24, Den: 1}}); err != nil {
		fmt.Println("not exportable:", err)
		return
	}
	fmt.Println("ok")
	// Output: ok
}

// ExampleProbe reads a file's metadata without building a clip graph.
func ExampleProbe() {
	info, err := mgo.Probe("input.mp4", mgo.ProbeOptions{FPSSource: mgo.FPSAvg})
	if err != nil {
		return
	}
	fmt.Printf("%dx%d %s\n", info.Size.W, info.Size.H, info.Codec)
}

// ExampleExtractFrame grabs a single still from a file.
func ExampleExtractFrame() {
	img, err := mgo.ExtractFrame(context.Background(), "input.mp4", mgo.Sec(3))
	if err != nil {
		return
	}
	_ = img.Bounds()
}

// ExampleWriteFrameSequence renders a clip to numbered PNG files.
func ExampleWriteFrameSequence() {
	v := mgo.Color(320, 240, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_, _ = mgo.WriteFrameSequence(context.Background(), v, "frames", mgo.FrameSequenceOptions{
		Rate: mgo.Rate{Num: 10, Den: 1},
	})
}

// ExampleBarProgress reports export progress with the built-in bar.
func ExampleBarProgress() {
	v := mgo.Color(320, 240, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = mgo.WriteVideo(context.Background(), v, "out.mp4", mgo.ExportOptions{
		Rate:     mgo.Rate{Num: 24, Den: 1},
		Progress: &mgo.BarProgress{Label: "render"},
	})
}

func ExampleVideo_Brightness() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Brightness(0.1)
}

func ExampleVideo_Contrast() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Contrast(1.2)
}

func ExampleVideo_Saturation() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Saturation(1.4)
}

func ExampleVideo_Gamma() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Gamma(2.2)
}

func ExampleVideo_Invert() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Invert()
}

func ExampleVideo_Grayscale() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Grayscale()
}

func ExampleVideo_GaussianBlur() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.GaussianBlur(4)
}

func ExampleVideo_Vignette() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Vignette(0.6)
}

func ExampleVideo_Rotate() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.Rotate(30) // grows the canvas so no corner is clipped
}

// ExampleVideo_Fx_colorGrade chains color ops and a film LUT through the generic
// Fx entry point for effects without a facade one-liner.
func ExampleVideo_Fx_colorGrade() {
	v := mgo.Color(640, 480, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.
		Fx(effect.ColorBalance{Mids: [3]float64{0.05, 0, -0.05}}).
		Fx(effect.HSL{Hue: 10, Sat: 0.2}).
		LUT("film.cube")
}

// ExampleAnimatedText reveals a title with a typewriter effect, then holds it.
func ExampleAnimatedText() {
	title, err := mgo.AnimatedText("Hello", mgo.TextOptions{FontSize: 64, Color: color.White},
		mgo.TextAnim{Type: mgo.AnimTypewriter, Dur: 2 * time.Second, Easing: mgo.EaseOut})
	if err != nil {
		return
	}
	_ = title.WithDuration(5 * time.Second).Position(mgo.Center)
}

// ExampleNewCanvas draws a vector lower-third bar and turns it into a clip.
func ExampleNewCanvas() {
	bar := mgo.NewCanvas(1920, 1080).
		Rect(80, 880, 760, 120, mgo.Paint{Fill: color.RGBA{A: 180}}).
		Line(80, 880, 840, 880, color.RGBA{R: 255, G: 80, B: 0, A: 255}, 4).
		WithDuration(4 * time.Second)
	_ = bar
}

// ExampleVideo_Watermark overlays a logo in the bottom-right corner.
func ExampleVideo_Watermark() {
	v := mgo.Color(1280, 720, [3]byte{0, 0, 0}).WithDuration(time.Second)
	logo := mgo.NewCanvas(120, 60).
		Rect(0, 0, 120, 60, mgo.Paint{Fill: color.RGBA{R: 255, G: 255, B: 255, A: 200}}).
		Image()
	_ = v.Watermark(logo, mgo.Corner(mgo.BottomRight), mgo.WithMargin(20))
}

// ExampleVideo_BurnTimecode stamps a running timecode for dailies/review.
func ExampleVideo_BurnTimecode() {
	v := mgo.Color(1280, 720, [3]byte{0, 0, 0}).WithDuration(time.Second)
	_ = v.BurnTimecode(mgo.TimecodeOptions{Start: mgo.Sec(0), Format: mgo.TCFrames})
}

// ExampleCredits scrolls an end-roll up through a viewport.
func ExampleCredits() {
	roll, err := mgo.Credits(
		[]string{"Directed by", "A. Director", "", "Music by", "B. Composer"},
		mgo.CreditsOptions{Size: mgo.Size{W: 1920, H: 1080}, FontSize: 48, Speed: 120},
	)
	if err != nil {
		return
	}
	_ = roll
}

// ExampleSplitAtCues cuts a video into one clip per subtitle cue.
func ExampleSplitAtCues() {
	v := mgo.Color(640, 360, [3]byte{0, 0, 0}).WithDuration(10 * time.Second)
	cues := []mgo.Cue{{Start: 0, End: mgo.Sec(3)}, {Start: mgo.Sec(3), End: mgo.Sec(7)}}
	parts := mgo.SplitAtCues(v, cues)
	fmt.Println(len(parts))
	// Output: 2
}
