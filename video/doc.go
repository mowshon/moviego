// Package video defines the VideoClip interface and the concrete video graph
// nodes. Low-level FFmpeg decode/encode lives in video/io; this package owns the
// clip graph and its capabilities (parallel-safety and source access class), not
// process lifecycle.
package video
