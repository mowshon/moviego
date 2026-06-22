// Command word-captions renders social-style, one-word-at-a-time captions over
// a vertical (9:16) video.
//
// It writes a demo.json in the rich object form — {styles, cues:[{…, words:[…]}]}
// with millisecond-number timestamps and per-word timing — then loads it with
// mgo.SubtitlesJSON using LayoutWordCenter. Each word is centered on the canvas
// and shown only during its own [start, end) window, the look used by vertical
// short-form videos. The text color comes from the JSON styles block (the
// caller leaves Color unset so the style drives it); VPos nudges the words to
// the lower third.
package main

import (
	"context"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

// demoJSON is the rich object form: a styles block, millisecond timestamps, and
// per-word timing that drives the word-centered layout.
const demoJSON = `{
  "styles": { "font": "Arial", "color": "#FFE600" },
  "cues": [
    {
      "start": 300, "end": 2700, "text": "make every word pop",
      "words": [
        {"word": "make",  "start": 300,  "end": 800},
        {"word": "every", "start": 850,  "end": 1500},
        {"word": "word",  "start": 1550, "end": 2100},
        {"word": "pop",   "start": 2150, "end": 2700}
      ]
    },
    {
      "start": 3000, "end": 6000, "text": "centered for vertical video",
      "words": [
        {"word": "centered", "start": 3000, "end": 3700},
        {"word": "for",      "start": 3750, "end": 4100},
        {"word": "vertical", "start": 4150, "end": 5000},
        {"word": "video",    "start": 5050, "end": 6000}
      ]
    }
  ]
}`

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

	jsonPath, err := writeDemoJSON("word-captions.json", demoJSON)
	if err != nil {
		log.Fatal(err)
	}

	// A 9:16 portrait canvas; the landscape sample is centre-cropped to fill it.
	const canvasW, canvasH = 360, 640
	const d = 6 * time.Second
	const cropW = 202 // 360 * 9/16, the widest 9:16 column of a 640x360 frame

	bg, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bg.Close()
	background := bg.Subclip(0, d).
		Crop((640-cropW)/2, 0, cropW, 360).
		ResizeTo(canvasW, canvasH)

	// Color is intentionally left unset so the JSON styles block ("#FFE600")
	// drives it; FontPath is set explicitly because the style's "Arial" is a
	// family name with no font file and is ignored.
	subs, err := mgo.SubtitlesJSON(jsonPath, mgo.SubtitleOptions{
		Size:        mgo.Size{W: canvasW, H: canvasH},
		FontPath:    fontPath,
		FontSize:    56,
		Stroke:      color.Black,
		StrokeWidth: 3,
		Layout:      mgo.LayoutWordCenter,
		VPos:        0.7, // lower third, typical of social captions
	})
	if err != nil {
		log.Fatal(err)
	}
	subs = subs.Layer(10)

	final := mgo.CompositeWith(mgo.CompositeOptions{
		Size: mgo.Size{W: canvasW, H: canvasH},
	}, background, subs)
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	out, err := assets.OutputPath("word-captions.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, final, out, mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}

// writeDemoJSON writes content to examples/input/<name> and returns its path.
func writeDemoJSON(name, content string) (string, error) {
	dir, err := assets.InputDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
