# ffmpeg — subprocess boundary

Package `ffmpeg` is the single point of contact between MovieGo and the FFmpeg
toolchain. It owns binary discovery, process lifecycle, metadata extraction
(`ffprobe`), filtergraph assembly, and fast-path stream-copy operations. It
depends only on `package clip` for value types; all codec/render logic lives in
the layers above.

---

## Binary discovery

```go
path, err := ffmpeg.FFmpegPath()   // resolves once, cached for the process lifetime
path, err := ffmpeg.FFprobePath()
line, err := ffmpeg.Version(path)  // first line of "<bin> -version"
```

Override the binaries via environment variables:

| Variable | Default |
| --- | --- |
| `MGO_FFMPEG` | `ffmpeg` on `PATH` |
| `MGO_FFPROBE` | `ffprobe` on `PATH` |

> **Note:** the exported constants `EnvFFmpeg`/`EnvFFprobe` hold the exact
> variable names; use them in tests instead of hardcoding the strings.

---

## Process management

### Starting a subprocess

```go
proc, err := ffmpeg.Start(ctx, ffmpeg.Spec{
    Path:   path,         // from FFmpegPath()
    Args:   []string{…},  // everything after the binary name
    Stdin:  true,         // set to get proc.Stdin (io.WriteCloser)
    Stdout: true,         // set to get proc.Stdout (io.ReadCloser)
})
```

`Start` configures the child in its own **process group** (Unix) so that
cancelling the context SIGKILLs the entire group — including helper processes
spawned by FFmpeg itself. On non-Unix platforms only the direct child is killed
(a known gap; see the package doc comment).

### Waiting and killing

```go
err := proc.Wait()   // blocks; returns *ExitError on non-zero exit
err  = proc.Kill()   // immediate SIGKILL of the process group; safe to call after exit
tail := proc.Stderr() // last 64 KiB of stderr, always available
```

`Wait` must be called exactly once after `Start`. Drain `proc.Stdout` fully
before calling `Wait` to avoid a deadlock (stdout is a synchronous pipe; the
process blocks if the pipe fills).

### ExitError

```go
var exit *ffmpeg.ExitError
if errors.As(err, &exit) {
    fmt.Println(exit.Stderr) // full tail
    fmt.Println(exit.Err)    // underlying *exec.ExitError
}
```

`ExitError.Error()` returns the underlying exit message plus the **last line**
of stderr for quick triage in log output.

---

## Metadata probing

```go
info, err := ffmpeg.Probe(ctx, "/path/to/file.mp4", ffmpeg.ProbeOptions{})
```

`Probe` runs `ffprobe -print_format json -show_streams -show_format` and
returns `*MediaInfo`. A corrupt or unreadable file surfaces as
`clip.ErrVideoCorrupted` (test with `errors.Is`). A cancelled context surfaces
as the context error, not a spurious corruption error.

### MediaInfo fields

```go
info.Video     *VideoStream  // nil for audio-only files
info.Audio     *AudioStream  // nil for video-only files
info.Duration  clip.Time     // from the format container; falls back to stream duration
info.Format    string        // e.g. "mov,mp4,m4a,3gp,3g2,mj2"
info.VariableFPS bool        // avg_frame_rate diverges meaningfully from r_frame_rate
info.FPSSource  FPSSource    // which field produced Video.Rate
```

### VideoStream

```go
v := info.Video
v.Size     clip.Size  // display size, after applying Rotation (swaps W/H for 90/270)
v.RawSize  clip.Size  // stored size, before rotation
v.Rate     clip.Rate  // rational frame rate, NTSC-snapped when applicable
v.Rotation int        // normalized: 0, 90, 180, or 270
v.PixFmt   string     // e.g. "yuv420p"
v.NBFrames int64      // 0 when ffprobe does not report it
v.Codec    string     // e.g. "h264"
```

Rotation is read from `side_data_list[type=Display Matrix]` first, then from
the legacy `tags.rotate` fallback, and normalized to the four cardinal steps.

### FPS source selection

```go
ffmpeg.ProbeOptions{FPSSource: ffmpeg.FPSDefault}  // r_frame_rate (default)
ffmpeg.ProbeOptions{FPSSource: ffmpeg.FPSAvg}       // avg_frame_rate
ffmpeg.ProbeOptions{FPSSource: ffmpeg.FPSTbr}       // tbr (resolves to r_frame_rate;
                                                    //  ffprobe JSON has no tbr field)
```

---

## Filtergraph assembly

The filtergraph layer assembles FFmpeg `-filter_complex` strings for the fusion
planner without launching any process. Every function is pure string assembly
and is unit-testable in isolation.

### Building a FilterGraph

```go
g := &ffmpeg.FilterGraph{
    Inputs: []ffmpeg.Input{
        {Name: "input.mp4"},
        {Args: []string{"-f", "lavfi"}, Name: "color=c=black:s=1920x1080"},
    },
    Lines:    []string{ /* filter lines from the builders below */ },
    OutVideo: "vout",
}

g.FilterComplex() // → single -filter_complex argument (lines joined with ";")
g.InputArgs()     // → ["-i", "input.mp4", "-f", "lavfi", "-i", "color=c=..."]
```

`InputArgs` calls `SafeInputPath` on every name so filenames beginning with
`-` are never misread as flags.

### Filter line builders

All builders return a complete filtergraph line `[in]filter[out]`.

| Function | FFmpeg filter | Notes |
| --- | --- | --- |
| `Scale(in, out, w, h)` | `scale` | Bicubic (diverges from Go's CatmullRom) |
| `Crop(in, out, w, h, x, y)` | `crop` | Exact pixel copy |
| `Transpose(in, out, quarter)` | `transpose` / `null` | 0=pass-through, 1=90°CW, 2=180°, 3=90°CCW |
| `RotateAngle(in, out, angle, ow, oh, col)` | `rotate` | Clockwise radians; bilinear |
| `HFlip(in, out)` | `hflip` | Exact pixel copy |
| `VFlip(in, out)` | `vflip` | Exact pixel copy |
| `Trim(in, out, start, dur)` | `trim+setpts` | Forward subclip equivalent |
| `SetPTS(in, out, factor)` | `setpts` | Speed change (factor>1 = faster) |
| `Fade(in, out, kind, st, d, col)` | `fade` | `FadeInKind` / `FadeOutKind` |
| `FPS(in, out, rate)` | `fps` | Rate normalization (appended last by planner) |
| `Concat(in, out)` | `concat` | joins all input pads, video-only |
| `Overlay(base, ovl, out, x, y)` | `overlay` | Composite second pad onto first |
| `EqColor(in, out, brightness, contrast, sat, gamma)` | `eq` | Identity terms omitted |
| `ColorBalance(in, out, shadows, mids, highlights)` | `colorbalance` | Per-channel [r,g,b] |
| `Hue(in, out, degrees, sat)` | `hue` | |
| `Lut3D(in, out, path)` | `lut3d` | Path is filtergraph-escaped |
| `GBlur(in, out, sigma)` | `gblur` | |
| `Volume(in, out, factor)` | `volume` | Audio |
| `AFade(in, out, kind, st, d)` | `afade` | Audio fade |

### Constraints

`FilterFragment.Constraints` carries requirements the planner uses to reject
unfusable graphs:

| Constant | Meaning |
| --- | --- |
| `ConstraintAlpha` | Requires an alpha-carrying pixel format end-to-end |
| `ConstraintEvenDims` | Requires even output dimensions (chroma-subsampled output) |

---

## Fast paths (stream copy)

These operations skip Go pixel processing entirely and shell out directly to
FFmpeg. They are **opt-in** utilities; the main export pipeline does not use
them automatically.

### StreamCopyTrim

```go
err := ffmpeg.StreamCopyTrim(ctx, "in.mp4", "out.mp4", start, dur)
// dur <= 0 means "to the end"
```

Cuts `[start, start+dur)` without re-encoding. The cut snaps to the nearest
preceding keyframe, so the real start may be slightly earlier than requested —
that imprecision is the whole reason this is opt-in. Only audio and video
streams are copied (`-map 0:v? -map 0:a?`); data, subtitle, and attachment
streams are dropped.

To inspect the generated arguments without running FFmpeg:

```go
args := ffmpeg.StreamCopyTrimArgs("in.mp4", "out.mp4", start, dur)
```

### ConcatDemux

```go
err := ffmpeg.ConcatDemux(ctx, []string{"a.mp4", "b.mp4", "c.mp4"}, "out.mp4")
```

Joins files end-to-end without re-encoding using the concat demuxer. Inputs
**must** share a codec, pixel format, and stream parameters; if they don't,
FFmpeg will return an error and the caller should fall back to re-encoding via
the concat filter. Input paths are resolved to absolute paths before being
written to the temporary list file (relative paths in the list are resolved
relative to the list file's directory, not the process CWD).

Helper to inspect the list file content:

```go
content := ffmpeg.ConcatListContent([]string{"a.mp4", "b.mp4"})
// → "file 'a.mp4'\nfile 'b.mp4'\n"
```

---

## Escaping

### File paths

```go
safe := ffmpeg.SafeInputPath(path)
```

Prepends `./` when the path starts with `-` so FFmpeg does not treat it as a
flag. Applied automatically by `InputArgs()`, `StreamCopyTrimArgs`, and
`ConcatDemuxArgs`.

### Filtergraph option values

```go
escaped := ffmpeg.escapeFilterValue(path) // package-internal
```

Backslash-escapes the characters special to a filtergraph option value
(`\ ' : [ ] , ;`). Used by `Lut3D` for the file path. Shell-level escaping is
not needed because arguments are passed directly via `exec.Command`.

---

## Encoder capability quirks

```go
ffmpeg.EncoderUsesPreset("libx264")           // true
ffmpeg.EncoderUsesPreset("h264_videotoolbox") // false
```

VideoToolbox encoders reject the `-preset` flag that libx264/libx265 and
nvenc/qsv encoders accept. Consult this before emitting `-preset` in a command.

---

## Error handling

| Error | Meaning |
| --- | --- |
| `*ExitError` | FFmpeg/ffprobe exited non-zero; `.Stderr` has the captured tail |
| `clip.ErrVideoCorrupted` | Probe returned no usable streams or ffprobe failed |
| `clip.ErrClosed` | Operation on an already-closed resource |

All errors wrap via `clip.Wrap(op, err)` so `errors.Is` / `errors.As` work
through the chain.

---

## Platform notes

| Platform | Process kill scope |
| --- | --- |
| Unix | Entire process group (`-SIGKILL` to `-pid`); helper processes spawned by FFmpeg are killed too |
| Windows / other | Direct child only (`Process.Kill`); helper processes may survive cancellation |

A Windows job-object equivalent is a known gap (see the `proc_other.go`
comment).
