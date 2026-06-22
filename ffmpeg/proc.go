package ffmpeg

import (
	"context"
	"io"
	"os/exec"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
)

// stderrRingSize bounds the retained FFmpeg stderr tail.
const stderrRingSize = 64 * 1024

// Spec describes a subprocess to start.
type Spec struct {
	Path   string   // binary path, from FFmpegPath/FFprobePath
	Args   []string // arguments, not including the program name
	Stdin  bool     // attach a stdin pipe (Proc.Stdin)
	Stdout bool     // attach a stdout pipe (Proc.Stdout)
}

// Proc is a running FFmpeg or ffprobe subprocess. Its stderr is drained into a
// bounded ring buffer for the process lifetime, and it runs in its own process
// group so canceling the context kills the whole group rather than leaking
// children.
type Proc struct {
	cmd    *exec.Cmd
	stderr *ringWriter

	// Stdin/Stdout are non-nil only when requested via Spec. The caller owns
	// reading Stdout fully and closing Stdin before Wait.
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
}

// Start launches the subprocess described by spec.
func Start(ctx context.Context, spec Spec) (*Proc, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	p := &Proc{cmd: cmd, stderr: newRingWriter(stderrRingSize)}
	cmd.Stderr = p.stderr // os/exec drains this on its own goroutine; never blocks
	configureProc(cmd)

	if spec.Stdin {
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, clip.Wrap("ffmpeg stdin", err)
		}
		p.Stdin = in
	}
	if spec.Stdout {
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, clip.Wrap("ffmpeg stdout", err)
		}
		p.Stdout = out
	}
	if err := cmd.Start(); err != nil {
		return nil, clip.Wrap("ffmpeg start", err)
	}
	return p, nil
}

// Wait blocks until the process exits. A non-zero exit (including a kill from
// context cancellation) yields an *ExitError carrying the stderr tail.
func (p *Proc) Wait() error {
	if err := p.cmd.Wait(); err != nil {
		return &ExitError{Err: err, Stderr: p.stderr.Bytes()}
	}
	return nil
}

// Kill terminates the process group immediately. Safe to call before exit or
// after; a no-op once the process is gone.
func (p *Proc) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return killProc(p.cmd)
}

// Stderr returns a copy of the retained stderr tail (last 64 KiB).
func (p *Proc) Stderr() []byte { return p.stderr.Bytes() }

// ExitError reports a non-zero FFmpeg exit with the captured stderr tail.
type ExitError struct {
	Err    error
	Stderr []byte
}

func (e *ExitError) Error() string {
	tail := strings.TrimSpace(string(e.Stderr))
	if tail == "" {
		return e.Err.Error()
	}
	if i := strings.LastIndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	return e.Err.Error() + ": " + tail
}

func (e *ExitError) Unwrap() error { return e.Err }
