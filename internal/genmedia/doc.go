// Package genmedia generates small, deterministic media fixtures with FFmpeg
// for integration tests: solid-color and rotated clips, sine-wave audio, and
// combined video+audio. Files are written into a caller-provided directory
// (typically t.TempDir()) so nothing is committed to the repo. Callers should
// guard with Available, which skips when the toolchain is missing.
package genmedia
