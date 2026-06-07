//go:build !unix

package ffmpeg

import "testing"

// assertNoProcessGroup is unix-only; elsewhere we rely on exec's context kill.
func assertNoProcessGroup(t *testing.T, pid int) { t.Helper() }
