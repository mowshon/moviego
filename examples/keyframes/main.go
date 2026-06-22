// Command keyframes demonstrates keyframe animation (opacity, scale, position)
// and time remapping (variable speed and freeze frames), all driven by the
// shared ease package.
//
// It downloads a sample video into examples/input on first run, then builds two
// clips: a speed-ramped + freeze-framed "hero" cut, and a still photo animated
// over it (fade + zoom + drift). It prints each clip's render report with
// mgo.Describe so the engine choice (a reversing remap drops to the sequential
// engine; a forward ramp and freeze stay on the pipeline) is visible, then
// exports a short MP4.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

func main() {
	ctx := context.Background()

	videoPath, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}
	photoPath, err := assets.Ensure(ctx, assets.PhotoLake)
	if err != nil {
		log.Fatal(err)
	}

	source, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer source.Close()

	photo, err := mgo.Image(photoPath)
	if err != nil {
		log.Fatal(err)
	}

	const w, h = 640, 360

	// Hero cut: ease into slow motion, then freeze on a beat. Both the speed ramp
	// and the freeze keep a non-decreasing source time, so the clip stays on the
	// parallel pipeline (and the freeze is served from the decoder's frame cache).
	hero := source.Subclip(0, 3*time.Second).FitTo(w, h).
		TimeRemap([]mgo.TimePoint{
			{Out: 0, In: 0},
			{Out: 2 * time.Second, In: time.Second}, // 2x slow-mo
			{Out: 3 * time.Second, In: 3 * time.Second},
		}, mgo.EaseInOut).
		Freeze(2500*time.Millisecond, time.Second)

	// Overlay a photo that fades in, gently zooms, and drifts across the frame —
	// three keyframe tracks composited over the hero cut.
	overlay := photo.WithDuration(hero.Duration()).FitTo(w/2, h/2).
		Animate(mgo.PropOpacity, []mgo.Keyframe{
			{At: 0, Val: 0},
			{At: time.Second, Val: 1},
			{At: hero.Duration() - time.Second, Val: 1},
			{At: hero.Duration(), Val: 0},
		}, mgo.EaseInOut).
		Animate(mgo.PropScale, []mgo.Keyframe{
			{At: 0, Val: 1.0},
			{At: hero.Duration(), Val: 1.25},
		}, mgo.EaseSmooth).
		AnimatePosition([]mgo.PositionKeyframe{
			{At: 0, X: 20, Y: 20},
			{At: hero.Duration(), X: float64(w - w/2 - 20), Y: float64(h - h/2 - 20)},
		}, mgo.EaseInOut)

	final := mgo.Composite(hero, overlay)
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	for _, c := range []struct {
		name string
		v    *mgo.Video
	}{{"hero", hero}, {"final", final}} {
		rep, err := mgo.Describe(c.v, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("describe %-5s: engine=%s class=%s frames=%d transparent=%v\n",
			c.name, rep.Engine, rep.Class, rep.Frames, rep.Transparent)
	}

	out, err := assets.OutputPath("keyframes.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, final, out, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}
