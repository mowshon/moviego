// Command audiomix demonstrates the audio lane end to end: decoding a video's
// own track and a separate music file, trimming both (the video trim propagates
// to its audio), shaping the music with volume and fades, mixing the two, and
// muxing the result back into the exported MP4.
package main

import (
	"context"
	"log"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	videoPath, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		return err
	}
	audioPath, err := assets.Ensure(ctx, assets.SampleAudio)
	if err != nil {
		return err
	}

	// 1. Open the video. It exposes its own audio track as a sidecar.
	vid, err := mgo.OpenVideo(videoPath)
	if err != nil {
		return err
	}
	defer vid.Close()

	// 2. Keep the first 8 seconds. Subclip trims the video AND its audio.
	clip := vid.Subclip(0, mgo.Sec(8))

	// 3. Open the music, trim it to the same window, then lower its volume and
	//    fade it in and out so it sits under the original audio.
	music, err := mgo.OpenAudio(audioPath)
	if err != nil {
		return err
	}
	defer music.Close()
	bed := music.
		Subclip(0, mgo.Sec(8)).
		Volume(0.25).
		FadeIn(2 * time.Second).
		FadeOut(2 * time.Second)

	// 4. Mix the (trimmed) original audio with the music bed and attach it.
	mixed := mgo.Mix(clip.AudioClip(), bed)
	final := clip.WithAudio(mixed)
	if final.Err() != nil {
		return final.Err()
	}

	out, err := assets.OutputPath("audiomix.mp4")
	if err != nil {
		return err
	}
	if err := mgo.WriteVideo(ctx, final, out, mgo.ExportOptions{}); err != nil {
		return err
	}
	log.Printf("wrote %s", out)
	return nil
}
