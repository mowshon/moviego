package genmedia

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/mowshon/moviego/v2/ffmpeg"
)

// Available reports whether both ffmpeg and ffprobe resolve, so tests can skip
// cleanly on machines without the toolchain.
func Available() bool {
	if _, err := ffmpeg.FFmpegPath(); err != nil {
		return false
	}
	_, err := ffmpeg.FFprobePath()
	return err == nil
}

// run invokes ffmpeg; the final arg is the output filename, joined under dir.
func run(dir string, args ...string) (string, error) {
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, args[len(args)-1])
	full := append([]string{"-y", "-nostdin"}, args[:len(args)-1]...)
	full = append(full, out)
	cmd := exec.CommandContext(context.Background(), bin, full...)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return "", &execError{err: err, output: combined}
	}
	return out, nil
}

type execError struct {
	err    error
	output []byte
}

func (e *execError) Error() string { return e.err.Error() + ": " + string(e.output) }

// ColorVideo writes a solid-color H.264 clip of size w x h, dur seconds, at the
// given rate (e.g. "30" or "30000/1001"), and returns its path.
func ColorVideo(dir, name, color string, w, h int, dur float64, rate string) (string, error) {
	return run(dir,
		"-f", "lavfi",
		"-i", lavfiColor(color, w, h, dur, rate),
		"-pix_fmt", "yuv420p",
		name,
	)
}

// TestPatternVideo writes an H.264 clip of FFmpeg's testsrc pattern, whose
// frames differ from one another so per-frame decode correctness is observable.
func TestPatternVideo(dir, name string, w, h int, dur float64, rate string) (string, error) {
	return run(dir,
		"-f", "lavfi",
		"-i", lavfiTestSrc(w, h, dur, rate),
		"-pix_fmt", "yuv420p",
		name,
	)
}

// EncodedVideo writes a test-pattern clip with an explicit video codec (e.g.
// "libx264", "mpeg4"). It is used to build inputs that are deliberately
// codec-incompatible, so the concat demuxer's stream copy must fail and fall
// back to a re-encode.
func EncodedVideo(dir, name, vcodec string, w, h int, dur float64, rate string) (string, error) {
	return run(dir,
		"-f", "lavfi",
		"-i", lavfiTestSrc(w, h, dur, rate),
		"-c:v", vcodec,
		"-pix_fmt", "yuv420p",
		name,
	)
}

// RotatedVideo writes a clip carrying a 90-degree display-matrix rotation, so
// its stored size is w x h but its display size is h x w. FFmpeg only stamps the
// matrix on a remux, so this encodes a base clip then copies it through
// -display_rotation with autorotation disabled.
func RotatedVideo(dir, name string, w, h int, dur float64, rate string) (string, error) {
	base, err := run(dir,
		"-f", "lavfi",
		"-i", lavfiTestSrc(w, h, dur, rate),
		"-pix_fmt", "yuv420p",
		"_rot_base.mp4",
	)
	if err != nil {
		return "", err
	}
	return run(dir,
		"-display_rotation", "90",
		"-noautorotate",
		"-i", base,
		"-c", "copy",
		name,
	)
}

// SineAudio writes a mono sine-wave WAV of dur seconds at the given sample rate.
func SineAudio(dir, name string, freq int, dur float64, sampleRate int) (string, error) {
	return run(dir,
		"-f", "lavfi",
		"-i", lavfiSine(freq, dur, sampleRate),
		name,
	)
}

// VideoWithAudio writes an H.264 + AAC clip combining a test pattern and a sine
// tone of dur seconds.
func VideoWithAudio(dir, name string, w, h int, dur float64, rate string) (string, error) {
	return run(dir,
		"-f", "lavfi",
		"-i", lavfiTestSrc(w, h, dur, rate),
		"-f", "lavfi",
		"-i", lavfiSine(440, dur, 44100),
		"-pix_fmt", "yuv420p",
		"-shortest",
		name,
	)
}

func lavfiColor(color string, w, h int, dur float64, rate string) string {
	return "color=c=" + color + ":s=" + size(w, h) + ":d=" + sec(dur) + ":r=" + rate
}

func lavfiTestSrc(w, h int, dur float64, rate string) string {
	return "testsrc=s=" + size(w, h) + ":d=" + sec(dur) + ":r=" + rate
}

func lavfiSine(freq int, dur float64, sampleRate int) string {
	return "sine=frequency=" + itoa(freq) + ":duration=" + sec(dur) + ":sample_rate=" + itoa(sampleRate)
}

func size(w, h int) string { return itoa(w) + "x" + itoa(h) }

func sec(d float64) string { return strconv.FormatFloat(d, 'f', -1, 64) }

func itoa(n int) string { return strconv.Itoa(n) }
