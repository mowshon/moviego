// Command subtitles burns captions onto a video from an SRT file.
//
// It downloads a public video and the Inter font into examples/input on first
// run, writes a small demo.srt next to them, parses it, and composites the
// resulting transparent subtitles clip over the footage. The subtitles clip is
// a canvas-sized, mostly-transparent video clip: it shows the active cue (auto
// wrapped to the canvas width, anchored to the bottom) and is fully transparent
// between cues, so it layers straight over the video.
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

const demoSRT = `1
00:00:00,300 --> 00:00:02,000
moviego renders subtitles

2
00:00:02,200 --> 00:00:04,800
SRT and VTT cues are parsed,
then composited over the video

3
00:00:05,000 --> 00:00:07,000
each cue is wrapped, cached, and
drawn only while it is on screen
`

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

	srtPath, err := writeDemoSRT()
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

	subs, err := mgo.SubtitlesSRT(srtPath, mgo.SubtitleOptions{
		Size:        mgo.Size{W: canvasW, H: canvasH},
		FontPath:    fontPath,
		FontSize:    30,
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

	out, err := assets.OutputPath("subtitles.mp4")
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

// writeDemoSRT writes the sample cues to examples/input/demo.srt.
func writeDemoSRT() (string, error) {
	dir, err := assets.InputDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "demo.srt")
	if err := os.WriteFile(path, []byte(demoSRT), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
