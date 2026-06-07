//go:build !unix

package ffmpeg

import (
	"os/exec"
	"time"
)

// configureProc falls back to exec's default context kill on non-unix platforms,
// which terminates only the direct child: the whole-process-group no-orphan
// guarantee is Unix-only for now (a Windows job object would go here, see the
// package doc). WaitDelay backstops a wedged child.
func configureProc(cmd *exec.Cmd) {
	cmd.WaitDelay = 10 * time.Second
}

func killProc(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
