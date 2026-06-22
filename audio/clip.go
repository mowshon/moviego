package audio

import (
	"context"

	"github.com/mowshon/moviego/v2/clip"
)

// AudioClip is the chunk-based audio contract. SamplesInto fills a caller-owned
// buffer; sample-at-a-time access is never part of the model.
type AudioClip interface {
	clip.Clip

	// SamplesInto fills dst with count sample frames starting at start.
	SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error
	SampleRate() int
	Channels() int
}
