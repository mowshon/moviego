# Sidecar Propagation

How every core operation and effect propagates to the **mask** and **audio**
sidecars. This is the explicit replacement for MoviePy's hidden decorators
(`apply_to_mask`, `apply_to_audio`, `audio_video_effect`, `outplace`).
No effect lands without its row tested.

## The contract

Each effect declares an `effect.EffectTargets{Video, Mask, Audio bool}` via
`Targets()`. `TestEffectTargetsMatchMatrix` pins those flags to the table below,
and companion behavioral tests in `effect/propagation_test.go` assert the actual
sidecar handling (e.g. a resize resizes the mask; a fade leaves it byte-for-byte
unchanged). An effect that claims `Mask:false` while altering the mask, or
vice-versa, fails CI.

## Matrix

| Operation | Video (RGB) | Mask | Audio |
| --- | --- | --- | --- |
| `WithStart` | set placement start | same start | same start |
| `WithEnd` / `WithDuration` | set end/duration | mirror | mirror |
| `Subclip` | time transform `t→a+t` | same time transform | same time transform |
| `MultiplySpeed` | time transform `t→t*f` | same | same |
| `Loop` | time transform `t mod dur` | same | same |
| `Resize` | resize frame | resize mask | unchanged |
| `Crop` | crop frame | crop mask | unchanged |
| `Rotate` | rotate frame | rotate mask | unchanged |
| `BlackAndWhite` / `Grayscale` | grayscale RGB | unchanged | unchanged |
| `Brightness`/`Contrast`/`Gamma`/`Invert`/`ColorBalance` | per-channel LUT | unchanged | unchanged |
| `Saturation` / `HSL` | per-pixel color mix | unchanged | unchanged |
| `LUT` (3D `.cube`) | trilinear color map | unchanged | unchanged |
| `Vignette` | corner darkening | unchanged | unchanged |
| `GaussianBlur` / `MotionBlur` | blur RGB | blur mask | unchanged |
| `FadeIn` (toward color) | RGB fade | unchanged | unchanged |
| `FadeOut` | RGB fade | unchanged | unchanged |
| `CrossFadeIn` *(deferred)* | unchanged | mask fade | unchanged |
| `WithOpacity` *(deferred)* | unchanged | scale mask | unchanged |
| `MultiplyVolume` *(audio lane)* | unchanged | unchanged | transform audio |
| `AudioFadeIn/Out` *(audio lane)* | unchanged | unchanged | transform audio |
| `Composite` | layer children | layer masks | mix children at each child start |
| `Concat` (chain/compose) | sequence children | sequence masks | sequence children at cumulative start |

## How it is realized in the graph

- **Time transforms** (`Subclip`, `MultiplySpeed`, `Loop`) are a single
  `TimeTransformNode` that wraps the whole clip. The mask and audio propagate
  automatically because rendering delegates the mapped time to the inner clip's
  own `RenderInto`/`MaskInto`/`Audio`. The `Targets()` for these effects mark
  Video, Mask, and Audio.
- **Geometry transforms** (`Resize`, `Crop`, `Rotate`) apply the *same* pixel
  operation to the RGB and, when present, to the Gray8 mask. For a static source
  (`StaticMapper`) this is done once eagerly; otherwise a `TransformNode`
  applies `rgbFn` and `maskFn` per frame. `Targets()` mark Video and Mask.
- **Color transforms** (`BlackAndWhite`, `FadeIn`, `FadeOut`) set a per-frame
  `TransformNode` with a `nil` mask function, so the mask passes through
  verbatim. `Targets()` mark Video only.

## Known gaps (tracked, not silent)

- **Audio time-mapping is implemented.** `Subclip`, `MultiplySpeed`,
  and `Loop` now remap the audio sidecar through `TimeTransformNode.audioClip`:
  a subclip shifts and trims the audio (`audio.Subclip`), a speed change
  linearly interpolates it (`audio.Speed`, which also shifts pitch, like
  MoviePy), and a loop wraps it on the source sample count (`audio.Loop`) so a
  source duration that is not a whole number of samples neither drops nor
  duplicates a sample at a loop boundary. `VideoFileNode.Audio()` exposes a
  file's own track, and `WithStart`/`WithDuration`/`WithEnd` mirror their
  timeline change onto the audio. Behavioral tests live in
  `audio/transform_test.go`.
- **Resampling is linear interpolation, not hidden.** When a mixer's children
  differ in sample rate, `AudioMixNode` resamples each to the mix rate (the max
  child rate) by linear interpolation between adjacent source samples; the
  equal-rate path (the common case) stays exact 1:1. Linear interpolation avoids
  the zipper/aliasing of nearest-neighbor; decoding each source at the mix rate
  via `DecoderOptions.SampleRate` would avoid the Go resampler entirely. Tested
  in `audio/mix_test.go`
  (`TestMixResampleLinear`, `TestMixResampleChunkContinuity`).
- **Composites and concats surface mixed child audio.**
  `CompositeNode.Audio()` returns an `audio.Mix` of every child's sidecar, each
  placed at the child's composite start so it stays in lockstep with the child's
  video (which renders at local time `t-ch.Start`); overlapping children sum.
  `ConcatChainNode.Audio()` places each child at its cumulative start so the
  tracks play back to back with no overlap (the half-open mix gate). A single
  audio-bearing child is still wrapped in a `Mix` so its placement is honored on
  render (a bare audio node ignores its own `Start`; only the mixer applies it).
  A duration override on the node (`WithDuration`/`WithEnd(changeDuration)`) is
  mirrored onto the mix (`mix.WithDuration(n.dur)`) — composite only when its
  duration is known, concat always — so a shortened node nested in a parent
  truncates its audio to match video gating instead of over-playing the full
  child window. `ConcatComposeNode` inherits the composite behavior. Behavioral
  tests: `composite/audio_test.go` (including the nested-shortened cases);
  end-to-end mux tests: `render.TestWriteVideo{Composite,Concat}MuxesAudio`.
- **Sample indexing floors toward −∞.** `clip.SampleIndex` is a true floor (not a
  truncation toward zero), so a clip placed at a negative or sub-sample offset
  gates at the correct sample and an out-of-range check around zero is not
  silently swallowed (`clip.TestSampleIndexFloorsNegative*`).
- **Export bitrate is honored.** `ExportOptions.AudioBitrate` flows through
  `renderAudio` → `audio.RenderToTemp` → `audioio.EncoderOptions.Bitrate`
  (`-b:a`), so a requested rate reaches FFmpeg (`audio.TestRenderToTempBitrate`).
- **Ownership is single-owner.** Every audio wrapper (`shiftNode`, `speedNode`,
  `loopNode`, `AudioMixNode`, the `audiofx` gain effects) closes nothing; the
  handle that opened a source closes it, and a `VideoFileNode` closes the audio
  sidecar it created in `OpenFile`. This mirrors the video nodes and keeps a
  trimmed/effected wrapper from tearing down a source another branch still uses
  (gated by `video.TestVideoFileNodeCloses*`).
- **`CrossFadeIn` and `WithOpacity` are deferred** (mask-only effects) and are
  listed here so their rows are reserved; they are not yet implemented.
