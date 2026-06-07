package clip

// AudioBuffer holds interleaved float32 samples (len = Count*Channels), reused
// across chunks by the audio pipeline.
type AudioBuffer struct {
	Samples  []float32
	Count    int // sample frames
	Channels int
}

// NewAudioBuffer allocates a buffer for count sample frames of the given channels.
func NewAudioBuffer(count, channels int) *AudioBuffer {
	return &AudioBuffer{
		Samples:  make([]float32, count*channels),
		Count:    count,
		Channels: channels,
	}
}

// Reset resizes the buffer for count sample frames of the given channels,
// reusing the backing slice when it is large enough.
func (b *AudioBuffer) Reset(count, channels int) {
	n := count * channels
	if cap(b.Samples) < n {
		b.Samples = make([]float32, n)
	} else {
		b.Samples = b.Samples[:n]
	}
	b.Count, b.Channels = count, channels
}
