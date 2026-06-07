// Package audiofx holds audio effects: volume scaling and fades. Each effect
// wraps an audio.AudioClip and returns a new clip whose SamplesInto applies a
// per-sample gain; the wrapped clip is never mutated.
package audiofx
