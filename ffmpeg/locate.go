package ffmpeg

import (
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/mowshon/moviego/clip"
)

// Override these via the environment to pin a specific toolchain build.
const (
	EnvFFmpeg  = "MGO_FFMPEG"
	EnvFFprobe = "MGO_FFPROBE"
)

var (
	ffmpeg  located
	ffprobe located
)

type located struct {
	once sync.Once
	path string
	err  error
}

// FFmpegPath resolves the ffmpeg binary ($MGO_FFMPEG, else PATH) and caches the
// result for the process lifetime.
func FFmpegPath() (string, error) {
	ffmpeg.once.Do(func() { ffmpeg.path, ffmpeg.err = locate(EnvFFmpeg, "ffmpeg") })
	return ffmpeg.path, ffmpeg.err
}

// FFprobePath resolves the ffprobe binary ($MGO_FFPROBE, else PATH) and caches
// the result for the process lifetime.
func FFprobePath() (string, error) {
	ffprobe.once.Do(func() { ffprobe.path, ffprobe.err = locate(EnvFFprobe, "ffprobe") })
	return ffprobe.path, ffprobe.err
}

func locate(env, name string) (string, error) {
	if p := os.Getenv(env); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", clip.Wrap("locate "+name, err)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", clip.Wrap("locate "+name, err)
	}
	return p, nil
}

// Version returns the first line of `<bin> -version`, e.g. "ffmpeg version 8.1.1".
func Version(bin string) (string, error) {
	out, err := exec.Command(bin, "-version").Output()
	if err != nil {
		return "", clip.Wrap("ffmpeg version", err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line), nil
}
