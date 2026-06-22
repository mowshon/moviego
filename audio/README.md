# audio — Audio clip graph

Package `audio` defines the `AudioClip` interface and the graph nodes that
implement it. Together with `audio/fx` (gain effects) and `audio/io`
(FFmpeg-backed decoder/encoder), it forms the audio side of MovieGo's editing
model.

Audio is a **sidecar**: it never imports the video packages, so video can carry
an `AudioClip` reference without creating an import cycle. The architecture
mirrors the video package — lazy nodes, immutable "with" copies, rational
timing — but the API is chunk-based rather than frame-based.

---

## Core concept: `AudioClip`

```go
type AudioClip interface {
    clip.Clip                                                        // Start/Duration/End + With* mutators
    SamplesInto(ctx context.Context, start clip.Time, count int,
                dst *clip.AudioBuffer) error
    SampleRate() int
    Channels() int
}
```

`SamplesInto` fills `dst` with `count` interleaved float32 sample frames
beginning at composition time `start`. The buffer is caller-owned; the node
writes into it without retaining a reference. Reads outside the clip's
`[0, Duration)` window are always zero-padded — callers never need to
special-case EOF or boundary conditions.

---

## Node reference

### Source nodes

#### `AudioFileNode` — file-backed audio source

```go
node, err := audio.OpenFile(ctx, "track.mp3")
// or, from already-probed metadata (used by VideoFileNode):
node := audio.NewFileNode(path, sampleRate, channels, duration)
```

Lazily opens an FFmpeg decoder on the first `SamplesInto` call. The decoder is
bound to the first call's context; if the context changes (a new render), the
decoder is replaced so the subprocess is always bound to the live context.

Each `WithStart`/`WithDuration`/`WithEnd` placement copy gets an **independent
decoder**, so two uses of the same file in a mix decode in parallel without
interfering.

`Close` is idempotent and kills the FFmpeg subprocess.

---

### Transform nodes

All transforms wrap an inner `AudioClip`. They own no decoders and do not
propagate `Close` — the caller that opened the source closes it.

Each `With*` mutator returns a new node with a fresh per-node scratch buffer
(see [Thread safety](#thread-safety)).

#### `Subclip(inner, a, b)` — time trim

Maps output time `t` → source time `a + t`. Rules match `video.Subclip`:

- Negative bound counts back from the source duration.
- `b == 0` means "to the end".
- An unbounded source with an open `b` stays unbounded.

```go
trimmed := audio.Subclip(src, 500*time.Millisecond, 2*time.Second)
```

Output frames outside `[0, b-a)` are silenced, so a trimmed sidecar attached
to a longer video stops cleanly rather than playing the inner source past the
trim point.

#### `SubclipDur(inner, offset, dur)` — primitive trim

Like `Subclip`, but takes an already-resolved offset and duration. Used by
`video.Subclip` to ensure the audio sidecar matches the video window exactly.

#### `Speed(inner, factor)` — time-scale

```go
fast := audio.Speed(src, 2.0) // 2× speed, half duration
slow := audio.Speed(src, 0.5) // half speed, double duration
```

Output time `t` reads source time `t * factor`. Samples are linearly
interpolated between adjacent source frames; the sample rate is unchanged.
A factor ≤ 0 is rejected and `inner` is returned unchanged.

#### `Loop(inner, n)` — repeat

```go
looped := audio.Loop(src, 3) // plays three times back-to-back
```

Wraps output sample `s` to source sample `s % srcLen` (counted in samples, not
time, so a non-integer-sample duration wraps cleanly with no drop or
duplicate). Output frames at or past `n * srcDur` are silenced. Requires a
known, positive source duration.

#### `Reverse(inner)` — time flip

```go
rev := audio.Reverse(src)
```

Output sample `s` reads source sample `total - 1 - s`. The pull is a single
contiguous read of the reversed range so the source decoder advances forward.
An unbounded source is returned unchanged.

#### `TimeRemap(inner, mapT, dur)` — arbitrary time warp

```go
rm := audio.TimeRemap(src, func(t clip.Time) clip.Time { return t / 2 }, 2*time.Second)
```

Output time `t` reads source time `mapT(t)`. Samples are linearly interpolated.
Used as the sidecar for `video.TimeRemap` and the `Freeze` family so audio
tracks the remapped video. Output frames outside `[0, dur)` are silenced.

---

### `AudioMixNode` — multi-track mixer

```go
mix := audio.Mix(trackA, trackB, music)
```

Sums any number of children over a shared timeline. The output sample rate and
channel count are the **maximum** across all children; mono children are
duplicated across channels. Each child is gated to its own
`[Start, End)` window. Mismatched sample rates are linearly resampled to the
output rate.

`Mix` is the composition primitive: use it to layer a voice-over on music, to
sync a sidecar at a non-zero start offset, or to join the audio sidecars of a
composite.

```go
// Place voice at 2s into a music bed.
voice := audio.OpenFile(ctx, "voice.mp3")
music := audio.OpenFile(ctx, "music.mp3")
placed := voice.WithStart(2 * time.Second).(audio.AudioClip)
final  := audio.Mix(music, placed)
```

**Note:** `RenderToTemp` samples the root from time 0 and ignores `root.Start()`.
To honor a start offset at render time, wrap the clip in a `Mix` first — the
mixer gates each child to its `[Start, End)` window.

---

## `audio/fx` — Gain effects

All effects wrap an `AudioClip` and scale each sample by a time-varying gain.
They own no state and `Close` is a no-op.

| Function | Curve | Notes |
|---|---|---|
| `MultiplyVolume(inner, factor)` | constant | `0.5` = −6 dB, `2.0` = +6 dB |
| `FadeIn(inner, d)` | linear 0→1 | over first `d` |
| `FadeOut(inner, d)` | linear 1→0 | over last `d`; requires finite duration |
| `EqualPowerFadeIn(inner, d)` | `sqrt(t/d)` | constant-power, for crossfades |
| `EqualPowerFadeOut(inner, d)` | `sqrt((dur-t)/d)` | constant-power, for crossfades |

### Crossfade recipe

Pair `EqualPowerFadeOut` on the outgoing clip with `EqualPowerFadeIn` on the
incoming clip to keep the summed signal power flat (linear fades dip ~3 dB at
the midpoint):

```go
import audiofx "github.com/mowshon/moviego/v2/audio/fx"

out := audiofx.EqualPowerFadeOut(clipA, overlap)
in  := audiofx.EqualPowerFadeIn(clipB, overlap)
mix := audio.Mix(out, in.WithStart(len(clipA)-overlap).(audio.AudioClip))
```

---

## `audio/io` — FFmpeg decoder / encoder

These are low-level building blocks; most callers use the higher-level
`audio.OpenFile` and `audio.RenderToTemp` instead.

### `Decoder`

```go
dec, err := audioio.OpenDecoder(ctx, path, audioio.DecoderOptions{
    SampleRate: 44100, // 0 = native
    Channels:   2,     // 0 = native
})
defer dec.Close()

dst := make([]float32, 1000 * dec.Channels())
err = dec.ReadInto(startFrame, 1000, dst)
```

Reads interleaved float32 PCM from any FFmpeg-readable file. Reads before
frame 0 and past the end of the stream are zero-padded (no error). The decoder
uses the same three-case seek table as the video decoder:

| Condition | Action |
|---|---|
| `target == current` | no-op |
| `target ≤ current + 1_000_000` (forward) | skip (read and discard) |
| otherwise (backward or far forward) | restart FFmpeg |

The 1 000 000-frame threshold (≈22 s at 44.1 kHz) is large because PCM pipe
skips are cheap compared to process restart overhead.

A decoder opened with a `Reaper`-carrying context is registered for
render-scoped cleanup (see `Reaper` below).

### `Encoder`

```go
enc, err := audioio.OpenEncoder(ctx, "out.m4a", audioio.EncoderOptions{
    SampleRate: 44100,
    Channels:   2,
    Codec:      "aac",
    Bitrate:    "192k", // empty = codec default
})
enc.WriteFrames(buf)
enc.Close() // flushes and waits for FFmpeg
```

Writes interleaved float32 frames to an encoded output file via FFmpeg stdin.
Samples are clamped to `[-0.99, 0.99]` and quantized to s16le before
transmission (matching MoviePy's guard against full-scale wrapping).

### `Reaper`

A `Reaper` collects the decoders opened during one render and closes them all
at the end. This covers transient decoders buried in mix-graph branches that
have no other owner to call `Close`:

```go
r := &audioio.Reaper{}
ctx = audioio.WithReaper(ctx, r)
defer r.CloseAll()
// decoders opened under ctx are automatically registered
```

The render driver (`render.renderAudio`) installs a reaper before pulling the
audio graph, so callers of `mgo.WriteVideo` do not need to manage this.

---

## `RenderToTemp` — audio export

```go
err := audio.RenderToTemp(ctx, root, "/tmp/audio.m4a", "aac", "192k", videoDur)
```

Renders `root` from time 0 over `[0, dur)` to the given file. The output is
trimmed or zero-padded to exactly `dur` so it stays in sync with the video
track. The render uses 65 536-frame chunks (~1.5 s at 44.1 kHz).

---

## Thread safety

A single `AudioClip` node instance is **not** safe for concurrent `SamplesInto`
calls. Nodes that own mutable scratch buffers (`AudioMixNode`, `shiftNode`,
`speedNode`, `loopNode`, `reverseNode`, `remapNode`) reuse those buffers
across calls.

The design relies on each `With*` mutator returning a new node with its own
scratch, and on each branch of the graph having its own node instance. The
audio render drives the graph from a single goroutine, so this constraint is
met automatically in normal use.

---

## Timing model

- Time is `clip.Time` (`time.Duration`, nanosecond resolution).
- `clip.SampleIndex(t, sr)` → floor of `t * sr / 1e9` (true floor for negative `t`).
- `clip.SampleTime(idx, sr)` → ceil of `idx * 1e9 / sr` (the earliest timestamp that maps back to `idx`).
- The two functions are an **exact inverse pair** for `idx ≥ 0`: a chunked render
  can advance through consecutive `SampleTime`-based start values and always
  visit every sample exactly once.
- Sub-sample offsets (`offset = 1ns` at 48 kHz) are rounded to the nearest
  sample boundary when converted through `SampleIndex`; audio timing is
  sample-accurate, not sub-sample-accurate.

---

## Common patterns

### Trim and speed-adjust a file

```go
src, _ := audio.OpenFile(ctx, "input.wav")
defer src.Close()

// Play seconds 10–20 at 1.5× speed (≈6.7 s output).
trimmed := audio.Subclip(src, 10*time.Second, 20*time.Second)
faster  := audio.Speed(trimmed, 1.5)
```

### Fade in, loop, fade out

```go
src, _ := audio.OpenFile(ctx, "loop.wav")
defer src.Close()

looped  := audio.Loop(src, 4)
faded   := audiofx.FadeIn(audiofx.FadeOut(looped, 2*time.Second), 2*time.Second)
```

### Mix music under a voice-over

```go
music, _ := audio.OpenFile(ctx, "music.mp3")
voice, _ := audio.OpenFile(ctx, "voice.mp3")
defer music.Close()
defer voice.Close()

quietMusic := audiofx.MultiplyVolume(music, 0.3)
shifted    := voice.WithStart(5 * time.Second).(audio.AudioClip)
final      := audio.Mix(quietMusic, shifted)
```

### Render to a temp file

```go
err := audio.RenderToTemp(ctx, final, "/tmp/mix.m4a", "aac", "192k", totalDur)
```
