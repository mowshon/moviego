//go:build unix

package ffmpeg

import (
	"os/exec"
	"syscall"
	"time"
)

// configureProc puts the child in its own process group and routes both context
// cancellation and explicit Kill through a group-wide SIGKILL, so transcodes
// that spawn helpers die whole. WaitDelay backstops a wedged child.
func configureProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killProc(cmd) }
	cmd.WaitDelay = 10 * time.Second
}

func killProc(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// A negative pid signals the whole process group created by Setpgid.
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil // already exited
	}
	return err
}
