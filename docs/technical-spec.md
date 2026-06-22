# MovieGo — Full Technical Specification

> **Module:** `github.com/mowshon/moviego/v2` · **Language:** Go 1.26 ·
> **External deps:** `golang.org/x/image` (+ transitive `golang.org/x/text`) ·
> **Runtime dep:** `ffmpeg` and `ffprobe` on `PATH`.
>
> This document describes the project **as built**. It is a reference map of the
> architecture, every package, the full file tree, and the design contracts a
> developer must respect when changing the code. The companion planning docs in
> this directory (`MGO.md`, `MGO_GO.md`, `FEATURES.md`, `future_draft.md`)
> describe the MoviePy analysis and the migration/roadmap; this file describes
> the resulting implementation.

---

## 1. What MovieGo Is

MovieGo is a **scripted (programmatic) video-editing library for Go**. You
assemble video, image, text, and audio clips into a *lazy graph* with a small,
explicit, fluent API, then export the result efficiently through FFmpeg.

It borrows MoviePy's **editing model** — a lazy `time → frame` graph, effects as
transforms, masks and audio as sidecars — and gives it a Go **execution model**:
a typed clip graph, single-owner frame buffers, rational frame timing, a
parallel render pipeline, and FFmpeg-native fast paths that skip Go pixels
entirely when the whole graph is expressible as a filtergraph.

**Guiding rule (repeated throughout the code and docs):** *MoviePy is the editing
model; Go provides the execution model.* Output is **correct and visually
equivalent**, not byte-identical to MoviePy. Every deliberate divergence is
catalogued in [`docs/compatibility.md`](../docs/compatibility.md).

### Headline properties

| Property | How it is achieved |
| --- | --- |
| **Fast** | Linear trim/scale/crop/fade/concat jobs fuse into one FFmpeg filtergraph (no Go pixels, opt-in). When Go does render, per-frame pixel work fans out across a worker pool over a single sequential decode. |
| **Safe** | Single-owner frame buffers moved through channels, idempotent `Close`, context cancellation that kills FFmpeg and unblocks every pipeline stage. No shared mutable decoders behind shallow copies. |
| **Predictable** | Rational `Rate` (never `float64` fps), exact frame timing, bounded caches, and a `parallel == sequential` equivalence guarantee enforced in CI under `-race`. |
| **Small surface** | Explicit graph nodes instead of nested closures; named methods and option structs instead of operator overloading. |

---

## 2. Architecture Overview

### 2.1 Layered model

MovieGo is layered so that dependencies point **downward only** (no cycles). The
root facade package assembles graphs; the subpackages own the execution model.

```
┌──────────────────────────────────────────────────────────────────────┐
│  FACADE  (package mgo — repo root *.go)                                │
│  Ergonomic constructors, option structs, fluent handles (Video/Audio). │
│  Carries build errors on handles; assembles the graph; forwards down.  │
└──────────────────────────────────────────────────────────────────────┘
        │ assembles                              │ drives export
        ▼                                        ▼
┌───────────────────────────────┐      ┌───────────────────────────────┐
│  GRAPH NODES                  │      │  RENDER / EXECUTION            │
│  video/ · audio/ · composite/ │◄─────│  render/  (planner + engines)  │
│  text/  · transition/ · effect│      │  cache/   (bounded LRU)        │
└───────────────────────────────┘      └───────────────────────────────┘
        │ pixel ops          │ shapes/curves        │ shells out
        ▼                    ▼                       ▼
┌───────────────────────────────┐      ┌───────────────────────────────┐
│  PIXEL / DRAW / MATH          │      │  FFMPEG BOUNDARY               │
│  imagex/ · draw/ · ease/      │      │  ffmpeg/ · video/io · audio/io │
│  keyframe/                    │      │  (subprocess lifecycle)        │
└───────────────────────────────┘      └───────────────────────────────┘
        │
        ▼
┌──────────────────────────────────────────────────────────────────────┐
│  FOUNDATION  (package clip)                                            │
│  Time, Rate, Size, Frame, AudioBuffer, FramePool, errors. Depends on   │
│  nothing else in the module — import-safe everywhere.                  │
└──────────────────────────────────────────────────────────────────────┘
```

### 2.2 The five core concepts

1. **The clip graph is lazy.** A `*Video` or `*Audio` is a *description*, not
   pixels. Effects return new clips; composites pull their children's frames at
   render time. Decoders are created at export time and owned by exactly one
   goroutine.
2. **Time is rational.** Frame rate is `Rate{Num, Den}` — `30000/1001` exactly,
   never a drifting `float64`. A `Time` is a `time.Duration` alias.
3. **Duration is a single value.** `Duration()`/`End()` return one `Time`. A
   clip with no fixed length yet reports `mgo.Infinite`; test it with
   `mgo.Finite(d)`. Export requires a finite duration.
4. **Masks and audio are sidecars.** Transparency is a mask kept separate from
   RGB during editing; audio rides along as a sidecar. Trimming or speeding up a
   clip transforms its mask and audio too.
5. **Effects are values.** An effect is a config struct applied through a named
   method (`Resize`, `FadeIn`, …) or the generic `Fx(effect.X{…})`.

### 2.3 Error-carrying fluent handles

`Video` and `Audio` handles carry a first-error field. Every method returns a
**new** handle (never mutates the receiver) and propagates the error, so a chain
like `v.Blur(...).Rotate(...).WriteVideo(...)` stops at the first error and
surfaces it from `WriteVideo` / `Err()` / `Validate()` **before any I/O**.

---

## 3. Module-by-Module Specification

### 3.1 `clip/` — Foundation value types (10 files)

The bottom of the dependency graph. Defines the shared vocabulary used by every
other package and imports nothing else in the module.

**Key types**

- `Clip` — interface: `Start/Duration/End` accessors, `WithStart/WithDuration/WithEnd` mutators (return new), `Close`.
- `Time` — alias for `time.Duration`; all timestamps and durations.
- `Infinite` — sentinel (`math.MaxInt64`) for unknown/unbounded length; `Finite(d)` predicate; `DurationOr`/`EndOr` helpers centralize the bookkeeping and guard `start + Infinite` overflow.
- `Rate{Num, Den}` — rational frame rate. `FrameTime(i)` and `TimeToFrame(t)` use `big.Int` to stay exact on multi-hour clips; NTSC snapping (`SnapNTSC`) and `RateFromFloat` conversions.
- `Size{W, H}` — pixel dimensions (W/H order, opposite of NumPy shape).
- `Frame` — packed pixel buffer (`Pix`, `W/H/Stride`, `Format`, private `pool` link). `PixelFormat` is `RGB24` / `RGBA` / `Gray8` (default mask).
- `MaskF32` — high-precision float32 mask, kept separate so the common path never pays for it.
- `AudioBuffer` — interleaved float32 samples (`Count*Channels`), reused across chunks.
- `FramePool` — recycles frame buffers keyed by byte length; `Get`/`Put` enforce the single-owner contract (the pool link, not refcounting), so double-`Put` is a safe no-op.
- `RenderedFrame` — what the export pipeline moves between stages: `RGB *Frame` + optional `Alpha *Frame` (Gray8 sidecar, nil = opaque) + `Index`.
- Sentinel errors: `ErrNoDuration`, `ErrNoRate`, `ErrClosed`, `ErrEOF`, `ErrVideoCorrupted`; `Wrap(op, err)` preserves the chain for `errors.Is`.
- `SampleTime`/`SampleIndex` — exact ceil/floor inverse pair for gap-free audio chunk boundaries.

**Critical points** — Rational rate everywhere (never float fps); `Infinite`
sentinel instead of an `ok` bool; single-owner frames enforced via pool link;
masks are Gray8 sidecars by default, escalating to `MaskF32` only where banding
shows.

### 3.2 `mgo` (repo-root) — The facade (19 non-test files)

The only package most callers import. Re-exports core types (`Time`, `Rate`,
`Size`, `Infinite`/`Finite`) and provides ergonomic constructors + fluent
methods. Assembles graphs and forwards to subpackages; contains **no** execution
logic.

API files grouped by theme:

| File | Public surface |
| --- | --- |
| `video_api.go` | `Video` handle + constructors `OpenVideo/Image/Color/ImageSequence`; metadata; `Subclip/WithDuration/WithStart/Position/Layer`; the full effect method set (`Resize/ResizeTo/Crop/Rotate/FlipH/FlipV/Pad/FitTo/Blur/Sharpen/BlackAndWhite/Grayscale/Brightness/Contrast/Saturation/Gamma/Invert/GaussianBlur/Vignette/LUT/FadeIn/FadeOut/WaterDrop/Speed/Loop`) and the generic `Fx(effect.VideoEffect)`. |
| `audio_api.go` | `Audio` handle + `OpenAudio`, `Mix`, trim/loop/speed/reverse, `Volume/FadeIn/FadeOut`; video↔audio bridge (`AudioClip/WithAudio/WithoutAudio/Volume/AudioFadeIn/AudioFadeOut`). |
| `composite_api.go` | `Position` + keywords (`Center/Left/Right/Top/Bottom`), `At(x,y)`, `RelPos(fx,fy)`; `CompositeOptions`; `Composite`/`CompositeWith`; `Concat`. |
| `canvas_api.go` | `Canvas` builder (`NewCanvas/Background/Rect/Circle/Ellipse/Line/Draw/Image/WithDuration`). |
| `text_api.go` | `Text`, subtitle constructors `Subtitles/SubtitlesSRT/SubtitlesVTT/SubtitlesJSON`; `TextOptions/SubtitleOptions/Cue/Word/Align` re-exports. |
| `motion_api.go` | `AnimatedText`, `Credits`, `SubtitlesASS`, `SplitAtCues`; `TextAnim` + entrance kinds; `BurnTimecode`/`Watermark`; `TimecodeOptions`, `Corner`, `WithMargin`. |
| `keyframe_api.go` | `Animate`/`AnimatePosition`; `Prop` (`PropOpacity/PropScale`); `TimeRemap`, `Freeze/FreezeStart/FreezeEnd`, `Reverse`, `Boomerang`; `Keyframe`, `TimePoint`. |
| `transition_api.go` | Transition constructors (`Crossfade/Dissolve/FadeThroughBlack/FadeThroughColor/Wipe*/Slide*/Push*/IrisOpen/IrisClose`); `ConcatWith`; `Sequence` builder; audio `CrossfadeCurve`. |
| `export_api.go` | `WriteVideo(ctx, v, path, ExportOptions)`; `CodecHEVCAlpha`. |
| `probe_api.go` | `Probe(path, opts…)` → `Info`/`AudioInfo`; `FPSSource`. |
| `frame_api.go` | `ExtractFrame`, `WriteFrameSequence`; `ImageFormat`. |
| `fastpath_api.go` | `StreamCopyTrim`, `ConcatFiles` (FFmpeg-only, no Go pixels). |
| `plan_api.go` | `Describe`/`Validate` → `Report` (engine, fusion reason, frame count, size/rate, memory budget); `Engine` enum. |
| `progress_api.go` | `Progress` interface, `BarProgress`. |
| `ease_api.go`, `time_api.go`, `units.go`, `mgo.go`, `doc.go` | Easing curve re-exports; `ParseTime`; `Sec`; core type aliases; package doc. |

### 3.3 `video/` — Video clip graph nodes (13 files) + `video/io/` (3 files)

Defines the `VideoClip` interface and the concrete source/transform nodes. Owns
**access-class declarations** (scheduling hints) and **filter advertisement**
(fusion hints).

**`VideoClip` interface** — `RenderInto(ctx,t,rgbDst,alphaDst)` (alphaDst may be
nil), `FrameInto` (opaque-only), `MaskInto` (alpha-only), `Size/Rate/HasMask/Audio`,
and the scheduling hints `ParallelSafe()` and `SourceAccess()`.

**Access classes** drive engine choice: `AccessStatic` (image/color),
`AccessLinear` (file passthrough, forward subclip), `AccessBounded` (reserved —
no node emits it yet), `AccessRandom` (reverse, loop, negative speed, backward
remap).

**Source nodes** — `VideoFileNode` (lazy FFmpeg decoder, **not** parallel-safe; a
placement copy via `WithStart/…` gets its own independent reader; rotation
applied at decode so `Size()` is post-rotation; declines fusion when rotated),
`ImageNode` (precomputed RGB + optional Gray8 mask, parallel-safe, supports
`MapStatic` to apply geometry once), `ColorNode` (uniform fill, parallel-safe),
`ImageSequenceNode` (ordered images through a bounded LRU; validates uniform
size/format).

**Transform nodes** — `TimeTransformNode` (remaps output→source time: Subclip,
MultiplySpeed, Loop, Reverse, TimeRemap, Freeze*; advertises a filter only for
forward/positive cases), `TransformNode` (per-frame pixel transform with a
per-node concurrency-safe `FramePool`).

**Supporting interfaces** — `Parent` (`Children()`), `FileSource`
(`SourceKey/SourcePath/SourceSize/SourceRate` — placement copies dedupe by key),
`FrameProvider` (render planner installs central decoding), `Filterable`/`FilterSource`
(opt-in FFmpeg filter advertisement). `WalkSources` enumerates distinct sources.

**`video/io/`** — `Decoder` (reads packed RGB24; seek state machine: no-op /
skip-nearby / restart-on-backward-or-far-forward(>100 frames); two-pass `-ss`)
and `Encoder` (validates alpha codec/pixfmt combos at launch — transparent
output is **never silently dropped**; enforces even dims for 4:2:0; passes
hardware-encoder flags).

**Critical points** — All `RenderInto`/`FrameInto` copy into the caller buffer,
never hand out internal references; placement copies are independent decoders;
reverse/loop/backward-remap force `AccessRandom` → sequential engine; time
transforms carry and remap the audio sidecar identically.

### 3.4 `ffmpeg/` — Subprocess boundary (11 files)

Owns FFmpeg/ffprobe process lifecycle, binary discovery, metadata extraction,
and filtergraph/command assembly. Guarantees no orphan children.

- **Process** — `Spec`/`Proc`/`ExitError`. Context-aware launch; on Unix the
  whole **process group** is SIGKILL'd on cancel (`proc_unix.go`); other
  platforms kill the direct child only (`proc_other.go`, known gap). A `WaitDelay`
  (~10 s) backstops hung children. Stderr is drained into a lock-free 64 KiB ring
  (`ring.go`) so it never blocks and the tail survives for error reporting.
- **Probe** — `Probe` → `MediaInfo`/`VideoStream`/`AudioStream`; `FPSSource`
  (`FPSDefault`=r_frame_rate / `FPSAvg` / `FPSTbr`); VFR detection; rotation
  normalized to 0/90/180/270; `RawSize` is pre-rotation.
- **Filtergraph** — `FilterGraph`/`Pad`/`Constraint`/`FilterContext`/`FilterFragment`
  + builders (`Scale/Crop/Transpose/RotateAngle/EqColor/ColorBalance/Hue/Lut3D/
  GBlur/HFlip/VFlip/Trim/SetPTS/Fade/AFade/FPS/Concat/Overlay/Volume`).
- **Fast paths** — `StreamCopyTrim` (keyframe-accurate, `-c copy`, no re-encode),
  `ConcatDemux` (concat demuxer for compatible files).
- **Discovery/escape** — `FFmpegPath`/`FFprobePath`/`Version` (honor `MGO_FFMPEG`/
  `MGO_FFPROBE`, cached); `SafeInputPath` (`./`-prefixes names starting with `-`);
  filtergraph option escaping.

> **Env var name bug:** The README documents the overrides as `MGO_FFMPEG`/
> `MGO_FFPROBE`, but the code actually reads **`MGO_FFMPEG`/`MGO_FFPROBE`**
> (`ffmpeg/locate.go:14-15`, `EnvFFmpeg`/`EnvFFprobe`). The `MPY_` spelling is a
> leftover from the MoviePy-derived prototype. The README is wrong — the working
> override today is `MPY_*`. This should be reconciled (see §5.13).

### 3.5 `render/` — Planner + engines (13 files) and `cache/` (2 files)

The orchestration layer. `BuildPlan` classifies the graph and picks an engine;
`WriteVideo` (in `export.go`) drives plan → audio render → engine dispatch.

**Engines (`Engine` enum)**

- `EngineSequential` (`sequential.go`) — single goroutine; the **correctness
  oracle**. Used for random/bounded access. For backward walks it installs a
  `reverseProvider` (`reverse.go`) — a ~256 MiB sliding window of decoded frames
  that turns O(N²) decoder restarts into O(N) decodes.
- `EnginePipeline` (`pipeline.go`) — the parallel **3-stage pipeline** for
  static/linear graphs:
  1. **Sequential decoder per source** (`source.go`): one `sourceProvider` owns
     each decoder; a cooperative driver advances it while others wait on a
     condition variable; pooled single-owner buffers released when no reader
     needs them.
  2. **Worker pool**: `plan.Workers` goroutines call `root.RenderInto`
     concurrently on dispatched indices.
  3. **Reorder + encode-feed** (single goroutine): buffers out-of-order results
     (`pending` map, bounded by `ReorderDepth`) and writes contiguous frames in
     index order, matching the sequential engine exactly.
- `EngineFFmpegOnly` (`ffmpegonly.go` + `fuse.go`) — **opt-in** single FFmpeg
  filtergraph invocation; reads `-progress` to drive the callback; falls back
  cleanly to a Go engine (reason logged) when the graph isn't expressible.

**Classification (`Class`)** — derived from the least-linear leaf's
`SourceAccess()`: `ClassStatic/Linear/Bounded/Random`. `pipelineEligible` gates
the pipeline on (1) a single forward pass per source (`maxSourceMultiplicity ≤ 1`)
and (2) no concurrency-unsafe non-file leaf. Anything else, or `Workers ≤ 1`, or
Bounded/Random class → Sequential.

**`Plan`** carries `Rate, Frames, Transparent, Size, Class, Engine, Workers,
FusionReason, ReorderDepth, MaxInflight, Budget` (memory ceiling for
observability). `Describe`/`Validate` (`describe.go`) expose this without
rendering. `Progress` (`progress.go`, `barprogress.go`) is driven only by the
single-threaded encode-feed.

**`cache/`** — generic bounded `LRU[K,V]` (mutex-guarded, eviction at capacity,
disabled at `cap ≤ 0`). Optimization only; a miss always recomputes, never a
correctness dependency.

**Critical points** — `parallel == sequential` is the keystone invariant
(asserted byte-for-byte in CI under `-race`); cancellation must propagate to
FFmpeg immediately; frames must be released exactly once; early EOF is graceful
(not an error) and both engines stop at the same `minEOF` frame.

### 3.6 `audio/` (8 files) + `audio/fx/` (5) + `audio/io/` (4)

Audio is a **sidecar**, never part of the video graph (avoids import cycles). A
`VideoClip.Audio()` may return an `AudioClip`.

- **`audio/`** — `AudioClip` interface (chunk-based `SamplesInto(ctx, start,
  count, dst)` + `SampleRate/Channels`). Nodes: `AudioFileNode` (lazy
  per-placement FFmpeg decoder, zero-pads outside bounds), `AudioMixNode` (`Mix`;
  sums children over `max(rate, channels)`, gates each to `[start,end)`,
  linear-resamples mismatched rates, up-mixes mono→stereo), `reverseNode`,
  `remapNode` (linear interpolation), Subclip/Speed/Loop/Shift, and `RenderToTemp`
  (renders root to a temp file, trim/zero-pad to the video's exact duration).
- **`audio/fx/`** — `gainNode` wrappers: `MultiplyVolume`, linear `FadeIn/FadeOut`,
  and constant-power (√-curve) `EqualPowerFadeIn/Out` for flat crossfades.
- **`audio/io/`** — `Decoder` (FFmpeg → s16le PCM → float32, seek=no-op/skip/
  restart>1M frames), `Encoder` (float32 clamped to [-0.99,0.99] → s16le → codec),
  `Reaper` (render-scoped cleanup so transient mix decoders aren't orphaned).

**Critical points** — Each mix node reuses a per-node scratch buffer → a single
node is **not** concurrency-safe, but each `With*` variant and each branch gets
its own; sidecar `Start` is gated inside `Mix` but ignored by top-level
`RenderToTemp`; Loop/modulo ops need a known duration.

### 3.7 `composite/` (7 files) + `composite/blend/` (3 files)

`CompositeNode` is the timeline compositor: pulls each child at its local time,
positions it, and blends over a canvas. It holds **no mutable per-frame state**
(scratch is per-call), so `RenderInto` is parallel-safe **iff all children are**.

- `CompositeChild{Clip, Start, Pos, Layer}` — children stable-sorted by `Layer`.
- `Position` — keyword / absolute `At(x,y)` / relative `RelPos` (fraction of
  **free space** = canvas − child, a deliberate divergence from MoviePy's
  fraction-of-canvas) / `Animated(t)` function.
- `ComputePosition` + `Rect.Overlap` clip a child against the canvas.
- `ConcatChainNode` (same-size, hard cut) and `ConcatComposeNode` (mixed sizes,
  centered on max canvas, optional padding/overlap).
- `transitionNode` — overlap node for an A→B crossfade (video via a
  `transition.Transition`; audio via a `CrossfadeCurve`).
- `Sequence` — stitches clips with transitions, trimming interior bodies to make
  room for overlaps.
- **`composite/blend/`** — four integer compositing kernels selected by (dst has
  mask?, src has mask?): `CopyRGBRow` (opaque→opaque), `+FillAlphaRow`
  (opaque→transparent), `OverOpaqueRow` (masked→opaque), `OverRow` (premultiplied
  masked→masked). `LerpRow` cross-fades rows. `ref.go` holds float64 reference
  impls; tests assert integer kernels stay within ±1/channel.

**Critical points** — z-order is a stable sort; relative position is fraction of
free space; `regionMin` short-circuits fully-opaque regions to the cheap path;
the `divide-by-255` rounding trick `(t+(t>>8))>>8` keeps the hot path
integer-only; a child placed at `t` plays its audio from its own 0, gated by the
mixer.

### 3.8 `effect/` — Effect catalog (23 files)

The effect model is explicit and testable. `EffectTargets{Video, Mask, Audio}`
declares (via `Targets()`) which sidecars an effect touches — replacing
MoviePy's hidden decorators. Two patterns:

- **`PixelFn`** — low-burden hook for same-size per-frame transforms: `{Name, Fn,
  Sidecars, Start, Filter}`. `Fn(t, dst, src)` must not retain frames; when
  `Sidecars.Mask` is set it runs identically on the Gray8 alpha; the optional
  `Filter` (FFmpeg fusion advertisement) is dropped when `Start != 0`.
- **`geometry()`** — helper for size-changing ops; maps static sources eagerly,
  wraps dynamic ones in a `TransformNode`, advertises a filter where safe.

**Catalog**

| Group | Effects |
| --- | --- |
| Framing/geometry | `Crop`, `Resize` (CatmullRom), `Rotate` (lossless quarter-turn or bilinear arbitrary, optional `Expand`), `Pad`, `FitTo`, `FlipH`, `FlipV` |
| Fading/opacity | `FadeIn`, `FadeOut` (toward a color), `Opacity` (keyframed alpha) |
| Color grading (LUT-backed) | `Brightness`, `Contrast`, `Gamma`, `Invert`, `Saturation`, `ColorBalance` (shadows/mids/highlights), `HSL`, `BlackAndWhite`/`Grayscale` |
| 3D LUT | `LUT{Path}` — parses Resolve/Adobe `.cube` (size capped at 144), trilinear sampling |
| Blur/sharpen | `Blur` (downsample/upsample), `GaussianBlur` (separable, O(2r)), `MotionBlur` (directional), `Sharpen` (unsharp mask) |
| Spatial | `Vignette`, `WaterDrop` (ripple), `ChromaKey` (Cb/Cr key, float32 matte → erode → soften → despill) |
| Time | `MultiplySpeed`, `Loop` |
| Animation | `Scale` (keyframed zoom), `Opacity` (keyframed) |

`colorlut.go` holds the LUT machinery (`rgbLUT`, `channelLUT`, `applyRGBLUT`,
`lutEffect`); per-frame cost of a graded chain is three table reads/pixel.
`Chain()` composes effects sequentially.

**FFmpeg fusion equivalents** are advertised for `crop/scale/rotate/eq/
colorbalance/hue/lut3d/gblur/hflip/vflip/fade`; `Blur/Vignette/WaterDrop/Pad/
Invert/MotionBlur/Sharpen/ChromaKey` are Go-only. Partial-timeline effects
(`Start != 0`) decline fusion.

### 3.9 `text/` — Typography, subtitles, motion titles (10 files)

Pure-Go rasterization onto `clip.Frame` via `golang.org/x/image`, with the
embedded Go Regular face as the deterministic default (no system-font lookup).

- `text.go` — `Render`/`New`; layout, word-wrap (with rune-level fallback),
  caption auto-fit by **integer bisection**, MoviePy-compatible height math.
- `font.go` — `LoadFont/ParseFont/DefaultFont`; `*Font` is concurrency-safe, a
  per-size `font.Face` is **not**, so one is built per rasterization.
- `animate.go` — `AnimatedTextNode`; entrance kinds `AnimNone/FadeIn/Typewriter/
  SlideUp|Down|Left|Right/Pop` + `Custom`; typewriter uses an LRU prefix cache.
- `subtitles.go` — `Cue`/`Word`, `SubtitleLayout` (`LayoutCaption` /
  `LayoutWordCenter`); `SubtitlesNode` is a transparent canvas-sized clip with an
  LRU of rendered cues, active unit found by binary search.
- `srt.go`/`json.go`/`ass.go` — parsers: SRT (`,` decimals) + VTT (`.` decimals);
  JSON in two shapes (flat array of string timestamps, or rich object with
  per-word ms timestamps); ASS/SSA common subset (Events/Dialogue, Format-mapped
  columns, override blocks stripped — styles/positioning/karaoke ignored).
- `credits.go` — scrolling roll (duration = travel/speed).
- `timecode.go` — `TimecodeNode`; `TCClock/TCMillis/TCFrames`; canvas fixed to
  the widest readout (no jitter), LRU string cache.

**Critical points** — text is *caption-stable, not pixel-identical* to MoviePy
(golden tests pin geometry, not glyph pixels); overlapping cues resolved by
"latest-starting still in-bounds"; ASS Format line must precede Dialogue.

### 3.10 `transition/` — Transition system (12 files)

A transition is a pure function of progress `p∈[0,1]` over outgoing frame A and
incoming frame B.

- `transition.go` — `Transition{Frame(p,dst,a,b); Name()}`; optional
  `MaskTransition{FrameMask(...)}` for the alpha sidecar; `Func` adapter;
  `clampUnit`/`unitToByte`.
- `decorators.go` — `Eased(t, ease.Func)` and `Reversed(t)` (swap A/B, invert p);
  both preserve `MaskTransition`.
- Built-ins (each also a `MaskTransition`): `CrossFade`, `Dissolve` (8×8 Bayer
  dither), `FadeThroughColor{Color, Mid}`, `Wipe{Dir, Softness}`, `Slide{Dir}`,
  `Push{Dir}` (share `slidepush.go`), `Iris{Shape, Out, Center}` (Circle/Diamond/
  Rectangle via L2/L1/L∞ metrics).
- `direction.go` — `Dir` and `Shape` enums.

**Critical points** — easing can overshoot, so built-ins clamp before indexing
pixels/matrices; transitions are **Go-only** (no fusion); a clip body + its
adjacent transition both reach the same source → multiplicity ≥ 2 → routes the
assembly to the sequential engine.

### 3.11 Pixel/draw/math support

- **`imagex/` (6 files)** — deterministic image I/O (`DecodeFile/DecodeBytes/
  FromImage/EncodePNG/EncodeJPEG/ToImage`) and geometry (`CropInto`, `RotateInto`
  lossless quarter-turns, `FlipH/VInto`, `PadInto`, `RotateAngleInto` bilinear,
  `ResizeInto` CatmullRom) plus alpha split/stack (`SplitAlpha`/`StackAlphaInto`).
  Uses CatmullRom resize (a documented divergence from FFmpeg's bicubic).
- **`draw/` (3 files)** — `Canvas` (fluent op accumulator) + `Shape` interface
  (`Fill`/`Outline`) with `Rect/Circle/Ellipse/Line` primitives, rendered through
  `golang.org/x/image/vector` (same rasterizer as glyphs; circles use the κ≈0.5523
  Bézier constant). Extend by implementing `Shape`.
- **`ease/` (1 file)** — `Func` type + `Linear/EaseIn/EaseOut/EaseInOut/
  EaseSmooth` (smoothstep), all with fixed endpoints for clean composition.
- **`keyframe/` (2 files)** — `Key{At,Val}` + `Track` (sorted keys, per-segment
  easing, `Eval` clamps before/after, never extrapolates). Immutable → parallel-safe.

### 3.12 `internal/` and `examples/`

- **`internal/genmedia/`** — generates small deterministic media fixtures via
  FFmpeg into a caller temp dir (`Available()` skips when toolchain absent).
- **`internal/testmedia/`** — FFmpeg-free in-memory bitmap fixtures (character
  grids) for golden-frame engine tests.
- **`examples/`** — 20+ buildable programs (`trim`, `composite`, `text`, `motion`,
  `slideshow`, `subtitles`/`-json`/`word-captions`, `concat`, `transitions`,
  `keyframes`, `reverse`, `colorgrade`, `audiomix`, `transparent`, `pipeline`,
  `reframe`, `still-image`), plus `input/` (fonts/assets) and `output/`
  (generated artifacts).

---

## 4. Full Folder & File Tree

> Legend: each entry is annotated with what it is *for*. Test files
> (`*_test.go`) are summarized per directory rather than listed individually
> (90 test files total; non-test source ≈ 179 files, ~32 k LOC).

```
moviego/
├── README.md                  Project intro, quick start, cookbook, export options
├── go.mod / go.sum            Module: go 1.26; dep golang.org/x/image (+ x/text)
├── .gitignore
│
├── doc.go                     Package mgo doc (the facade)
├── mgo.go                     Core type aliases: Time, Rate, Size; Infinite/Finite
├── units.go                   Sec(float64) → Time
│
│  ── FACADE API (package mgo) ──
├── video_api.go               Video handle: constructors + all video effect methods
├── audio_api.go               Audio handle + video↔audio bridge methods
├── composite_api.go           Composite / Concat / Position helpers
├── canvas_api.go              Canvas vector-drawing builder
├── text_api.go                Text + SRT/VTT/JSON subtitle constructors
├── motion_api.go              AnimatedText, Credits, ASS subs, timecode, watermark
├── keyframe_api.go            Animate/AnimatePosition, TimeRemap, Freeze, Reverse, Boomerang
├── transition_api.go          Transition constructors + Sequence builder + ConcatWith
├── export_api.go              WriteVideo + CodecHEVCAlpha
├── probe_api.go               Probe → Info/AudioInfo
├── frame_api.go               ExtractFrame, WriteFrameSequence
├── fastpath_api.go            StreamCopyTrim, ConcatFiles (FFmpeg-only)
├── plan_api.go                Describe / Validate → Report; Engine enum
├── progress_api.go            Progress interface + BarProgress
├── ease_api.go                Easing curve re-exports
├── time_api.go                ParseTime
│   (facade tests: facade_test, media_api_test, motion_api_test, keyframe_api_test,
│    transition_api_test, reverse_api_test, fastpath_api_test, fx_api_test,
│    composite_export_test, static_export_test, placement_internal_test,
│    leak_test, example_doc_test, examples_test)
│
├── clip/                      FOUNDATION value types (imports nothing in-module)
│   ├── doc.go                 Package overview
│   ├── clip.go                Clip interface + duration/end helpers
│   ├── time.go                Time alias, Infinite, Finite, ParseTime
│   ├── rate.go                Rate{Num,Den}, FrameTime, TimeToFrame, NTSC snapping
│   ├── frame.go               PixelFormat, Size, Frame, MaskF32
│   ├── sample.go              SampleTime / SampleIndex (audio timing inverse pair)
│   ├── audiobuffer.go         AudioBuffer (interleaved float32)
│   ├── pool.go                FramePool (single-owner buffer recycling)
│   ├── render.go              RenderedFrame (RGB + alpha sidecar + index)
│   └── errors.go              Sentinel errors + Wrap
│
├── video/                     Video clip graph nodes
│   ├── doc.go
│   ├── clip.go                VideoClip interface, AccessClass, StaticMapper
│   ├── source.go              Parent/FileSource/FrameProvider, WalkSources
│   ├── filter.go              Filterable / FilterSource (fusion advertisement)
│   ├── file.go                VideoFileNode (lazy decoder, per-placement reader)
│   ├── image.go               ImageNode (precomputed RGB + mask, MapStatic)
│   ├── color.go               ColorNode (uniform fill)
│   ├── imageseq.go            ImageSequenceNode (LRU-backed image playback)
│   ├── transform.go           TimeTransformNode + TransformNode
│   ├── timeremap.go           TimeRemap, Freeze*, TimePoint
│   ├── reverse.go             Reverse + FrameCount
│   ├── audioattach.go         WithAudio / WithoutAudio
│   ├── iter.go                IterFrames / IterFramesAt
│   └── io/
│       ├── doc.go
│       ├── decoder.go         RGB24 decoder, seek state machine, two-pass -ss
│       └── encoder.go         Frame→FFmpeg encoder, alpha/even-dim validation
│
├── audio/                     Audio sidecar graph
│   ├── clip.go                AudioClip interface (chunk-based)
│   ├── file.go                AudioFileNode (lazy per-placement decoder, zero-pad)
│   ├── mix.go                 AudioMixNode (sum/gate/resample/up-mix)
│   ├── render.go              RenderToTemp (root → temp PCM-ish file)
│   ├── reverse.go             reverseNode (sample mirror)
│   ├── timeremap.go           remapNode (linear interpolation)
│   ├── transform.go           Subclip / Speed / Loop / Shift
│   ├── fx/
│   │   ├── doc.go
│   │   ├── gain.go            Generic gain(t) wrapper
│   │   ├── volume.go          MultiplyVolume
│   │   ├── fade.go            Linear FadeIn / FadeOut
│   │   └── crossfade.go       EqualPower (√) fades
│   └── io/
│       ├── doc.go
│       ├── decoder.go         FFmpeg → s16le → float32, seek/restart
│       ├── encoder.go         float32 (clamped) → s16le → codec
│       └── reaper.go          Render-scoped decoder cleanup
│
├── composite/                 Layering / concat / transition assembly
│   ├── doc.go
│   ├── composite.go           CompositeNode (z-order layering, 4 blend paths, Audio mix)
│   ├── child.go               CompositeChild / Position / PlacementNode
│   ├── position.go            ComputePosition (keyword/abs/rel/animated)
│   ├── rect.go                Rect + Overlap (clip child to canvas)
│   ├── concat.go              ConcatChainNode / ConcatComposeNode
│   ├── transition.go          transitionNode (A→B overlap, video+audio)
│   └── blend/
│       ├── doc.go
│       ├── blend.go           Blend1/CopyRGBRow/LerpRow/FillAlphaRow/OverOpaqueRow/OverRow
│       └── ref.go             float64 reference kernels (tolerance tests)
│
├── effect/                    Effect catalog (PixelFn + geometry helpers)
│   ├── doc.go
│   ├── effect.go              EffectTargets, VideoEffect/AudioEffect, Chain, geometry
│   ├── pixelfn.go             PixelFn hook + ApplyVideo + Start gating
│   ├── crop.go  resize.go  rotate.go  pad.go  flip.go        Framing/geometry
│   ├── fade.go  opacity.go                                   Fading/opacity
│   ├── color.go  colorlut.go  lut.go  blackwhite.go          Color grading + 3D LUT
│   ├── blur.go  gaussianblur.go  sharpen.go                  Blur / sharpen
│   ├── vignette.go  waterdrop.go  chromakey.go               Spatial effects
│   ├── speed.go  loop.go                                     Time transforms
│   ├── scale.go                                              Keyframed zoom
│   ├── README.md                                             Effect catalog + how to extend
│   └── testdata/fuzz/FuzzParseCube/                          .cube parser fuzz corpus
│
├── text/                      Typography, subtitles, motion titles
│   ├── doc.go
│   ├── text.go                Render/New, layout, wrap, caption auto-fit
│   ├── font.go                LoadFont/ParseFont/DefaultFont (embedded Go Regular)
│   ├── animate.go             AnimatedTextNode + entrance animations
│   ├── subtitles.go           Cue/Word, SubtitleLayout, SubtitlesNode
│   ├── srt.go                 SRT + VTT parsers
│   ├── json.go                JSON subtitles (array + rich object shapes)
│   ├── ass.go                 ASS/SSA common-subset parser
│   ├── credits.go             Scrolling credits roll
│   ├── timecode.go            Running timecode burn-in
│   └── README.md
│
├── transition/                Transition system
│   ├── doc.go
│   ├── transition.go          Transition + MaskTransition interfaces, Func adapter
│   ├── decorators.go          Eased / Reversed
│   ├── crossfade.go  dissolve.go  fade.go                    Blend transitions
│   ├── wipe.go  slide.go  push.go  slidepush.go              Geometric transitions
│   ├── iris.go                                               Aperture transition
│   ├── direction.go           Dir / Shape enums
│   └── README.md
│
├── render/                    Planner + render engines
│   ├── doc.go
│   ├── plan.go                BuildPlan, Class/Engine, pipelineEligible, budgets
│   ├── export.go              WriteVideo entry point + encoderSink
│   ├── pipeline.go            3-stage parallel engine
│   ├── sequential.go          Single-goroutine oracle engine
│   ├── source.go              sourceProvider (pipeline decode driver)
│   ├── reverse.go             reverseProvider (backward-buffered reader)
│   ├── fuse.go                FFmpeg-only fusion planner
│   ├── ffmpegonly.go          FFmpeg-only execution + progress parsing
│   ├── audio.go               renderAudio (sidecar → temp file)
│   ├── describe.go            Describe / Validate
│   ├── progress.go            Progress interface + NopProgress
│   └── barprogress.go         BarProgress (stderr bar)
│
├── cache/                     Bounded LRU (optimization only)
│   └── lru.go                 Generic LRU[K,V]
│
├── ffmpeg/                    FFmpeg/ffprobe subprocess boundary
│   ├── doc.go
│   ├── proc.go                Spec/Proc/ExitError, launch + wait
│   ├── proc_unix.go           Process-group SIGKILL on cancel (Unix)
│   ├── proc_other.go          Direct-child kill fallback (non-Unix)
│   ├── probe.go               ffprobe → MediaInfo/Video/AudioStream
│   ├── filtergraph.go         FilterGraph assembly + filter builders
│   ├── streamcopy.go          StreamCopyTrim + ConcatDemux fast paths
│   ├── encoderflags.go        Encoder capability quirks (e.g. VideoToolbox)
│   ├── escape.go              SafeInputPath + filtergraph escaping
│   ├── locate.go              Binary discovery + Version (env overrides, cached)
│   └── ring.go                Lock-free 64 KiB stderr ring buffer
│
├── imagex/                    Image I/O + geometry + alpha
│   ├── doc.go
│   ├── decode.go              DecodeFile/DecodeBytes/FromImage
│   ├── encode.go              EncodePNG/EncodeJPEG/ToImage
│   ├── geometry.go            Crop/Rotate/Flip/Pad/RotateAngle (into dst)
│   ├── resize.go              ResizeInto (CatmullRom)
│   └── alpha.go               SplitAlpha / StackAlphaInto
│
├── draw/                      Vector drawing
│   ├── doc.go
│   ├── canvas.go              Canvas, Paint, Shape interface, Render
│   ├── shape.go               Rect/Circle/Ellipse/Line primitives
│   └── README.md
│
├── ease/                      Easing curves
│   └── ease.go                Func + Linear/EaseIn/Out/InOut/Smooth
│
├── keyframe/                  Scalar property interpolation
│   ├── keyframe.go            Key + Track + Eval
│   └── README.md
│
├── internal/
│   ├── genmedia/              FFmpeg-generated deterministic fixtures
│   └── testmedia/             FFmpeg-free in-memory bitmap fixtures
│
├── examples/                  20+ buildable demo programs (+ input/ assets, output/)
│   ├── README.md
│   ├── trim/ composite/ text/ motion/ slideshow/ concat/ transitions/
│   ├── keyframes/ reverse/ colorgrade/ audiomix/ transparent/ pipeline/
│   ├── reframe/ still-image/ subtitles/ subtitles-json/ word-captions/
│   ├── input/{fonts,assets}/  output/                temp/
│   └── internal/assets/
│
├── docs/                      Behavioral contract docs (freeze the semantics)
│   ├── timing.md              Rational rate, frame counting, subclip/duration rules
│   ├── decoder-semantics.md   Seek/skip/restart state machine, position model
│   ├── sidecar-propagation.md How effects touch mask/audio (EffectTargets matrix)
│   ├── render-capabilities.md Graph classification → engine selection
│   ├── ffmpeg-policy.md       Binary discovery, codecs, pixfmt/alpha, escaping
│   ├── compatibility.md       Every deliberate divergence from MoviePy
│   └── chroma-key.md          Chroma-key pipeline and parameters
│
└── PLAN_MD/                   Planning + analysis docs (this directory)
    ├── TECHNICAL_SPEC.md      ← this document
    ├── MGO.md                 MoviePy 2.2.0 architecture analysis + migration plan
    ├── MGO_GO.md              MovieGo implementation plan (iterations 0–8)
    ├── FEATURES.md            v2 feature roadmap (transition system, etc.)
    └── future_draft.md        Early library review & recommendations
```

---

## 5. Critical Moments — What Developers Must Watch

These are the invariants and footguns. Violating one usually compiles fine and
fails subtly (a race, a leak, a one-frame drift, dropped alpha).

### 5.1 The keystone invariant: `parallel == sequential`

The sequential engine is the **correctness oracle**. CI renders the same graph
on both engines under `-race` and asserts **byte-for-byte** pixel equality. If
you add a node or effect:

- It must produce identical output regardless of engine.
- If its per-frame work cannot be made concurrency-safe, it must report
  `ParallelSafe() == false` (or a non-linear `SourceAccess`) so the planner
  routes it to the sequential engine. Silent non-determinism breaks the suite.

### 5.2 Single-owner frame buffers

Frames come from a `FramePool` and have **exactly one owner** at a time. The
pipeline moves them through channels; the encode-feed releases them after
writing. Rules:

- Release each frame **exactly once**. Dropping a frame without releasing leaks;
  releasing twice is a no-op (the pool link guards it) but signals a logic bug.
- Never hand out a reference to an internal buffer from `RenderInto`/`FrameInto`
  — always copy into the caller's `dst`.

### 5.3 Context cancellation must kill FFmpeg

Cancelling the export context must (a) unblock every pipeline stage and (b)
terminate every FFmpeg subprocess. On Unix the whole process group is SIGKILL'd;
**non-Unix kills only the direct child** (a known gap — helper processes spawned
by FFmpeg can survive on Windows). Any new long-lived subprocess must be bound to
a context and reaped (see `audio/io/reaper.go` for the transient-decoder case).

### 5.4 Rational time, never float fps

Use `Rate{Num,Den}` and the `clip` timing helpers. `FrameTime`/`TimeToFrame` use
`big.Int` to stay exact on long clips. Export uses a **floor** frame count (frame
`i` is emitted while `FrameTime(i) < duration`). Subclip bounds: negative counts
back from the end, `end == 0` means "to end", and one rational tick of overshoot
is tolerated. Don't reintroduce float fps anywhere.

### 5.5 Alpha is never silently dropped

Transparent export validates the codec/pixfmt/container combination **at launch**
and fails early with a typed error (`ErrAlphaUnsupported`, `ErrAlphaContainer`,
`ErrPixFmtNoAlpha`, `ErrCodecPixFmt`) rather than writing an opaque file. If you
add a codec path, keep this guarantee: an explicit `PixFmt` must both look like
an alpha format *and* be listed by `ffmpeg -h encoder=`.

### 5.6 Sidecar propagation (mask + audio)

Every effect declares `Targets()`. When you add one:

- Geometry/timing effects must transform the **mask identically** to RGB (set
  `Sidecars.Mask`), or transparency will desync from the picture.
- Time transforms (subclip/speed/loop/reverse/remap) must remap the **audio
  sidecar** by the same function, or audio drifts out of sync.
- Color effects leave the mask untouched (nil mask fn).
- The propagation matrix is frozen in `docs/sidecar-propagation.md`.

### 5.7 Fusion is opt-in and conservative

`EngineFFmpegOnly` is **off by default** because the Go engine is the oracle.
Even with `EnableFusion`, a graph that isn't fully expressible falls back cleanly
(reason via `ExportOptions.Debugf`). Fused output is YUV-domain and *not*
byte-identical to the Go path (a documented divergence) — never assert pixel
equality across the fusion boundary. Rotated sources and `Start != 0` effects
decline fusion.

### 5.8 Source multiplicity forces sequential

If one source is reachable by more than one path (a clip used twice, or a body
clip plus its adjacent transition both touching the same file),
`maxSourceMultiplicity > 1` and the planner picks the sequential engine. This is
why transition assemblies and reverse/loop graphs are sequential. Expect it;
don't try to force the pipeline.

### 5.9 Per-node scratch buffers are not shared-safe

`AudioMixNode` and some effect nodes reuse a per-node scratch buffer, so a single
node instance cannot be driven concurrently. The design relies on each `With*`
variant and each graph branch having its own node. If you add a node with mutable
scratch, either keep it single-threaded (report not-parallel-safe) or allocate
per-call.

### 5.10 Decoder seek semantics

The video decoder's `pos` is the index of the **next** frame to be read. Seeks
are no-op (same pos) / skip (nearby forward) / restart (backward or far forward).
The two-pass `-ss` (coarse before `-i`, precise after) plus the `TimeToFrame`
`1e-5 s` epsilon keep a frame's timestamp from rounding into the previous frame.
Changing these thresholds changes which frame you get — they are pinned by
golden tests in `video/io` and documented in `docs/decoder-semantics.md`.

### 5.11 Text is caption-stable, not pixel-identical

Golden tests pin *layout geometry* (line breaks, box fit, positions), not glyph
pixels. Don't assert byte-for-byte glyph output. Caption auto-fit is integer
bisection (always yields ≥ 1 px). A per-size `font.Face` is not concurrency-safe
— build one per rasterization.

### 5.12 Positioning model differs from MoviePy

`RelPos`/relative position is a fraction of **free space** (canvas − child), so
`RelPos(0.5, 0.5)` centers the child. This is intentional and listed in
`docs/compatibility.md`. Don't "fix" it to fraction-of-canvas.

### 5.13 Environment-variable name bug (`MGO_` vs `MPY_`)

The README documents the binary overrides as `MGO_FFMPEG`/`MGO_FFPROBE`, but the
code reads **`MGO_FFMPEG`/`MGO_FFPROBE`** (`ffmpeg/locate.go:14-15`, constants
`EnvFFmpeg`/`EnvFFprobe`) — a leftover from the MoviePy-derived prototype. So the
working override today is `MPY_*`, and the documented `MGO_*` does nothing. Fix
by renaming the constants to `MGO_*` (and optionally accepting both for
backward-compat), then the README becomes correct.

### 5.14 Build-artifact hygiene

Building an example (`go build ./examples/motion`) drops its output binary in the
working directory under the package's name (e.g. `motion`). One such binary was
previously committed to the repo root and has since been removed. Guard against a
repeat: don't commit example build outputs, and make sure `.gitignore` covers
them.

---

## 6. Build, Test & Quality Gates

```sh
go build ./...
go vet ./...
gofumpt -l .            # formatting must be clean
go test -race ./...     # keystone: parallel == sequential, no races, no leaks
go test -bench=. ./composite/ ./audio/ ./effect/
```

- The suite favors synthetic bitmap fixtures (`internal/testmedia`) so most tests
  run **without** FFmpeg; tests that genuinely need it (`internal/genmedia`) skip
  cleanly when the toolchain is absent.
- `effect/testdata/fuzz/FuzzParseCube` carries a fuzz corpus for the `.cube` LUT
  parser — keep it green when touching `effect/lut.go`.
- Leak and cancellation tests (`leak_test.go`) assert no goroutine/process leaks
  after cancelled exports.

## 7. Requirements & Runtime

- **Go 1.26** (per `go.mod`; README states a 1.22+ floor for consumers).
- **FFmpeg + ffprobe** on `PATH` (or pointed at by the env overrides — see §5.13).
  MovieGo shells out for all decode, encode, and probing.
- Only third-party dependency: `golang.org/x/image` (CatmullRom resize, the
  vector rasterizer for glyphs/shapes) plus its transitive `golang.org/x/text`.

---

*Generated as a reference map of the codebase. For behavioral contracts that the
implementation must not violate, treat the files in [`docs/`](../docs/) as
authoritative; for the editing-model rationale, see the planning docs alongside
this file in `PLAN_MD/`.*
