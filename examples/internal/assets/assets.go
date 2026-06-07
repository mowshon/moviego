// Package assets provides small public media fixtures for runnable examples.
package assets

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Asset describes a downloadable example input.
type Asset struct {
	Name string
	URL  string
}

var (
	SampleVideo = Asset{
		Name: "sample-video.mp4",
		URL:  "https://filesamples.com/samples/video/mp4/sample_640x360.mp4",
	}
	SampleAudio = Asset{
		Name: "sample-audio.mp3",
		URL:  "https://filesamples.com/samples/audio/mp3/sample3.mp3",
	}
	PhotoLake = Asset{
		Name: "photo-lake.jpg",
		URL:  "https://picsum.photos/id/10/640/360.jpg",
	}
	PhotoDesk = Asset{
		Name: "photo-desk.jpg",
		URL:  "https://picsum.photos/id/20/640/360.jpg",
	}
	PhotoForest = Asset{
		Name: "photo-forest.jpg",
		URL:  "https://picsum.photos/id/28/640/360.jpg",
	}
	PhotoCity = Asset{
		Name: "photo-city.jpg",
		URL:  "https://picsum.photos/id/42/640/360.jpg",
	}
	TransparentPNG = Asset{
		Name: "transparent-demo.png",
		URL:  "https://upload.wikimedia.org/wikipedia/commons/4/47/PNG_transparency_demonstration_1.png",
	}
	FontInter = Asset{
		Name: "fonts/Inter.ttf",
		URL:  "https://raw.githubusercontent.com/google/fonts/main/ofl/inter/Inter%5Bopsz%2Cwght%5D.ttf",
	}
	FontNotoSans = Asset{
		Name: "fonts/NotoSans.ttf",
		URL:  "https://raw.githubusercontent.com/google/fonts/main/ofl/notosans/NotoSans%5Bwdth%2Cwght%5D.ttf",
	}
	FontRobotoMono = Asset{
		Name: "fonts/RobotoMono.ttf",
		URL:  "https://raw.githubusercontent.com/google/fonts/main/ofl/robotomono/RobotoMono%5Bwght%5D.ttf",
	}
)

// TextFonts is the default font set for text/subtitle examples.
var TextFonts = []Asset{FontInter, FontNotoSans, FontRobotoMono}

// Ensure downloads asset into examples/input if it is not already present.
func Ensure(ctx context.Context, asset Asset) (string, error) {
	dir, err := InputDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, asset.Name)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "moviego-examples/1.0")

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download %s: unexpected HTTP status %s", asset.Name, resp.Status)
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("save %s: %w", asset.Name, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// EnsureTextFonts downloads the default fonts for text/subtitle examples.
func EnsureTextFonts(ctx context.Context) ([]string, error) {
	return EnsureAll(ctx, TextFonts...)
}

// EnsureAll downloads each asset and returns paths in the same order.
func EnsureAll(ctx context.Context, list ...Asset) ([]string, error) {
	paths := make([]string, len(list))
	for i, asset := range list {
		path, err := Ensure(ctx, asset)
		if err != nil {
			return nil, err
		}
		paths[i] = path
	}
	return paths, nil
}

// OutputPath returns a path inside examples/output, creating the directory.
func OutputPath(name string) (string, error) {
	dir, err := OutputDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// InputDir returns the repository examples/input directory.
func InputDir() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "examples", "input"), nil
}

// OutputDir returns the repository examples/output directory.
func OutputDir() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "examples", "output"), nil
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate examples directory")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), nil
}
