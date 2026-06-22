package audio_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// BenchmarkAudioMix measures summing six stereo sine tracks into one 1024-frame
// chunk (the §14 "audio mix" workload), FFmpeg-free.
func BenchmarkAudioMix(b *testing.B) {
	const sr = 48000
	tracks := make([]audio.AudioClip, 0, 6)
	for k := 0; k < 6; k++ {
		freq := float64(220 * (k + 1))
		tracks = append(tracks, newFake(sr, 2, time.Second, func(i int) float32 {
			return float32(0.2 * math.Sin(2*math.Pi*freq*float64(i)/sr))
		}))
	}
	mix := audio.Mix(tracks...)
	buf := &clip.AudioBuffer{}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := mix.SamplesInto(ctx, 0, 1024, buf); err != nil {
			b.Fatal(err)
		}
	}
}
