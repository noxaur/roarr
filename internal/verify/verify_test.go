package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// scene draws a frame that looks like footage: a gradient with shapes whose
// position depends on seed, plus grain.
func scene(seed int) image.Image {
	img := image.NewGray(image.Rect(0, 0, 160, 90))
	r := rand.New(rand.NewPCG(uint64(seed), 1))
	cx, cy := 20+seed*17%120, 15+seed*11%60
	for y := 0; y < 90; y++ {
		for x := 0; x < 160; x++ {
			v := float64(x+y+seed*13%80) * 0.9
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) < 300 {
				v = 230 - v/3
			}
			v += r.Float64()*30 - 15
			img.SetGray(x, y, color.Gray{uint8(math.Max(0, math.Min(255, v)))})
		}
	}
	return img
}

func solid(v uint8) image.Image {
	img := image.NewGray(image.Rect(0, 0, 160, 90))
	for i := range img.Pix {
		img.Pix[i] = v
	}
	return img
}

// slate is white text-like bars on black: bright, contrasty, but only two
// brightness levels.
func slate() image.Image {
	img := image.NewGray(image.Rect(0, 0, 160, 90))
	for y := 35; y < 55; y++ {
		for x := 20; x < 140; x++ {
			if (x/4)%2 == 0 {
				img.SetGray(x, y, color.Gray{235})
			}
		}
	}
	return img
}

func TestFrameStats(t *testing.T) {
	if s := Stats(solid(0)); !s.Black() || s.Real() {
		t.Fatalf("black frame: %+v", s)
	}
	if s := Stats(solid(128)); !s.Flat() || s.Real() {
		t.Fatalf("grey frame: %+v", s)
	}
	if s := Stats(slate()); !s.Flat() || s.Entropy > 2.5 {
		t.Fatalf("slate should be flat (low entropy): mean=%.1f std=%.1f entropy=%.2f", s.Mean, s.Std, s.Entropy)
	}
	s := Stats(scene(3))
	if !s.Real() {
		t.Fatalf("scene should be real: mean=%.1f std=%.1f entropy=%.2f edges=%.2f", s.Mean, s.Std, s.Entropy, s.Edges)
	}
	if d := Diff(Stats(scene(1)), Stats(scene(5))); d < staticDiff {
		t.Fatalf("different scenes differ by only %.2f", d)
	}
	if d := Diff(Stats(scene(2)), Stats(scene(2))); d != 0 {
		t.Fatalf("same frame diff = %v", d)
	}
}

func statsOf(imgs ...image.Image) []FrameStats {
	out := make([]FrameStats, len(imgs))
	for i, im := range imgs {
		out[i] = Stats(im)
	}
	return out
}

func TestJudgePicture(t *testing.T) {
	cases := []struct {
		name   string
		frames []FrameStats
		failed int
		want   Level
	}{
		{"real footage", statsOf(scene(1), scene(2), scene(3), scene(4), scene(5), scene(6), scene(7)), 0, Pass},
		{"dark scenes are normal", statsOf(scene(1), solid(2), scene(3), scene(4), solid(0), scene(6), scene(7)), 0, Pass},
		{"all black", statsOf(solid(0), solid(1), solid(0), solid(3), solid(0), solid(0)), 0, Fail},
		{"text slate", statsOf(slate(), slate(), slate(), slate(), slate(), slate()), 0, Fail},
		{"one still image", statsOf(scene(4), scene(4), scene(4), scene(4), scene(4), scene(4)), 0, Fail},
		{"mostly black", statsOf(scene(1), solid(0), solid(0), solid(0), scene(5), solid(0), solid(0)), 0, Warn},
		{"single real frame", statsOf(scene(1), solid(0), solid(0), solid(0), solid(0), solid(0)), 0, Fail},
		{"undecodable", nil, 7, Fail},
		{"half undecodable", statsOf(scene(1), scene(2), scene(3)), 4, Warn},
	}
	for _, c := range cases {
		got := JudgePicture(c.frames, c.failed)
		if got.Level != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got.Level, got.Reason, c.want)
		}
	}
}

func TestSampleTimes(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	ts := SampleTimes(1000, 7, rng)
	if len(ts) != 7 {
		t.Fatalf("got %d times", len(ts))
	}
	for i, x := range ts {
		if x < 50 || x > 950 {
			t.Fatalf("time %v outside the middle 90%%", x)
		}
		// one per slice of (950-50)/7
		lo := 50 + float64(i)*900/7
		if x < lo || x > lo+900/7 {
			t.Fatalf("time %d = %v not in its slice", i, x)
		}
	}
	if SampleTimes(0, 7, rng) != nil {
		t.Fatal("no duration should give no samples")
	}
}

func TestCheckPayload(t *testing.T) {
	cases := []struct {
		name  string
		files []File
		want  Level
		kept  int
	}{
		{"episode", []File{{Name: "Show.S01E01.1080p.mkv", Size: 1 << 30}}, Pass, 1},
		{"pack with sample", []File{{Name: "Pack/Show.S01E01.mkv", Size: 1 << 30}, {Name: "Pack/Show.S01E02.mkv", Size: 1 << 30}, {Name: "Pack/Sample/show.sample.mkv", Size: 1 << 24}, {Name: "Pack/info.nfo"}}, Pass, 2},
		{"codec exe", []File{{Name: "Movie/Movie.2024.1080p.mkv", Size: 1 << 30}, {Name: "Movie/Codec.Setup.exe", Size: 1 << 20}}, Fail, 1},
		{"rarbg marker", []File{{Name: "M/m.mkv", Size: 1 << 30}, {Name: "M/RARBG_DO_NOT_MIRROR.exe", Size: 99}}, Pass, 1},
		{"shortcut", []File{{Name: "Movie/Movie.2024.1080p.mp4.lnk", Size: 1 << 10}}, Fail, 0},
		{"password rar", []File{{Name: "Movie/movie.rar", Size: 1 << 30}, {Name: "Movie/movie.r00", Size: 1 << 30}, {Name: "Movie/password.txt"}}, Fail, 0},
		{"rar with video", []File{{Name: "Movie/movie.mkv", Size: 1 << 30}, {Name: "Movie/subs.zip", Size: 1 << 20}}, Pass, 1},
		{"sample only", []File{{Name: "Movie.2024.1080p-sample.mkv", Size: 1 << 25}}, Fail, 0},
		{"trailer only", []File{{Name: "Movie (2024)/Movie-trailer.mp4", Size: 1 << 25}}, Fail, 0},
		{"title with trailer", []File{{Name: "Trailer.Park.Boys.S01E01.720p.mkv", Size: 1 << 29}}, Pass, 1},
		{"tiny unnamed sample", []File{{Name: "M/movie.mkv", Size: 4 << 30}, {Name: "M/abc.mkv", Size: 50 << 20}}, Pass, 1},
		{"nothing", []File{{Name: "readme.txt"}}, Fail, 0},
		{"disc image", []File{{Name: "Movie/Movie.iso", Size: 40 << 30}}, Warn, 0},
	}
	for _, c := range cases {
		got, kept := CheckPayload(c.files)
		if got.Level != c.want || len(kept) != c.kept {
			t.Errorf("%s: %s (%s), %d kept; want %s, %d kept", c.name, got.Level, got.Reason, len(kept), c.want, c.kept)
		}
	}
}

func TestPickPrimary(t *testing.T) {
	files := []File{
		{Name: "Show.S01/Show.S01E03.mkv", Size: 3},
		{Name: "Show.S01/Show.S00E01.Special.mkv", Size: 9},
		{Name: "Show.S01/Show.S01E01.mkv", Size: 1},
		{Name: "Show.S01/Show.S01E02.mkv", Size: 2},
	}
	if p, _ := PickPrimary(files, "tv"); p.Name != "Show.S01/Show.S01E01.mkv" {
		t.Fatalf("tv primary = %s", p.Name)
	}
	if p, _ := PickPrimary(files, "movie"); p.Size != 9 {
		t.Fatalf("movie primary = %s", p.Name)
	}
	anime := []File{{Name: "[G] Show - 02 (1080p).mkv"}, {Name: "[G] Show - 01 (1080p).mkv"}}
	if p, _ := PickPrimary(anime, "tv"); !strings.Contains(p.Name, "- 01") {
		t.Fatalf("anime primary = %s", p.Name)
	}
}

func TestCheckDuration(t *testing.T) {
	cases := []struct {
		sec     float64
		runtime int
		eps     int
		kind    string
		want    Level
	}{
		{44 * 60, 45, 1, "tv", Pass},
		{60, 45, 1, "tv", Fail},       // sample
		{10 * 60, 45, 1, "tv", Fail},  // cut / preview
		{25 * 60, 45, 1, "tv", Warn},  // short
		{88 * 60, 45, 2, "tv", Pass},  // double episode
		{88 * 60, 45, 1, "tv", Pass},  // long, within 3.5x
		{200 * 60, 45, 1, "tv", Warn}, // way off
		{118 * 60, 120, 1, "movie", Pass},
		{3 * 60, 0, 1, "movie", Fail},
		{30 * 60, 0, 1, "movie", Warn},
		{100 * 60, 0, 1, "movie", Pass},
		{11 * 60, 0, 1, "tv", Pass}, // kids' show
		{0, 45, 1, "tv", Warn},
	}
	for _, c := range cases {
		if got := CheckDuration(c.sec, c.runtime, c.eps, c.kind); got.Level != c.want {
			t.Errorf("%v s, runtime %d x%d %s: %s (%s), want %s", c.sec, c.runtime, c.eps, c.kind, got.Level, got.Reason, c.want)
		}
	}
}

func TestCountCues(t *testing.T) {
	srt := "\ufeff1\n00:00:01,000 --> 00:00:02,000\n<i>Hello</i>\n\n2\n00:00:03,000 --> 00:00:04,000\n{\\an8}\n\n3\n00:00:05,000 --> 00:00:06,000\nTwo\nlines\n"
	if n := CountCues([]byte(srt)); n != 2 {
		t.Fatalf("cues = %d, want 2", n)
	}
	if CountCues(nil) != 0 {
		t.Fatal("empty input")
	}
}

// fakeRunner serves a probe fixture and generated frames.
type fakeRunner struct {
	probe  []byte
	frame  func(at float64) image.Image
	srt    string
	calls  []string
	failAt map[int]bool // frame call numbers that fail
	n      int
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name == "ffprobe" {
		return f.probe, nil
	}
	if containsStr(args, "srt") {
		return []byte(f.srt), nil
	}
	f.n++
	if f.failAt[f.n] {
		return nil, errors.New("decode error")
	}
	at := 0.0
	for i, a := range args {
		if a == "-ss" {
			at, _ = strconv.ParseFloat(args[i+1], 64)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, f.frame(at))
	return buf.Bytes(), nil
}

func newFake(t *testing.T, probe string, frame func(float64) image.Image) (*Verifier, *fakeRunner, string) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + probe)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{probe: b, frame: frame}
	v := &Verifier{Tools: Tools{FFprobe: "ffprobe", FFmpeg: "ffmpeg"}, Run: fr, Rand: rand.New(rand.NewPCG(7, 7))}
	return v, fr, file
}

func TestVerifyGoodRelease(t *testing.T) {
	v, fr, file := newFake(t, "multi.json", func(at float64) image.Image { return scene(int(at)) })
	vd := v.Verify(context.Background(), Target{
		Release:    "Movie.2024.MULTi.VFF.1080p.BluRay.x265.10bit.HDR.ENG.SUBS-GRP",
		Kind:       "movie",
		RuntimeMin: 44,
		Files:      []File{{Name: "Movie/movie.mkv", Path: file, Size: 1 << 30}, {Name: "Movie/movie.nfo", Size: 10}},
	})
	if vd.Level != Pass {
		t.Fatalf("verdict %s: %s\n%+v", vd.Level, vd.Summary, vd.Checks)
	}
	if vd.Media == nil || vd.Media.Width != 1920 || len(vd.Media.Audio) != 3 {
		t.Fatalf("media = %+v", vd.Media)
	}
	frames := 0
	for _, c := range fr.calls {
		if strings.HasPrefix(c, "ffmpeg") && strings.Contains(c, "image2pipe") {
			frames++
		}
	}
	if frames != 7 {
		t.Fatalf("sampled %d frames, want 7", frames)
	}
}

func TestVerifyFakeRelease(t *testing.T) {
	v, _, file := newFake(t, "fake.json", func(float64) image.Image { return slate() })
	vd := v.Verify(context.Background(), Target{
		Release: "Big.Movie.2023.2160p.WEB-DL.x265-FAKE",
		Kind:    "movie",
		Files:   []File{{Name: "Big.Movie.2023.2160p.WEB-DL.x265-FAKE.mkv", Path: file, Size: 1 << 30}},
	})
	if vd.Level != Fail {
		t.Fatalf("verdict %s: %s", vd.Level, vd.Summary)
	}
	c, _ := checkByName(vd.Checks, "picture")
	if c.Level != Fail {
		t.Fatalf("picture: %s %s", c.Level, c.Reason)
	}
}

func TestVerifyCountsUntaggedTextSubs(t *testing.T) {
	v, fr, file := newFake(t, "fake.json", func(at float64) image.Image { return scene(int(at)) })
	// Drop the frame-count tag so the verifier has to read cues.
	p, _ := ParseProbe(fr.probe)
	p.Streams[2].Tags = map[string]string{"language": "eng"}
	fr.probe = mustJSON(t, p)
	fr.srt = "1\n00:00:01,000 --> 00:00:02,000\nHi\n"
	vd := v.Verify(context.Background(), Target{Release: "Big.Movie.2023.720p.WEB-DL.ENG.SUBS-GRP", Kind: "movie", Files: []File{{Name: "m.mkv", Path: file, Size: 1}}})
	c, _ := checkByName(vd.Checks, "subtitles")
	if c.Level != Pass {
		t.Fatalf("subtitles: %s %s", c.Level, c.Reason)
	}
}

func TestVerifySkipsInvisibleFiles(t *testing.T) {
	v, _, _ := newFake(t, "multi.json", nil)
	vd := v.Verify(context.Background(), Target{Kind: "movie", Files: []File{{Name: "m.mkv", Path: "/nowhere/m.mkv", Size: 1}}})
	if vd.Level != Skip {
		t.Fatalf("verdict %s: %s", vd.Level, vd.Summary)
	}
	// A payload failure needs no file access at all.
	vd = v.Verify(context.Background(), Target{Kind: "movie", Files: []File{{Name: "m.rar"}, {Name: "setup.exe"}}})
	if vd.Level != Fail {
		t.Fatalf("verdict %s: %s", vd.Level, vd.Summary)
	}
}

// TestRealFFmpeg encodes tiny clips and verifies them end to end. It runs
// only where ffmpeg and ffprobe are installed.
func TestRealFFmpeg(t *testing.T) {
	tools := FindTools()
	if tools.FFprobe == "" || tools.FFmpeg == "" {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	dir := t.TempDir()
	encode := func(name string, args ...string) string {
		out := filepath.Join(dir, name)
		cmd := exec.Command(tools.FFmpeg, append(append([]string{"-v", "error", "-y"}, args...), out)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("encode %s: %v\n%s", name, err, b)
		}
		return out
	}
	real := encode("real.mkv", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=10:duration=180",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=180", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac",
		"-metadata:s:a:0", "language=eng")
	black := encode("black.mkv", "-f", "lavfi", "-i", "color=c=black:size=1280x720:rate=10:duration=180",
		"-c:v", "libx264", "-preset", "ultrafast")
	srt := filepath.Join(dir, "en.srt")
	if err := os.WriteFile(srt, []byte("1\n00:00:05,000 --> 00:00:07,000\nHello there\n\n2\n00:01:05,000 --> 00:01:07,000\nGeneral Kenobi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	subbed := encode("subbed.mkv", "-i", real, "-i", srt, "-map", "0", "-map", "1", "-c", "copy", "-c:s", "srt", "-metadata:s:s:0", "language=eng")
	v := New(tools)
	v.Frames = 5

	vd0 := v.Verify(context.Background(), Target{Release: "Test.S01E01.720p.WEB-DL.x264.ENG.SUBS-GRP", Kind: "tv", RuntimeMin: 3, Files: []File{{Name: "Test.S01E01.mkv", Path: subbed, Size: 1 << 20}}})
	if c, _ := checkByName(vd0.Checks, "subtitles"); c.Level != Pass || !strings.Contains(c.Reason, "present") {
		t.Fatalf("subbed clip: %+v", vd0.Checks)
	}
	vd0 = v.Verify(context.Background(), Target{Release: "Test.S01E01.720p.WEB-DL.x264.ENG.SUBS-GRP", Kind: "tv", RuntimeMin: 3, Files: []File{{Name: "Test.S01E01.mkv", Path: real, Size: 1 << 20}}})
	if c, _ := checkByName(vd0.Checks, "subtitles"); c.Level != Fail {
		t.Fatalf("unsubbed clip claiming subs: %+v", vd0.Checks)
	}

	vd := v.Verify(context.Background(), Target{Release: "Test.S01E01.720p.WEB-DL.x264-GRP", Kind: "tv", RuntimeMin: 3, Files: []File{{Name: "Test.S01E01.720p.WEB-DL.x264-GRP.mkv", Path: real, Size: 1 << 20}}})
	if vd.Level != Pass {
		t.Fatalf("real clip: %s: %s\n%+v", vd.Level, vd.Summary, vd.Checks)
	}
	vd = v.Verify(context.Background(), Target{Release: "Test.S01E01.1080p.WEB-DL.x264-GRP", Kind: "tv", RuntimeMin: 3, Files: []File{{Name: "Test.S01E01.mkv", Path: black, Size: 1 << 20}}})
	if vd.Level != Fail {
		t.Fatalf("black clip: %s: %s", vd.Level, vd.Summary)
	}
	if c, _ := checkByName(vd.Checks, "picture"); c.Level != Fail {
		t.Fatalf("black picture check: %+v", c)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
