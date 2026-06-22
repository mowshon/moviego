// Command motion demonstrates moviego's motion-graphics features: a vector-drawn
// lower third (the draw canvas), an animated title and a typewriter subtitle
// (AnimatedText), a watermark logo, and a burned-in timecode — all composited
// over a sample video. It then writes a second file, a scrolling credits roll
// over black.
//
// It downloads a sample video and the Inter font into examples/input on first
// run.
package main

import (
	"context"
	"image/color"
	"log"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

const (
	w, h = 640, 360
	dur  = 6 * time.Second
)

func main() {
	ctx := context.Background()

	videoPath, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}
	fontPath, err := assets.Ensure(ctx, assets.FontInter)
	if err != nil {
		log.Fatal(err)
	}

	writeOverlays(ctx, videoPath, fontPath)
	writeCredits(ctx, fontPath)
}

// writeOverlays composites the drawing, animated text, watermark, and timecode
// over the footage.
func writeOverlays(ctx context.Context, videoPath, fontPath string) {
	src, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer src.Close()

	bg := src.Subclip(0, dur).FitTo(w, h)

	// A vector lower-third: a translucent bar with a colored accent rule, drawn
	// straight into a transparent clip — no image asset needed.
	lowerThird := mgo.NewCanvas(w, h).
		Rect(0, h-90, w, 90, mgo.Paint{Fill: color.RGBA{A: 150}}).
		Line(0, h-90, w, h-90, color.RGBA{R: 255, G: 90, B: 0, A: 255}, 3).
		WithDuration(dur).
		Layer(5)

	// An animated title that slides up into place, and a typewriter subtitle that
	// starts a beat later.
	title, err := mgo.AnimatedText("MovieGo — Motion", textOpts(fontPath, 40),
		mgo.TextAnim{Type: mgo.AnimSlideUp, Dur: 800 * time.Millisecond, Easing: mgo.EaseOut})
	if err != nil {
		log.Fatal(err)
	}
	title = title.WithDuration(dur).Position(mgo.At(24, h-78)).Layer(10)

	subtitle, err := mgo.AnimatedText("text, drawing & timecode", textOpts(fontPath, 22),
		mgo.TextAnim{Type: mgo.AnimTypewriter, Dur: 1500 * time.Millisecond, Easing: mgo.EaseOut})
	if err != nil {
		log.Fatal(err)
	}
	subtitle = subtitle.WithDuration(dur).WithStart(time.Second).Position(mgo.At(24, h-40)).Layer(11)

	// A circular vector logo, watermarked into the top-right corner.
	logo := mgo.NewCanvas(44, 44).
		Circle(22, 22, 20, mgo.Paint{Fill: color.RGBA{R: 255, G: 255, B: 255, A: 220}, Stroke: color.RGBA{R: 255, G: 90, B: 0, A: 255}, StrokeWidth: 3}).
		Image()

	final := mgo.Composite(bg, lowerThird, title, subtitle).
		Watermark(logo, mgo.Corner(mgo.TopRight), mgo.WithMargin(16)).
		BurnTimecode(mgo.TimecodeOptions{Format: mgo.TCFrames, FontPath: fontPath, FontSize: 20})
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	out, err := assets.OutputPath("motion.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, final, out, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}

// writeCredits scrolls an end-roll over a black background.
func writeCredits(ctx context.Context, fontPath string) {
	roll, err := mgo.Credits(
		[]string{
			"MovieGo", "",
			"Directed by", "The Graph", "",
			"Drawing", "x/image/vector", "",
			"Thanks for watching",
		},
		mgo.CreditsOptions{Size: mgo.Size{W: w, H: h}, FontPath: fontPath, FontSize: 28, Speed: 90},
	)
	if err != nil {
		log.Fatal(err)
	}

	bg := mgo.Color(w, h, [3]byte{0, 0, 0}).WithDuration(roll.Duration())
	scene := mgo.Composite(bg, roll.Layer(1))
	if scene.Err() != nil {
		log.Fatal(scene.Err())
	}

	out, err := assets.OutputPath("credits.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, scene, out, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}

// textOpts is the shared title/subtitle styling: white fill with a dark outline
// for legibility over footage.
func textOpts(fontPath string, size float64) mgo.TextOptions {
	return mgo.TextOptions{
		FontPath:    fontPath,
		FontSize:    size,
		Color:       color.White,
		Stroke:      color.RGBA{R: 8, G: 12, B: 18, A: 255},
		StrokeWidth: 2,
	}
}
