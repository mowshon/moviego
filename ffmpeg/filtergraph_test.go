package ffmpeg

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

func TestFilterStringGoldens(t *testing.T) {
	r := clip.Rate{Num: 30, Den: 1}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"scale", Scale("0:v", "v1", 320, 240), "[0:v]scale=320:240[v1]"},
		{"crop", Crop("v1", "v2", 100, 80, 10, 5), "[v1]crop=100:80:10:5[v2]"},
		{"transpose-90", Transpose("0:v", "v1", 1), "[0:v]transpose=1[v1]"},
		{"transpose-180", Transpose("0:v", "v1", 2), "[0:v]transpose=1,transpose=1[v1]"},
		{"transpose-270", Transpose("0:v", "v1", 3), "[0:v]transpose=2[v1]"},
		{"transpose-0", Transpose("0:v", "v1", 0), "[0:v]null[v1]"},
		{"trim", Trim("0:v", "v1", time.Second, 2*time.Second), "[0:v]trim=start=1:duration=2,setpts=PTS-STARTPTS[v1]"},
		{"trim-frac", Trim("0:v", "v1", 500*time.Millisecond, 1500*time.Millisecond), "[0:v]trim=start=0.5:duration=1.5,setpts=PTS-STARTPTS[v1]"},
		{"setpts", SetPTS("0:v", "v1", 2), "[0:v]setpts=PTS/2[v1]"},
		{"setpts-frac", SetPTS("0:v", "v1", 0.5), "[0:v]setpts=PTS/0.5[v1]"},
		{"fade-in", Fade("0:v", "v1", FadeInKind, 0, time.Second, [3]byte{0, 0, 0}), "[0:v]fade=t=in:st=0:d=1:color=0x000000[v1]"},
		{"fade-out", Fade("v1", "v2", FadeOutKind, 4*time.Second, time.Second, [3]byte{255, 0, 0}), "[v1]fade=t=out:st=4:d=1:color=0xff0000[v2]"},
		{"fps", FPS("v1", "vout", r), "[v1]fps=30/1[vout]"},
		{"concat", Concat([]string{"v0", "v1", "v2"}, "vout"), "[v0][v1][v2]concat=n=3:v=1:a=0[vout]"},
		{"overlay", Overlay("base", "ovl", "vout", 12, 34), "[base][ovl]overlay=12:34[vout]"},
		{"volume", Volume("0:a", "a1", 0.5), "[0:a]volume=0.5[a1]"},
		{"afade", AFade("0:a", "a1", FadeInKind, 0, time.Second), "[0:a]afade=t=in:st=0:d=1[a1]"},
		{"eq-brightness", EqColor("0:v", "v1", 0.2, 1, 1, 1), "[0:v]eq=brightness=0.2[v1]"},
		{"eq-contrast", EqColor("0:v", "v1", 0, 1.5, 1, 1), "[0:v]eq=contrast=1.5[v1]"},
		{"eq-multi", EqColor("0:v", "v1", 0.1, 1.2, 0.8, 2), "[0:v]eq=brightness=0.1:contrast=1.2:saturation=0.8:gamma=2[v1]"},
		{"eq-identity", EqColor("0:v", "v1", 0, 1, 1, 1), "[0:v]null[v1]"},
		{"colorbalance", ColorBalance("0:v", "v1", [3]float64{0.1, 0, 0}, [3]float64{0, 0.2, 0}, [3]float64{0, 0, 0.3}), "[0:v]colorbalance=rs=0.1:gs=0:bs=0:rm=0:gm=0.2:bm=0:rh=0:gh=0:bh=0.3[v1]"},
		{"hue", Hue("0:v", "v1", 30, 1.5), "[0:v]hue=h=30:s=1.5[v1]"},
		{"gblur", GBlur("0:v", "v1", 2.5), "[0:v]gblur=sigma=2.5[v1]"},
		{"lut3d", Lut3D("0:v", "v1", "film.cube"), "[0:v]lut3d=file=film.cube[v1]"},
		{"lut3d-escape", Lut3D("0:v", "v1", `C:\Users\a'b,c;d.cube`), `[0:v]lut3d=file=C\:\\Users\\a\'b\,c\;d.cube[v1]`},
		{"rotate", RotateAngle("0:v", "v1", 0.5, 100, 80, [3]byte{0, 0, 0}), "[0:v]rotate=a=-0.5:ow=100:oh=80:c=0x000000[v1]"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestFilterGraphAssembly(t *testing.T) {
	g := &FilterGraph{
		Inputs: []Input{
			{Name: "in.mp4"},
			{Args: []string{"-f", "lavfi"}, Name: "color=c=black:s=320x240"},
		},
		Lines: []string{
			"[0:v]trim=start=1:duration=2,setpts=PTS-STARTPTS[v1]",
			"[v1]scale=320:240[vout]",
		},
		OutVideo: "vout",
	}
	wantFC := "[0:v]trim=start=1:duration=2,setpts=PTS-STARTPTS[v1];[v1]scale=320:240[vout]"
	if got := g.FilterComplex(); got != wantFC {
		t.Errorf("FilterComplex = %q, want %q", got, wantFC)
	}
	wantArgs := []string{"-i", "in.mp4", "-f", "lavfi", "-i", "color=c=black:s=320x240"}
	got := g.InputArgs()
	if len(got) != len(wantArgs) {
		t.Fatalf("InputArgs = %v, want %v", got, wantArgs)
	}
	for i := range got {
		if got[i] != wantArgs[i] {
			t.Fatalf("InputArgs[%d] = %q, want %q", i, got[i], wantArgs[i])
		}
	}
}

// TestSafeInputPathLeadingDash guards the filename-safety rule for fused inputs.
func TestSafeInputPathLeadingDash(t *testing.T) {
	g := &FilterGraph{Inputs: []Input{{Name: "-weird.mp4"}}}
	got := g.InputArgs()
	if len(got) != 2 || got[1] != "./-weird.mp4" {
		t.Fatalf("InputArgs = %v, want [-i ./-weird.mp4]", got)
	}
}
