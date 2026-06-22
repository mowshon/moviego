<p align="center">
  <img src="docs/assets/moviego-logo.png" alt="MovieGo" width="520">
</p>

<p align="center">
  <strong>Scripted video editing for Go.</strong><br>
  Compose video, image, text, and audio clips with a small, explicit API — then export efficiently through FFmpeg.
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/mowshon/moviego/v2"><img src="https://pkg.go.dev/badge/github.com/mowshon/moviego/v2.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/mowshon/moviego/v2"><img src="https://goreportcard.com/badge/github.com/mowshon/moviego/v2" alt="Go Report Card"></a>
  <a href="https://github.com/mowshon/moviego"><img src="https://img.shields.io/badge/github-mowshon%2Fmoviego-181717?logo=github" alt="GitHub"></a>
  <a href="#license"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT License"></a>
</p>

---

MovieGo takes the editing model that made [MoviePy](https://github.com/Zulko/moviepy)
pleasant — a lazy `time → frame` graph, effects as transforms, masks and audio as
sidecars — and gives it a **Go execution model**: a typed clip graph, single-owner
frame buffers, rational frame timing, a planned parallel render pipeline, and
FFmpeg-native fast paths that skip Go pixels entirely when the whole graph is
expressible as a filtergraph.

You describe *what* you want — trim this, scale that, fade here, overlay a title —
and MovieGo figures out the fastest correct way to produce it.

```go
out := clip.Subclip(mgo.Sec(1), mgo.Sec(4)).Resize(0.5).FadeIn(time.Second)
mgo.WriteVideo(ctx, out, "out.mp4", mgo.ExportOptions{})
```

> **Status:** core feature-complete and tested (`go test -race ./...` green).
> Covers open/trim/transform/composite/concat, text & subtitles, audio mix & mux,
> transparent export, the parallel render pipeline, and the opt-in FFmpeg-only fast path.

---

## Table of contents

- [Why MovieGo](#why-moviego)
- [Requirements](#requirements)
- [Install](#install)
- [Quick start](#quick-start)
- [Core concepts](#core-concepts)
- [Cookbook](#cookbook)
  - [Trim, resize & fade](#trim-resize--fade)
  - [Composite layers & positioning](#composite-layers--positioning)
  - [Text, titles & captions](#text-titles--captions)
  - [Subtitles](#subtitles)
  - [Effects & color grading](#effects--color-grading)
  - [Time, speed & motion](#time-speed--motion)
  - [Transitions](#transitions)
  - [Vector drawing & motion graphics](#vector-drawing--motion-graphics)
  - [Audio](#audio)
  - [Transparent export](#transparent-export)
  - [FFmpeg-only fast paths](#ffmpeg-only-fast-paths)
  - [Probing & extracting frames](#probing--extracting-frames)
- [Export options](#export-options)
- [How rendering works](#how-rendering-works)
- [Best practices](#best-practices)
- [Package map](#package-map)
- [Runnable examples](#runnable-examples)
- [Contract documentation](#contract-documentation)
- [Development](#development)
- [License](#license)

---

## Why MovieGo

| | |
| --- | --- |
| ⚡ **Fast** | Most social / template / slideshow jobs are pure trim + scale + crop + fade + concat — expressible as one FFmpeg filtergraph, so MovieGo can run them without ever touching Go pixels (opt-in). When it does render, per-frame pixel work fans out across a worker pool over a single sequential decode. |
| 🔒 **Safe** | Single-owner frame buffers transferred through channels, idempotent `Close`, and context cancellation that kills FFmpeg and unblocks every stage. No shared mutable decoders behind shallow copies. |
| 🎯 **Predictable** | Rational `Rate` (never `float64` fps), exact frame timing, bounded caches, and a `parallel == sequential` equivalence guarantee enforced in CI under `-race`. |
| 🧩 **Small surface** | Explicit graph nodes instead of nested closures; named methods and option structs instead of operator overloading. |

---

## Requirements

- **Go 1.25+**
- **FFmpeg and ffprobe** on your `PATH`. MovieGo shells out to them for decode,
  encode, and probing.

```sh
ffmpeg -version    # confirm both are reachable
ffprobe -version
```

If the binaries are not on `PATH`, point MovieGo at them with environment
variables:

```sh
export MGO_FFMPEG=/opt/ffmpeg/bin/ffmpeg
export MGO_FFPROBE=/opt/ffmpeg/bin/ffprobe
```

---

## Install

```sh
go get github.com/mowshon/moviego/v2@latest
```

```go
import mgo "github.com/mowshon/moviego/v2"
```

MovieGo v2 uses Go's semantic import versioning. When upgrading from v1, update
every MovieGo import to include `/v2`, including subpackages such as
`github.com/mowshon/moviego/v2/effect`.

The `mgo` alias is the convention used throughout the docs and examples — it is
the only package most projects ever import. Subpackages (`effect`, `transition`,
`keyframe`, …) are reached only when you need a type the facade doesn't re-export.

---

## Quick start

Open a video, trim it, scale and fade it, and write the result:

```go
package main

import (
	"context"
	"log"
	"time"

	mgo "github.com/mowshon/moviego/v2"
)

func main() {
	ctx := context.Background()

	clip, err := mgo.OpenVideo("input.mp4")
	if err != nil {
		log.Fatal(err)
	}
	defer clip.Close()

	// Trim 1s–4s, halve the size, fade in over a second.
	out := clip.Subclip(mgo.Sec(1), mgo.Sec(4)).
		Resize(0.5).
		FadeIn(time.Second)

	if err := mgo.WriteVideo(ctx, out, "out.mp4", mgo.ExportOptions{
		Rate:  mgo.Rate{Num: 30, Den: 1},
		Codec: "libx264",
		CRF:   20,
	}); err != nil {
		log.Fatal(err)
	}
}
```

Editing methods are **fluent** and **lazy**: each returns a new handle and builds
a graph node; nothing is decoded until `WriteVideo`. A build error (e.g. an effect
that needs a known duration) is carried on the handle and surfaced by `WriteVideo`
or `Video.Err()`, so chains stay readable and you don't `if err != nil` after
every step.

---

## Core concepts

These five ideas explain almost everything about how the API behaves.

### 1. The clip graph is lazy

A `*Video` (or `*Audio`) is a *description*, not pixels. Effects return new clips;
composites pull their children's frames at render time. Decoders are created at
export time and owned by exactly one goroutine. Building a chain does **no I/O** —
only `WriteVideo` (and friends like `ExtractFrame`) actually run FFmpeg.

### 2. Time is rational

Frame rate is `Rate{Num, Den}` — `30000/1001` exactly, never a drifting `float64`.
Build a `Time` (a `time.Duration` alias) with `mgo.Sec(1.5)`, `mgo.ParseTime("00:01:30.5")`,
or any `time.Duration` expression like `2*time.Second`.

```go
mgo.Rate{Num: 30, Den: 1}        // 30 fps
mgo.Rate{Num: 30000, Den: 1001}  // 29.97 fps (NTSC), exactly
mgo.Sec(2.5)                     // 2.5 seconds as a Time
```

### 3. Duration is a single value

`Duration()` and `End()` return one `Time`. A clip with no fixed length yet — a
freshly generated `Color`, an unbounded `Loop` — reports `mgo.Infinite`. Test it
with `mgo.Finite(d)`; **export requires a finite duration**, so give generated
sources one with `WithDuration`.

```go
bg := mgo.Color(1920, 1080, [3]byte{0, 0, 0}) // Infinite duration...
bg = bg.WithDuration(5 * time.Second)         // ...now finite.
```

### 4. Start and end times are flexible

Anywhere you give a start and an end, **the end is optional and defaults to the
end of the media**, and negative values count back from the end:

```go
clip.Subclip(mgo.Sec(2))             // 2s → end of the video
clip.Subclip(mgo.Sec(2), mgo.Sec(5)) // 2s → 5s
clip.Subclip(0, -mgo.Sec(1))         // start → 1s before the end
music.Subclip(mgo.Sec(10))           // same rule for audio
```

### 5. Masks and audio are sidecars

Transparency is a **mask** kept separate from RGB during editing; audio rides
along as a **sidecar**. Trimming, speeding up, or reversing a clip transforms its
mask and audio too — so they never drift out of sync with the picture. Effects are
config values applied through a named method (`Resize`, `FadeIn`, `FlipH`, …) or
the generic `clip.Fx(effect.X{...})`.

---

## Cookbook

Every snippet below assumes `ctx := context.Background()` and an opened source
where relevant. Full, runnable versions live in [`examples/`](examples/).

### Trim, resize & fade

```go
out := clip.
	Subclip(mgo.Sec(1), mgo.Sec(4)). // keep 1s–4s (trims audio too)
	Resize(0.5).                     // half size; ResizeTo(1280, 720) for a target
	FadeIn(time.Second).
	FadeOut(time.Second)

mgo.WriteVideo(ctx, out, "out.mp4", mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}})
```

### Composite layers & positioning

`Composite` stacks clips on a canvas (the first child sets the canvas size). Use
`Layer(n)` for z-order — higher renders on top.

```go
bg := mgo.Color(1920, 1080, [3]byte{0, 0, 0}).WithDuration(5 * time.Second)

title, err := mgo.Text("Hello, MovieGo", mgo.TextOptions{
	FontPath: "Inter.ttf", FontSize: 72, Color: color.White,
})
if err != nil {
	log.Fatal(err)
}
title = title.WithDuration(5 * time.Second).FadeIn(time.Second)

final := mgo.Composite(
	bg,
	title.Position(mgo.Center).Layer(10),
)
mgo.WriteVideo(ctx, final, "title.mp4", mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}})
```

Placement helpers:

| Helper | Meaning |
| --- | --- |
| `mgo.Center` / `Left` / `Right` / `Top` / `Bottom` | keyword anchors |
| `mgo.At(x, y)` | absolute top-left pixel |
| `mgo.RelPos(fx, fy)` | fraction of the **free space** (`RelPos(0.5, 0.5)` centers) |

> **Note:** relative position is a fraction of *free space* (canvas − child), a
> deliberate divergence from MoviePy. `RelPos(0, 0)` is top-left, `RelPos(1, 1)`
> is bottom-right, `RelPos(0.5, 0.5)` is dead center.

Picture-in-picture is just a smaller, positioned child that starts later:

```go
inset := overlay.Resize(0.3).Position(mgo.At(40, 40)).Layer(5)
final := mgo.CompositeWith(mgo.CompositeOptions{}, background, inset)
```

### Text, titles & captions

```go
title, _ := mgo.Text("Hello, MovieGo", mgo.TextOptions{
	FontPath:    "Inter.ttf", // optional — a default font is embedded
	FontSize:    72,
	Color:       color.White,
	Stroke:      color.Black,
	StrokeWidth: 3,
})
title = title.WithDuration(5 * time.Second).FadeIn(time.Second)
```

Two layout modes mirror MoviePy: **label** (default) sizes the image to the text;
**caption** (`Caption: true`) wraps to a fixed `Size.W` and, when `FontSize` is 0,
bisects for the largest size that fits.

Titles can animate in with `AnimatedText` — pick a built-in entrance or supply a
custom curve:

```go
title, _ := mgo.AnimatedText("Hello", opts, mgo.TextAnim{
	Type: mgo.AnimTypewriter, Dur: 2 * time.Second, Easing: mgo.EaseOut,
})
title = title.WithDuration(5 * time.Second).Position(mgo.Center)
```

Entrances: `AnimFadeIn`, `AnimTypewriter`, `AnimSlideUp`/`Down`/`Left`/`Right`,
`AnimPop`, or a `Custom` function. See [text/README.md](text/README.md).

### Subtitles

```go
subs, _ := mgo.SubtitlesSRT("captions.srt", mgo.SubtitleOptions{
	Size: mgo.Size{W: 1280, H: 720}, FontPath: "Inter.ttf",
})
final := mgo.Composite(video, subs)
```

Loaders for **SRT**, **VTT**, **JSON** (flat array or rich word-timed form), and
**ASS/SSA** (common subset). `LayoutWordCenter` shows one timed word at a time for
social / vertical video. `mgo.SplitAtCues(v, cues)` is the inverse: it slices a
clip into one segment per caption.

### Effects & color grading

Apply effects with a **named method** for the common one-liners, or `.Fx(effect.X{...})`
for full control of every field:

```go
import "github.com/mowshon/moviego/v2/effect"

graded := clip.
	Brightness(0.05).
	Contrast(1.15).
	Saturation(1.2).
	Gamma(1.1).
	Fx(effect.Vignette{Amount: 0.4}).
	Fx(effect.GaussianBlur{Radius: 2})
```

Highlights from the catalog (full table in [effect/README.md](effect/README.md)):

- **Framing** — `Resize`, `ResizeTo`, `Crop`, `Rotate` (any angle, bilinear; 90°
  multiples are lossless), `Pad`, `FitTo`, `FlipH`, `FlipV`.
- **Color grading** — `Brightness`, `Contrast`, `Saturation`, `Gamma`, `Invert`,
  `ColorBalance` (shadows/mids/highlights), `HSL`, `Grayscale`. Per-channel ops
  precompute a lookup table, so the hot loop is three table reads per pixel.
- **3D LUTs** — `LUT("film.cube")` loads a Resolve/Adobe `.cube` and trilinearly
  samples it per pixel — the standard way to ship a film "look."
- **Blur / sharpen** — `GaussianBlur` (separable, `O(2r)`), `Blur` (cheap
  resample), `MotionBlur` (directional smear), `Sharpen` (unsharp mask).
- **Spatial** — `Vignette`, `WaterDrop` (ripple), `ChromaKey` (green-screen).

Green-screen keying writes a mask so the subject can be composited:

```go
keyed := clip.Fx(effect.ChromaKey{Hex: "#00FF00", Similarity: 0.3, Blend: 0.1})
out := mgo.Composite(background, keyed)
```

Many color/geometry effects advertise an FFmpeg equivalent (`eq`, `colorbalance`,
`hue`, `lut3d`, `gblur`, `scale`, `crop`, `rotate`, `hflip`/`vflip`, `fade`), so a
straight grading chain can collapse into a single FFmpeg invocation under
`EnableFusion`; the rest render on the Go oracle.

**Write your own** in one function with `effect.PixelFn` — no node type needed:

```go
tintRed := effect.PixelFn{
	Name: "tint-red",
	Fn: func(t clip.Time, dst, src *clip.Frame) error {
		copy(dst.Pix, src.Pix)
		for i := 0; i < len(dst.Pix); i += 3 {
			dst.Pix[i] = 255 // boost the red channel
		}
		return nil
	},
}
v := clip.Fx(tintRed)
```

### Time, speed & motion

A clip's timeline is editable end to end. Forward operations keep the clip on the
fast parallel pipeline; operations that read the source backward run on the
sequential engine (check which with `mgo.Describe`).

```go
clip.Speed(2.0)   // 2× faster (audio time-scaled to match)
clip.Speed(0.5)   // slow-mo
clip.Loop(3)      // repeat 3×
clip.Reverse()    // play backward, audio included
clip.Boomerang()  // forward then backward, seamless loop of 2× the duration
```

**Variable speed / ramps** — map output time onto source time through a piecewise,
eased curve:

```go
ramp := clip.TimeRemap([]mgo.TimePoint{
	{Out: 0, In: 0},
	{Out: mgo.Sec(2), In: mgo.Sec(1)}, // first 2s out = first 1s source (2× slow-mo)
	{Out: mgo.Sec(3), In: mgo.Sec(5)}, // next 1s out = source 1s→5s (4× fast-forward)
}, mgo.EaseInOut)
```

**Freeze frames** hold a single frame and stay on the fast pipeline:

```go
clip.Freeze(mgo.Sec(4), mgo.Sec(2)) // play to 4s, hold 2s, continue
clip.FreezeStart(time.Second)        // hold frame 0 first
clip.FreezeEnd(time.Second)          // hold the last frame
```

**Keyframes** interpolate a property over clip-local time:

```go
hero := photo.WithDuration(6 * time.Second).
	Animate(mgo.PropScale, []mgo.Keyframe{ // a "Ken Burns" push
		{At: 0, Val: 1.0},
		{At: 6 * time.Second, Val: 1.25},
	}, mgo.EaseSmooth)
```

`PropOpacity` and `AnimatePosition` only take visible effect inside a `Composite`.
The deep dive is in [keyframe/README.md](keyframe/README.md).

### Transitions

Stitch clips together with the fluent `Sequence` builder for mixed transitions:

```go
v := mgo.NewSequence().
	Add(intro).
	Then(mgo.Crossfade(time.Second)).
	Add(body).
	Then(mgo.WipeLeft(500 * time.Millisecond)).
	Add(outro).
	Then(mgo.FadeThroughBlack(time.Second)).
	Add(credits).
	Video()
```

Or one uniform transition between every clip:

```go
import "github.com/mowshon/moviego/v2/transition"

v := mgo.ConcatWith(mgo.ConcatOptions{
	Transition: transition.CrossFade{},
	Duration:   500 * time.Millisecond,
}, a, b, c)
```

Built-ins: `Crossfade`, `Dissolve`, `FadeThroughBlack`/`FadeThroughColor`,
`WipeLeft`/`Right`/`Up`/`Down`, `SlideLeft`/…, `PushLeft`/…, `IrisOpen`/`IrisClose`.
Each also blends the alpha sidecar, and any transition can be wrapped with
`transition.Eased(...)` or `transition.Reversed(...)`. Writing a custom transition
is a single `Frame(p, dst, a, b)` method — see [transition/README.md](transition/README.md).

For a plain join with no transition:

```go
intro := a.Subclip(0, mgo.Sec(2)).FadeOut(300 * time.Millisecond)
outro := b.Subclip(mgo.Sec(3)).FadeIn(300 * time.Millisecond)
reel := mgo.Concat(intro, outro) // chains same-size clips; composes mixed sizes
```

### Vector drawing & motion graphics

Draw lower thirds, callouts, and progress bars with no image asset:

```go
import "image/color"

bar := mgo.NewCanvas(1920, 1080).
	Rect(80, 880, 760, 120, mgo.Paint{Fill: color.RGBA{A: 180}}).
	Circle(140, 940, 36, mgo.Paint{Fill: color.White}).
	Line(80, 880, 840, 880, color.White, 4).
	WithDuration(4 * time.Second)
```

Review and broadcast overlays:

```go
v.Watermark(logo, mgo.Corner(mgo.BottomRight), mgo.WithMargin(20)) // pin a logo
v.BurnTimecode(mgo.TimecodeOptions{Format: mgo.TCFrames})           // HH:MM:SS:FF
mgo.Credits(lines, mgo.CreditsOptions{Speed: 120})                  // scrolling roll
```

The drawing layer is a thin assembler over `golang.org/x/image/vector` (the same
rasterizer used for glyphs), so it adds no dependency. See
[draw/README.md](draw/README.md) for the shape model and how to add a primitive.

### Audio

Audio is a sidecar that follows its video through trims, speed changes, and
reverses. You can also edit it directly and mix beds:

```go
clip := vid.Subclip(0, mgo.Sec(8)) // trims video AND its audio sidecar

bed := music.Subclip(0, mgo.Sec(8)).
	Volume(0.25).
	FadeIn(2 * time.Second).
	FadeOut(2 * time.Second)

final := clip.WithAudio(mgo.Mix(clip.AudioClip(), bed))
mgo.WriteVideo(ctx, final, "mixed.mp4", mgo.ExportOptions{})
```

`mgo.Mix` sums any number of audio clips, resampling and up-mixing as needed.
Other handy bridges: `WithoutAudio()`, `AudioFadeIn`/`AudioFadeOut`, and per-clip
`Volume`.

### Transparent export

A clip with a mask (from `ChromaKey`, a transparent `Canvas`, `PropOpacity`, or a
PNG with alpha) exports with alpha automatically — **alpha is never silently
dropped**: an output container that can't carry it fails early with a clear error.

```go
// Portable, lossless: .mov (qtrle) or .mkv (ffv1) — chosen automatically.
mgo.WriteVideo(ctx, overlay, "overlay.mov", mgo.ExportOptions{})

// Web-friendly transparent .mp4 (HEVC+alpha, macOS VideoToolbox, opt-in):
mgo.WriteVideo(ctx, overlay, "overlay.mp4", mgo.ExportOptions{Codec: mgo.CodecHEVCAlpha})
```

### FFmpeg-only fast paths

When the whole graph is a linear filtergraph, skip Go pixels entirely:

```go
// Fuse a linear trim/scale/crop/fade graph into one FFmpeg invocation:
mgo.WriteVideo(ctx, clip, "out.mp4", mgo.ExportOptions{EnableFusion: true})

// Stream-copy trim — no re-encode, keyframe-accurate:
mgo.StreamCopyTrim(ctx, "in.mp4", "cut.mp4", mgo.Sec(5), mgo.Sec(10))

// Concat compatible files via the demuxer — no re-encode:
mgo.ConcatFiles(ctx, "joined.mp4", "a.mp4", "b.mp4")
```

`EnableFusion` is opt-in and conservative: a graph it can't express falls back
cleanly to a Go engine, with the reason reported via `ExportOptions.Debugf`.

### Probing & extracting frames

```go
info, _ := mgo.Probe("input.mp4")
fmt.Println(info.Size, info.Duration, info.Rate)

// Pull a single still as PNG:
mgo.ExtractFrame(ctx, clip, mgo.Sec(2), "thumb.png")

// Pre-flight the render plan without rendering anything:
report, _ := mgo.Describe(clip, mgo.ExportOptions{})
fmt.Println(report.Engine, report.Frames, report.FusionReason)
```

---

## Export options

`mgo.WriteVideo(ctx, clip, path, mgo.ExportOptions{…})`:

| Field | Purpose |
| --- | --- |
| `Rate` | Output frame rate. Zero uses the clip's own rate. |
| `Codec` | Video codec (default `libx264`; `mgo.CodecHEVCAlpha` for transparent `.mp4`). |
| `Preset` | Encoder preset (default `medium`; ignored by VideoToolbox). |
| `CRF` | Constant-quality target (lower = better; e.g. 18–28 for x264). |
| `Bitrate` | Target video bitrate (e.g. `"4M"`). |
| `PixFmt` | Output pixel format (default `yuv420p`). |
| `Threads` | FFmpeg encoder threads (0 = FFmpeg default). |
| `ExtraOutputArgs` | Advanced output-side FFmpeg args appended before the output path, for muxer/container flags such as HLS (`[]string{"-f", "hls", "-hls_time", "3"}`) or MP4 flags (`[]string{"-movflags", "+faststart"}`). Not for input, global, or filtergraph args. |
| `Workers` | Render worker-pool size (0 = auto from `GOMAXPROCS`). |
| `Progress` | Per-frame progress callbacks (`mgo.BarProgress` draws a bar). |
| `DisableAudio`, `AudioCodec`, `AudioBitrate` | Audio mux control. |
| `EnableFusion` | Opt into the FFmpeg-only fast path. |
| `HWAccel` | Hardware encoder family (`nvenc`, `qsv`, `videotoolbox`). |
| `Debugf` | Receives the planner's engine / fusion decision. |

Cancelling `ctx` stops the export and kills FFmpeg.

---

## How rendering works

The planner classifies the graph and picks an engine:

- **static / linear-streamable / bounded-cache** → the **3-stage pipeline**: one
  sequential decoder per source feeds a worker pool that does the per-frame pixel
  work, then a single goroutine reorders by index and feeds the encoder.
- **random-access** (reverse, loop, a source used twice, a transition timeline) →
  the **sequential engine**, which is also the correctness oracle: CI asserts
  `parallel == sequential` byte-for-byte under `-race`.
- **FFmpeg-only** (opt-in, when the whole graph is a filtergraph) → **one FFmpeg
  invocation**, no Go pixels.

A graph that can't fuse falls back cleanly to a Go engine, with the reason
available via `ExportOptions.Debugf`. Use `mgo.Describe(clip, opts)` to see the
chosen engine, frame count, and memory budget before rendering.

---

## Best practices

- **Build the chain, check the error once.** Methods defer their first error onto
  the handle. Let `WriteVideo` surface it, or call `v.Err()` / `mgo.Validate(v)`
  before a long render — you don't need to check after every step.
- **Always `defer clip.Close()`** on anything you `OpenVideo`/`OpenAudio`. It
  releases the underlying decoder; placement copies share the source safely.
- **Give generated sources a duration.** `Color`, `Canvas`, and `Text` report
  `Infinite` until you call `WithDuration` — export needs a finite length.
- **Set an explicit `Rate` for generated graphs.** Images, colors, and time
  remaps have no inherent fps; pass `ExportOptions{Rate: …}`.
- **Prefer `Speed`/`Freeze`/forward `TimeRemap`** when you can — they stay on the
  parallel pipeline. `Reverse` and rewinding remaps are correct but sequential.
- **Reach for the fast paths first** for cut-only or concat-only jobs:
  `StreamCopyTrim` and `ConcatFiles` avoid re-encoding entirely; `EnableFusion`
  collapses linear grade/transform chains into one FFmpeg call.
- **Pre-flight with `Describe`.** It reports the engine and frame count without
  rendering — the quickest way to confirm a graph stayed fast.
- **Pick the right transparent container.** `.mov`/`.mkv` are portable and
  lossless; `CodecHEVCAlpha` gives a web-friendly `.mp4` but needs macOS
  VideoToolbox.
- **Wire `ctx` through and honor cancellation** — it kills FFmpeg and unblocks
  every render stage, which matters for servers and request-scoped work.

---

## Package map

Most code only imports the root `mgo` facade. The subpackages own the execution
model and each carries focused docs:

| Package | What it is | Docs |
| --- | --- | --- |
| `mgo` (root) | Fluent `Video`/`Audio` handles, constructors, export | [pkg.go.dev](https://pkg.go.dev/github.com/mowshon/moviego/v2) |
| `effect` | Effect catalog + `PixelFn` extension hook | [effect/README.md](effect/README.md) |
| `transition` | Transition system + custom transitions | [transition/README.md](transition/README.md) |
| `keyframe` | Property animation & time-remapping engine | [keyframe/README.md](keyframe/README.md) |
| `draw` | Vector shapes rasterized into clips | [draw/README.md](draw/README.md) |
| `text` | Titles, captions, subtitles, credits, timecode | [text/README.md](text/README.md) |
| `clip` | Foundation value types (`Time`, `Rate`, `Frame`, …) | — |

A full architectural reference — every package, the file tree, and the invariants
you must respect when changing the code — lives in
[`docs/technical-spec.md`](docs/technical-spec.md).

---

## Runnable examples

The [`examples/`](examples/) directory has complete, buildable programs. They
download small public sample media into `examples/input/` on first run and write
results to `examples/output/`.

| Example | Shows |
| --- | --- |
| `trim` | open → trim → resize → fade → write |
| `composite` | layered composite with a picture-in-picture inset |
| `text` | titles and captions over video |
| `motion` | vector drawing, animated text, watermark, timecode, scrolling credits |
| `slideshow` | image sequence to video |
| `subtitles`, `subtitles-json`, `word-captions` | SRT / VTT / JSON captions |
| `concat` | joining clips |
| `transitions` | built-in, eased, and custom transitions |
| `keyframes` | keyframe animation, speed ramps, freeze frames |
| `reverse` | backward playback and the boomerang loop |
| `colorgrade` | color grading, a 3D `.cube` LUT, vignette, blur |
| `audiomix` | music bed under original audio |
| `transparent` | alpha export |
| `pipeline` | the parallel render pipeline |
| `reframe` | probe → describe → reframe → extract still → export |

```sh
go run ./examples/trim
```

---

## Contract documentation

The docs in [`docs/`](docs/) freeze the behaviors that matter — read them before
relying on subtle semantics or changing the engine:

- [`timing.md`](docs/timing.md) — rational rate, frame counting, subclip & duration rules
- [`decoder-semantics.md`](docs/decoder-semantics.md) — seek / skip / restart behavior
- [`sidecar-propagation.md`](docs/sidecar-propagation.md) — how effects touch mask / audio
- [`render-capabilities.md`](docs/render-capabilities.md) — graph classification → engine
- [`ffmpeg-policy.md`](docs/ffmpeg-policy.md) — codecs, pixfmt / alpha, fps source, escaping
- [`chroma-key.md`](docs/chroma-key.md) — the chroma-key pipeline and parameters
- [`compatibility.md`](docs/compatibility.md) — every deliberate divergence from MoviePy

---

## Development

```sh
go build ./...
go vet ./...
gofumpt -l .            # formatting must be clean
go test -race ./...     # the keystone: parallel == sequential, no races, no leaks
go test -bench=. ./composite/ ./audio/ ./effect/
```

The test suite uses synthetic bitmap fixtures with no FFmpeg dependency where
possible; tests that need FFmpeg skip cleanly when it is not installed.

---

## License

MIT — see [LICENSE](LICENSE).
