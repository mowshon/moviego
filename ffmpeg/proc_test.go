package ffmpeg

import (
	"context"
	"testing"
	"time"
)

func TestProcCancelKillsPromptly(t *testing.T) {
	bin, err := FFmpegPath()
	if err != nil {
		t.Skip("ffmpeg not available:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc, err := Start(ctx, Spec{
		Path: bin,
		// A long-running encode with chatty stderr (-stats), so we also prove the
		// stderr drain does not block process exit.
		Args: []string{
			"-nostdin", "-stats",
			"-f", "lavfi", "-i", "testsrc=size=320x240:rate=30",
			"-t", "100", "-f", "null", "-",
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := proc.cmd.Process.Pid

	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- proc.Wait() }()

	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed > 5*time.Second {
			t.Fatalf("Wait took %v after cancel; expected prompt exit", elapsed)
		}
		if ctx.Err() == nil {
			t.Fatal("context not canceled")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Wait did not return after cancel: process or stderr drain blocked")
	}

	assertNoProcessGroup(t, pid)
}
