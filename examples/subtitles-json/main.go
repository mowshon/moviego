// Command subtitles-json burns captions onto a video from a JSON subtitle file.
//
// It writes a demo.json in the flat array form — a top-level list of
// {start, end, text} objects with string timestamps ("HH:MM:SS,mmm") — then
// loads it with mgo.SubtitlesJSON and composites the resulting transparent
// subtitles clip over real footage as wrapped, bottom-anchored captions. This
// is the JSON counterpart of the `subtitles` (SRT) example.
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

// demoJSON is the flat array form: string timestamps, no styles, no word timing.
const demoJSON = `[
  {"start": "00:00:00,300", "end": "00:00:02,000", "text": "moviego loads subtitles from JSON"},
  {"start": "00:00:02,200", "end": "00:00:04,800", "text": "the flat array form uses string timestamps, just like SRT"},
  {"start": "00:00:05,000", "end": "00:00:07,000", "text": "each cue is wrapped, cached, and drawn only while on screen"}
]`

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

	jsonPath, err := writeDemoJSON("subtitles.json", demoJSON)
	if err != nil {
		log.Fatal(err)
	}

	const canvasW, canvasH = 640, 360
	const d = 7 * time.Second

	bg, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bg.Close()
	background := bg.Subclip(0, d).ResizeTo(canvasW, canvasH)

	subs, err := mgo.SubtitlesJSON(jsonPath, mgo.SubtitleOptions{
		Size:        mgo.Size{W: canvasW, H: canvasH},
		FontPath:    fontPath,
		FontSize:    28,
		Color:       color.White,
		Stroke:      color.Black,
		StrokeWidth: 2,
		Align:       mgo.AlignCenter,
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

	out, err := assets.OutputPath("subtitles-json.mp4")
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
