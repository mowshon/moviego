# FFmpeg policy

How MovieGo invokes the FFmpeg toolchain. Known gaps are noted so the policy is
in one place; the rest is implemented today.

## Binary discovery

`ffmpeg`/`ffprobe` are resolved from `MGO_FFMPEG`/`MGO_FFPROBE` (each checked
with `os.Stat`) then `PATH`, cached via `sync.Once`. `Version` returns the first
line of `<bin> -version`. Tests skip cleanly when neither resolves.

## Process lifecycle

Every process runs under `exec.CommandContext` with stderr drained into a bounded
64 KiB ring. On Unix the child is put in its own process group and cancellation
SIGKILLs the whole group, so transcodes that spawn helpers die whole; a
`WaitDelay` backstops a wedged child. The no-orphan guarantee is Unix-only for
now (a Windows job object is a known gap). A non-zero exit surfaces as
`*ExitError` carrying the stderr tail.

## Probing

`ffprobe -v error -print_format json -show_streams -show_format <path>`.

- **fps source.** `ProbeOptions.FPSSource` selects `r_frame_rate`
  (default), `tbr` (resolves to `r_frame_rate`; ffprobe has no distinct field),
  or `avg_frame_rate`. The chosen source is recorded on `MediaInfo`.
- **VFR detection.** When `avg_frame_rate` diverges from `r_frame_rate`
  by more than 1%, `MediaInfo.VariableFPS` is set. Export still uses the explicit
  output rate; frame-accuracy limits for VFR inputs are documented, not hidden.
- **Rotation.** Preferred from a `Display Matrix` side-data entry, else the legacy
  `tags.rotate`, normalized to 0/90/180/270; 90/270 swap W/H for the display size.

## Decode

Two-pass `-ss` (coarse before `-i`, precise after, `offset = min(1s, start)`),
then `-an -f rawvideo -pix_fmt rgb24 -`. See
[decoder-semantics.md](decoder-semantics.md) for the seek state table and the
skip-vs-restart thresholds. Input pixel format is `rgb24` today; `rgba` for
masked sources remains a known gap.

## Encode

`-y -f rawvideo -s WxH -pix_fmt rgb24 -r Num/Den -i - -an -vcodec {codec}
-preset {preset} -pix_fmt {pixfmt} {out}`. Defaults: `libx264` / `medium` /
`yuv420p`.

- **Even-dimension guard.** A chroma-subsampled output pixel format (the 4:2:0
  family) requires even width and height; an odd size fails fast with the typed
  `ErrOddSize` before FFmpeg is launched, rather than as a stderr-tail error.
- **Output rate** must be a positive rational (`Num>0`, `Den>0`); a zero rate is
  rejected before any file is created.
- **Extra output args.** `ExportOptions.ExtraOutputArgs` is appended immediately
  before the output path on both Go-rendered and FFmpeg-only exports. It is the
  escape hatch for advanced output-side FFmpeg flags not modeled as typed
  options: muxer/container flags such as `-f hls`, `-hls_time`,
  `-hls_segment_filename`, `-movflags +faststart`, and similar output options.
  It is not for input options, global options, or filtergraph construction.
- **Alpha output** *(known gap)*: `yuva420p` only when both dims are even
  (libx264/nvenc); libvpx/webm adds `-auto-alt-ref 0`.

## Filtergraph fusion (FFmpeg-only export)

Fusion is **opt-in** (`ExportOptions.EnableFusion`, off by default). When enabled
and a graph is fully FFmpeg-expressible the planner skips the Go decode/encode
pipeline and runs **one** invocation built from a labeled `-filter_complex`:

```text
ffmpeg -nostdin -loglevel error -y -i {src} [-i {temp-audio}] \
  -filter_complex "[0:v]trim=…,setpts=PTS-STARTPTS[v1];[v1]scale=…[v2];[v2]fps=Num/Den[vout]" \
  -map "[vout]" [-map 1:a:0] -frames:v {N} \
  -vcodec {codec} -preset {preset} -pix_fmt {pixfmt} [-c:a copy -shortest] {out}
```

- **Decision, not advertisement.** Nodes only advertise a structured
  `ffmpeg.FilterFragment` (via the optional `video.Filterable`/`FilterSource`
  interfaces); `render/fuse.go` decides whether the *whole* graph fuses and
  `render/plan.go` records the engine and reason. The builders in
  `ffmpeg/filtergraph.go` are pure and unit-tested without launching FFmpeg.
- **Scope (v1).** One file source through forward trim / positive speed / scale
  / crop / fade / color grading / arbitrary-angle rotation. Transparent output,
  multi-layer composites/concat, image/color sources, quarter-turn rotation (the
  lossless Go transpose, not yet filter-gated), and non-expressible effects fall
  back to the Go engine. See
  [compatibility.md](compatibility.md#filtergraph-fusion-ffmpeg-only-export).
- **Frame count.** The fused output is capped with `-frames:v` to the planner's
  scheduled (floor) frame count, and an `fps` filter normalizes to the export
  rate, so the fused and Go paths produce the same number of frames and both
  stop at source EOF.
- **Even-dimension guard** still applies before launch (`ErrOddSize` for an odd
  size with a 4:2:0 output), matching the Go encoder.

## Stream copy & concat fast paths

These avoid Go pixels *and* a filtergraph; they are explicit utilities
(`ffmpeg.StreamCopyTrim`, `ffmpeg.ConcatDemux`; facade `mpy.StreamCopyTrim`,
`mpy.ConcatFiles`), never automatic export rerouting, because their accuracy
constraints would surprise a caller.

- **Stream-copy trim.** `-ss {start}` before `-i` (fast keyframe seek), `-t`
  after, `-map 0:v? -map 0:a? -c copy -avoid_negative_ts make_zero`. Only the
  video and audio streams are copied (optionally, so a single-stream source still
  works); `-map 0` would also copy data/subtitle/attachment streams that some
  containers reject. The cut snaps to the keyframe at or before `start`, so it is
  **not** frame-accurate — re-encode via the editing graph when accuracy matters.
- **Concat demuxer.** `-f concat -safe 0 -i {list} -c copy`, with a temp list
  file (`file '<path>'`, single quotes escaped). It requires codec/parameter
  compatible inputs. `ffmpeg.ConcatDemux` fails on incompatible inputs; the
  `mpy.ConcatFiles` facade catches that and falls back to a **graph re-encode**
  (`Concat(...)` + `WriteVideo` through the Go render path) — a full re-encode,
  not an FFmpeg concat-filter pass.

## Hardware encoding

`ExportOptions.HWAccel` (`nvenc`/`cuda`, `qsv`, `videotoolbox`/`vt`) selects the
matching H.264 hardware encoder when no explicit `Codec` is set, on both the
fused and Go paths (opaque output only). It is opt-in; an unconfigured encoder
surfaces as a normal FFmpeg error at launch rather than being assumed available.
Encoder-appropriate flags only: `-preset` is emitted for libx264/libx265 and the
nvenc/qsv encoders but **omitted for VideoToolbox**, which rejects it
(`ffmpeg.EncoderUsesPreset`, shared by both encode paths).

## Filenames & typed errors

A path beginning with `-` is prefixed with `./` (`ffmpeg.SafeInputPath`) so it is
never parsed as a flag. A corrupt or truncated input surfaces as a typed
`ErrVideoCorrupted` wrapping the stderr tail; a partial raw frame on decode is
treated as corruption, not a clean EOF.

## Audio decode

`[-ss …] -i <path> -vn -f s16le -acodec pcm_s16le -ar {rate} -ac {channels} -`.
Decoded s16 little-endian PCM is normalized to float32 by dividing by
`2^(8*nbytes-1)`. The reader streams forward; it skips for a nearby forward seek
and restarts only on a backward seek or a jump beyond **1,000,000** sample
frames (MoviePy's audio threshold, far larger than the 100-frame video one).
Reads before sample 0 or past the end zero-pad (silence), so a caller that knows
the timeline never special-cases EOF. `DecoderOptions.SampleRate`/`Channels` are
the decode target FFmpeg resamples/remixes to; the seek timestamp uses the
overflow-safe `clip.SampleTime`.

A file source is currently opened at its **native** sample rate, so when a mix
combines sources of differing rates the rate conversion happens in Go, in the
mixer, via linear interpolation between adjacent source frames (a documented
divergence — see `compatibility.md`). Two consequences follow and are tracked,
not yet optimized: linear interpolation is lower quality than FFmpeg's resampler
for large rate ratios, and because each output chunk re-reads the source frame on
its leading boundary, a resampled source forces one decoder **restart per chunk**
(the seek goes one frame backward of the decoder's forward position). The
intended fix is to decode each mismatched file source at the mix rate through
`DecoderOptions.SampleRate` so the 1:1 path always runs; same-rate mixes (the
common case — most media is 44.1/48 kHz) already take that path and never
restart.

## Audio encode, temp format & mux

`-f s16le -ar {rate} -ac {channels} -i - -acodec {codec} {temp}`. Float samples
are quantized by clamping to `[-0.99, 0.99]`, scaling by `2^(8*nbytes-1)`, and
rounding to nearest int16.

`-c:a copy` of raw PCM into MP4 is invalid, so the temp audio codec/container is
chosen from the output extension and stream-copied into the final mux:

| Output | Temp container | Codec |
| --- | --- | --- |
| `.mp4`, `.mov`, `.m4v` | `.m4a` | `aac` |
| `.webm` | `.webm` | `libopus` |
| `.ogg`, `.oga` | `.ogg` | `libvorbis` |
| `.mkv` | `.mka` | `aac` |

Audio is rendered to the temp file first (MoviePy order); the video encode then
muxes it with a second input: `-i {temp} -map 0:v:0 -map 1:a:0 -c:a copy
-shortest`. The output timeline (the video duration) is authoritative — audio is
trimmed or zero-padded to match, keeping A/V in sync — and `-shortest` bounds the
mux by the shorter stream so a source that ends early (fewer video frames than
declared) never leaves the audio overrunning the video tail. The temp file is
removed on success and on failure. `ExportOptions.DisableAudio` skips the whole
path; `AudioCodec`/`AudioBitrate` override the temp encode.

`RenderToTemp` samples the sidecar from time 0 and does not apply the clip's
composition `Start()` (placement is a composite concern, matching MoviePy's
`write_audiofile`); wrap a clip in `Mix` to honor a start offset.
