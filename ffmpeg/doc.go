// Package ffmpeg is the subprocess boundary between MovieGo and the FFmpeg
// toolchain. It locates the ffmpeg/ffprobe binaries, runs them under explicit
// process-group control so cancellation never leaves orphans, and turns
// ffprobe's JSON into structured MediaInfo. This package owns process lifecycle,
// metadata, and command assembly, and depends on package clip for value types.
//
// The no-orphan guarantee is currently Unix-only: cancellation SIGKILLs the
// child's whole process group. On other platforms (Windows) only the direct
// child is killed via exec's default; a job-object equivalent is a known gap.
package ffmpeg
