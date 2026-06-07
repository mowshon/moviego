# text — titles, captions, motion text & timecode

The `text` package rasterizes text into clips: static titles and captions,
subtitles (SRT/VTT/JSON/ASS), animated titles, scrolling credits, and burned-in
timecode. Everything renders with `golang.org/x/image/font` into a transparent
frame, so the result composites straight over video.

All of the entry points below are re-exported on the facade (`mgo.…`); reach for
the package directly only when you need a node type.

## Static text

```go
title, _ := mgo.Text("Hello, MovieGo", mgo.TextOptions{
    FontPath: "Inter.ttf", FontSize: 72, Color: color.White,
    Stroke: color.Black, StrokeWidth: 3,
})
title = title.WithDuration(5 * time.Second).FadeIn(time.Second)
```

Two layout modes mirror MoviePy: **label** (default) sizes the image to the text
at the given `FontSize`; **caption** (`Caption: true`) wraps to a fixed `Size.W`
and, when `FontSize` is 0, bisects for the largest size that fits.

## Animated text

`AnimatedText` renders a title that animates in over `anim.Dur`, then holds. It
is a transparent clip — composite it over footage and place it with `Position`.

```go
title, _ := mgo.AnimatedText("Hello", opts, mgo.TextAnim{
    Type:   mgo.AnimTypewriter,
    Dur:    2 * time.Second,
    Easing: mgo.EaseOut,
})
title = title.WithDuration(5 * time.Second).Position(mgo.Center)
```

Built-in entrances (`AnimKind`):

| Kind | Effect |
| --- | --- |
| `AnimNone` | shown fully from frame 0 |
| `AnimFadeIn` | opacity 0 → 1 |
| `AnimTypewriter` | reveals one character at a time, left to right |
| `AnimSlideUp` / `Down` / `Left` / `Right` | slides into place from that edge |
| `AnimPop` | scales up from nothing while fading in |

`TextAnim.Distance` overrides a slide's travel (default: the raster's own
width/height). The clip's default duration is `anim.Dur`; extend it with
`WithDuration` to hold the title after it arrives.

### Custom animations

For anything outside the built-ins, set `TextAnim.Custom`: a function from eased
progress (`0..1`) to an `AnimState`. This is the extension point — no new node
type required.

```go
type AnimState struct {
    Reveal float64 // fraction of characters shown (typewriter); 1 shows all
    Alpha  float64 // opacity 0..1
    Dx, Dy float64 // offset from the resting position, in pixels
    Scale  float64 // 1 is natural size, about the center
}

// A bounce-in: overshoot the scale, then settle.
anim := mgo.TextAnim{Dur: time.Second, Custom: func(p float64) mgo.AnimState {
    return mgo.AnimState{Reveal: 1, Alpha: p, Scale: 1 + 0.2*math.Sin(p*math.Pi)}
}}
```

A state with `Reveal < 1` takes the typewriter path (left-anchored prefix);
otherwise the full raster is scaled and offset about the center.

## Scrolling credits

```go
roll, _ := mgo.Credits(
    []string{"Directed by", "A. Director", "", "Music by", "B. Composer"},
    mgo.CreditsOptions{Size: mgo.Size{W: 1920, H: 1080}, FontSize: 48, Speed: 120},
)
```

The lines are stacked into one block that scrolls up through the `Size` viewport
at `Speed` pixels per second. The clip's duration is the time for the block to
travel from just below the frame to fully above it. Use an empty string for a
blank spacer line.

## Timecode burn-in

`BurnTimecode` overlays a running readout counting up from `Start`, for dailies
and review:

```go
out := v.BurnTimecode(mgo.TimecodeOptions{
    Format:   mgo.TCFrames, // HH:MM:SS:FF (uses the video's rate)
    Position: mgo.At(40, 40),
})
```

Formats: `TCClock` (`HH:MM:SS`), `TCMillis` (`HH:MM:SS.mmm`), `TCFrames`
(`HH:MM:SS:FF`; falls back to millis if the clip has no rate). A zero `Position`
defaults to the bottom-right corner inset by `Margin`. The readout canvas is
fixed to the widest the format can render, so the overlay never jitters as the
digits change width.

## Subtitles

```go
subs, _ := mgo.SubtitlesSRT("captions.srt", mgo.SubtitleOptions{
    Size: mgo.Size{W: 1280, H: 720}, FontPath: "Inter.ttf",
})
final := mgo.Composite(video, subs)
```

Loaders: `SubtitlesSRT`, `SubtitlesVTT`, `SubtitlesJSON` (array or rich
word-timed form), and `SubtitlesASS`. ASS/SSA support covers the common subset —
the `[Events]` Dialogue lines mapped through the `Format` declaration, with
override tags (`{\…}`) stripped and `\N` turned into newlines; styling and
positioning tags are ignored. `LayoutWordCenter` shows one timed word at a time
for social/vertical video.

## Split at cues

`SplitAtCues(v, cues)` is the inverse of burning captions in: it slices a clip
into one segment per cue interval, sharing the source (close the original when
done).

```go
cues, _ := text.ParseSRTFile("captions.srt")
parts := mgo.SplitAtCues(video, cues) // []*mgo.Video, one per caption
```

## Rendering model

The animated-text, credits, timecode, and subtitle nodes are all transparent,
canvas-sized `video.VideoClip`s reporting `AccessStatic` and `ParallelSafe`: each
output frame is an independent function of time over immutable config and a
mutex-guarded LRU of rendered rasters, so they fan out cleanly across the
parallel render pipeline.
