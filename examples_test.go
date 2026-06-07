package mgo_test

import (
	"os/exec"
	"testing"
)

// TestExamplesCompile builds every program under examples/ so a broken example
// fails CI even when only `go test` (not `go build ./...`) is run. It does not
// execute them — the examples write real files through FFmpeg; their logic is
// covered by the facade and render tests. Skipped when the go toolchain is not
// on PATH (e.g. a stripped CI image running prebuilt test binaries).
func TestExamplesCompile(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	cmd := exec.Command("go", "build", "./examples/...")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("examples failed to build: %v\n%s", err, out)
	}
}
