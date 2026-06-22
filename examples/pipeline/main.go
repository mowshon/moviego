// Command pipeline demonstrates the parallel render pipeline added in stage 4.
//
// It downloads a public video into examples/input on first run, then exports a
// trimmed, scaled, faded version twice: once forced to a single worker (the
// sequential engine) and once with the worker pool (the parallel pipeline).
package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

func main() {
	ctx := context.Background()

	source, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}

	// Build a linear, monotonic graph (trim -> scale -> fade). The planner
	//    classifies this as linear-streamable, so the multi-worker export runs
	//    on the parallel pipeline while the single-worker export runs on the
	//    sequential engine. Both must produce the same visible result.
	// build returns the derived clip plus the source handle it was opened from.
	// Derived transform/time nodes deliberately do not own the source, so the
	// caller must close the returned source handle to release the decoder.
	build := func() (clip, src *mgo.Video) {
		v, err := mgo.OpenVideo(source)
		if err != nil {
			log.Fatal(err)
		}
		clip = v.Subclip(mgo.Sec(1), mgo.Sec(5)).
			Resize(0.5).
			FadeIn(time.Second).
			FadeOut(time.Second)
		if clip.Err() != nil {
			v.Close()
			log.Fatal(clip.Err())
		}
		return clip, v
	}

	type run struct {
		name    string
		out     string
		workers int
	}
	sequentialOut, err := assets.OutputPath("pipeline_sequential.mp4")
	if err != nil {
		log.Fatal(err)
	}
	parallelOut, err := assets.OutputPath("pipeline_parallel.mp4")
	if err != nil {
		log.Fatal(err)
	}
	runs := []run{
		{"sequential", sequentialOut, 1},
		{"parallel", parallelOut, runtime.GOMAXPROCS(0)},
	}

	for _, r := range runs {
		clip, src := build()
		fmt.Printf("rendering %q with %d worker(s):\n", r.name, r.workers)
		start := time.Now()
		err := mgo.WriteVideo(ctx, clip, r.out, mgo.ExportOptions{
			Workers:  r.workers,
			Progress: &mgo.BarProgress{Label: r.name},
		})
		// Closing the source releases the decoder; the derived nodes own nothing.
		src.Close()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s in %s", r.out, time.Since(start).Round(time.Millisecond))
	}
}
