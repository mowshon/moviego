//go:build unix

package ffmpeg

import (
	"syscall"
	"testing"
	"time"
)

// assertNoProcessGroup verifies the killed child's process group has no surviving
// members, i.e. no orphaned FFmpeg children. signal 0 to a negative pid probes
// the group; ESRCH means it is gone.
func assertNoProcessGroup(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(-pid, 0)
		if err == syscall.ESRCH {
			return // group fully reaped
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still alive after cancel (err=%v)", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
