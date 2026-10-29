package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func probeJSON(codec string, dur float64, audio, subs, chapters int, extraVideo string) []byte {
	var streams []string
	streams = append(streams, fmt.Sprintf(`{"index":0,"codec_type":"video","codec_name":%q,"width":1920,"height":1080,"pix_fmt":"yuv420p","color_range":"tv","color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020","disposition":{"attached_pic":0}%s}`, codec, extraVideo))
	for i := 0; i < audio; i++ {
		streams = append(streams, `{"codec_type":"audio","codec_name":"eac3"}`)
	}
	for i := 0; i < subs; i++ {
		streams = append(streams, `{"codec_type":"subtitle","codec_name":"subrip"}`)
	}
	ch := make([]string, chapters)
	for i := range ch {
		ch[i] = "{}"
	}
	return []byte(fmt.Sprintf(`{"streams":[%s],"chapters":[%s],"format":{"format_name":"matroska","duration":"%.3f"}}`,
		strings.Join(streams, ","), strings.Join(ch, ","), dur))
}

func mustInfo(t *testing.T, b []byte) Info {
	t.Helper()
	i, err := ParseProbe(b)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func has(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return true
		}
	}
	return false
}

func TestAutoCRF(t *testing.T) {
	for _, c := range []struct{ w, h, want int }{
		{720, 480, 26}, {1280, 720, 28}, {1920, 1080, 30}, {1920, 800, 30}, {2560, 1440, 32}, {3840, 2160, 34}, {0, 0, 30},
	} {
		if got := AutoCRF(c.w, c.h); got != c.want {
			t.Errorf("AutoCRF(%d,%d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

func TestBuildArgsMKV(t *testing.T) {
	info := mustInfo(t, probeJSON("hevc", 100, 2, 3, 4, ""))
	a := BuildArgs(Options{Preset: 6}, EncSVT, info, "/m/a.mkv", "/m/.a.tmp")
	for _, seq := range [][]string{
		{"-i", "/m/a.mkv"}, {"-map", "0"}, {"-c", "copy"}, {"-map_chapters", "0"}, {"-map_metadata", "0"},
		{"-c:v:0", "libsvtav1"}, {"-crf:v:0", "30"}, {"-preset:v:0", "6"}, {"-pix_fmt:v:0", "yuv420p10le"},
		{"-svtav1-params:v:0", "tune=0:enable-qm=1"},
		{"-color_primaries:v:0", "bt2020"}, {"-color_trc:v:0", "smpte2084"}, {"-colorspace:v:0", "bt2020nc"},
		{"-f", "matroska", "/m/.a.tmp"},
	} {
		if !has(a, seq...) {
			t.Errorf("args lack %v:\n%v", seq, a)
		}
	}
	if has(a, "-c:s", "mov_text") {
		t.Error("mkv must copy subtitles")
	}
}

func TestBuildArgsOptions(t *testing.T) {
	info := mustInfo(t, probeJSON("h264", 100, 1, 0, 0, ""))
	a := BuildArgs(Options{CRF: 24, Preset: 8, FilmGrain: 10}, EncSVT, info, "/m/a.mkv", "/m/t")
	if !has(a, "-crf:v:0", "24") || !has(a, "-preset:v:0", "8") || !has(a, "-svtav1-params:v:0", "tune=0:enable-qm=1:film-grain=10:film-grain-denoise=0") {
		t.Errorf("options not applied: %v", a)
	}
	a = BuildArgs(Options{Preset: 6}, EncAOM, info, "/m/a.mkv", "/m/t")
	if !has(a, "-c:v:0", "libaom-av1") || !has(a, "-b:v:0", "0") || !has(a, "-cpu-used:v:0", "4") {
		t.Errorf("libaom args wrong: %v", a)
	}
}

func TestBuildArgsMP4AndCoverArt(t *testing.T) {
	b := `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1280,"height":720,"disposition":{"attached_pic":0}},
	 {"index":1,"codec_type":"video","codec_name":"mjpeg","width":300,"height":300,"disposition":{"attached_pic":1}},
	 {"index":2,"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"10"}}`
	a := BuildArgs(Options{Preset: 6}, EncSVT, mustInfo(t, []byte(b)), "/m/a.mp4", "/m/t")
	if !has(a, "-c:s", "mov_text") || !has(a, "-f", "mp4", "/m/t") || !has(a, "-crf:v:0", "28") {
		t.Errorf("mp4 args wrong: %v", a)
	}
	if has(a, "-c:v:1", "libsvtav1") {
		t.Errorf("cover art must not be re-encoded: %v", a)
	}
}

func TestPickEncoder(t *testing.T) {
	both := " V....D libaom-av1           libaom AV1\n V..... libsvtav1            SVT-AV1\n"
	if e, err := pickEncoder(both); err != nil || e != EncSVT {
		t.Errorf("want svt, got %q %v", e, err)
	}
	if e, _ := pickEncoder(" V....D libaom-av1  libaom AV1\n"); e != EncAOM {
		t.Errorf("want libaom fallback, got %q", e)
	}
	if _, err := pickEncoder(" V..... libx264  x\n"); err == nil {
		t.Error("want error without AV1 encoder")
	}
}

func TestSkip(t *testing.T) {
	dv := `,"side_data_list":[{"side_data_type":"DOVI configuration record"}]`
	for _, c := range []struct {
		name, path string
		info       Info
		want       string
	}{
		{"h264", "a.mkv", mustInfo(t, probeJSON("h264", 60, 1, 0, 0, "")), ""},
		{"av1", "a.mkv", mustInfo(t, probeJSON("av1", 60, 1, 0, 0, "")), "already AV1"},
		{"dolby vision", "a.mkv", mustInfo(t, probeJSON("hevc", 60, 1, 0, 0, dv)), "Dolby Vision"},
		{"avi", "a.avi", mustInfo(t, probeJSON("h264", 60, 1, 0, 0, "")), "container not supported"},
		{"audio only", "a.mkv", mustInfo(t, []byte(`{"streams":[{"codec_type":"audio","codec_name":"flac"}],"format":{"duration":"5"}}`)), "no video"},
		{"no duration", "a.mkv", mustInfo(t, probeJSON("h264", 0, 1, 0, 0, "")), "unknown duration"},
	} {
		got := Skip(c.info, c.path)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: Skip = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWorthKeeping(t *testing.T) {
	for _, c := range []struct {
		src, out int64
		want     bool
	}{{1000, 850, true}, {1000, 851, false}, {1000, 400, true}, {1000, 1000, false}, {1000, 0, false}, {0, 10, false}} {
		if got := WorthKeeping(c.src, c.out, 15); got != c.want {
			t.Errorf("WorthKeeping(%d,%d) = %v", c.src, c.out, got)
		}
	}
}

func TestVerify(t *testing.T) {
	src := mustInfo(t, probeJSON("hevc", 100, 2, 3, 4, ""))
	good := mustInfo(t, probeJSON("av1", 100.6, 2, 3, 4, ""))
	good.Size = 10
	if err := Verify(src, good); err != nil {
		t.Errorf("good output rejected: %v", err)
	}
	for name, c := range map[string]struct {
		info Info
		want string
	}{
		"short":    {mustInfo(t, probeJSON("av1", 98.5, 2, 3, 4, "")), "duration"},
		"audio":    {mustInfo(t, probeJSON("av1", 100, 1, 3, 4, "")), "audio stream count"},
		"subs":     {mustInfo(t, probeJSON("av1", 100, 2, 0, 4, "")), "subtitle stream count"},
		"chapters": {mustInfo(t, probeJSON("av1", 100, 2, 3, 0, "")), "chapter"},
		"codec":    {mustInfo(t, probeJSON("hevc", 100, 2, 3, 4, "")), "not AV1"},
	} {
		c.info.Size = 10
		if err := Verify(src, c.info); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want error containing %q", name, err, c.want)
		}
	}
	empty := mustInfo(t, probeJSON("av1", 100, 2, 3, 4, ""))
	if err := Verify(src, empty); err == nil {
		t.Error("empty output accepted")
	}
}

// fakeRunner stands in for ffmpeg and ffprobe.
type fakeRunner struct {
	probes   map[string][]byte // path -> ffprobe json
	outSize  int               // bytes ffmpeg "writes"
	outProbe []byte            // ffprobe json of the written file
	runErr   error
	onRun    func() // runs while ffmpeg "encodes"
	proc     *fakeProc
	ran      [][]string
}

// fakeProc records suspend and resume calls.
type fakeProc struct {
	err               error
	suspends, resumes int
}

func (p *fakeProc) Suspend() error {
	if p.err != nil {
		return p.err
	}
	p.suspends++
	return nil
}

func (p *fakeProc) Resume() error {
	p.resumes++
	return nil
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if strings.HasSuffix(name, "ffprobe") && len(args) > 0 {
		p := args[len(args)-1]
		if b, ok := f.probes[p]; ok {
			return b, nil
		}
		if strings.HasSuffix(p, ".roarr-av1.tmp") {
			return f.outProbe, nil
		}
		return nil, fmt.Errorf("no such file")
	}
	return nil, nil
}

func (f *fakeRunner) Run(ctx context.Context, _ int, _ string, args []string, line func(string), started func(Proc)) error {
	f.ran = append(f.ran, args)
	if f.proc != nil && started != nil {
		started(f.proc)
	}
	if f.onRun != nil {
		f.onRun()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	line("out_time_us=50000000")
	line("speed=2.5x")
	if f.runErr != nil {
		return f.runErr
	}
	return os.WriteFile(args[len(args)-1], make([]byte, f.outSize), 0o644)
}

func setup(t *testing.T, f *fakeRunner, srcBytes int, srcProbe []byte) (*Manager, *Job, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Show S01E01.mkv")
	if err := os.WriteFile(path, make([]byte, srcBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	f.probes = map[string][]byte{path: srcProbe}
	m := New(f, t.TempDir())
	m.SetOptions(Options{Enabled: true, Concurrency: 1, Preset: 6, MinSavings: 15})
	if !m.Submit(Item{Path: path, Title: "Show", Kind: "tv", TitleID: 1}) {
		t.Fatal("submit refused")
	}
	return m, m.jobs[0], path
}

func size(t *testing.T, p string) int64 {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

func TestProcessReplacesOriginal(t *testing.T) {
	f := &fakeRunner{outSize: 400, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	var done Job
	m.OnDone(func(j Job) { done = j })
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Done || size(t, path) != 400 || j.SavedBytes != 600 || m.saved != 600 {
		t.Fatalf("state %s (%s), size %d, saved %d", j.State, j.Message, size(t, path), j.SavedBytes)
	}
	if done.ID != j.ID {
		t.Error("OnDone not called")
	}
	if j.Speed != "2.5x" {
		t.Errorf("speed = %q", j.Speed)
	}
	if _, err := os.Stat(tempPath(path)); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
	if st := m.Status(); st.Counts.Done != 1 || st.BytesSaved != 600 {
		t.Errorf("status = %+v", st)
	}
}

func TestProcessKeepsOriginalWhenNotSmaller(t *testing.T) {
	f := &fakeRunner{outSize: 900, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Skipped || size(t, path) != 1000 || !strings.Contains(j.Message, "10% smaller") {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
	if _, err := os.Stat(tempPath(path)); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
}

func TestProcessKeepsOriginalWhenVerifyFails(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 90, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 1000 || !strings.Contains(j.Message, "duration") {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
}

func TestProcessFfmpegErrorKeepsOriginal(t *testing.T) {
	f := &fakeRunner{runErr: fmt.Errorf("exit status 1: boom")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 1000 || !strings.Contains(j.Message, "boom") {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
}

func TestProcessSkipsAV1WithoutEncoding(t *testing.T) {
	f := &fakeRunner{}
	m, j, _ := setup(t, f, 1000, probeJSON("av1", 100, 2, 3, 4, ""))
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Skipped || len(f.ran) != 0 {
		t.Fatalf("state %s (%s), ffmpeg runs %d", j.State, j.Message, len(f.ran))
	}
}

func TestProcessDiscardsResultWhenSourceChanged(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	f.onRun = func() { _ = os.WriteFile(path, make([]byte, 1234), 0o644) } // the *arr swapped the file
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 1234 {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
}

func TestSubmitDedupesAndRespectsToggle(t *testing.T) {
	m := New(&fakeRunner{}, t.TempDir())
	if m.Submit(Item{Path: "/a.mkv"}) {
		t.Error("submit must be refused while disabled")
	}
	m.SetOptions(Options{Enabled: true, Concurrency: 1})
	if !m.Submit(Item{Path: "/a.mkv"}) || m.Submit(Item{Path: "/a.mkv"}) {
		t.Error("duplicate path must be queued once")
	}
}

func TestQueueRunsWithConcurrencyLimitAndPersists(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{outSize: 100, outProbe: probeJSON("av1", 100, 1, 0, 0, ""), probes: map[string][]byte{}}
	m := New(f, dir)
	m.SetOptions(Options{Enabled: true, Concurrency: 1, Preset: 6, MinSavings: 15})
	m.mu.Lock()
	m.enc = EncSVT
	m.mu.Unlock()
	for _, n := range []string{"a", "b"} {
		p := filepath.Join(t.TempDir(), n+".mkv")
		_ = os.WriteFile(p, make([]byte, 1000), 0o644)
		f.probes[p] = probeJSON("hevc", 100, 1, 0, 0, "")
		m.Submit(Item{Path: p, Title: n})
	}
	m.startReady(context.Background())
	if st := m.Status(); st.Counts.Running+st.Counts.Done > 1 {
		t.Errorf("concurrency 1 exceeded: %+v", st.Counts)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && m.Status().Counts.Done < 2 {
		m.startReady(context.Background())
		time.Sleep(10 * time.Millisecond)
	}
	m.save() // wait for the last goroutine's state before reloading
	if st := m.Status(); st.Counts.Done != 2 || st.BytesSaved != 1800 {
		t.Fatalf("status %+v", st)
	}
	again := New(f, dir)
	if st := again.Status(); st.Counts.Done != 2 || st.BytesSaved != 1800 {
		t.Errorf("state not restored: %+v", st)
	}
}

// replaceFile swaps in a different file at path (new inode), like a *arr
// importing an upgrade.
func replaceFile(t *testing.T, path string, content int) {
	t.Helper()
	nf := path + ".new"
	if err := os.WriteFile(nf, make([]byte, content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(nf, path); err != nil {
		t.Fatal(err)
	}
}

func TestSwapKeepsNewerFileReplacedBeforeBackup(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	// Same size and mtime, different inode: only an identity check sees it.
	m.beforeSwap = func() {
		st, _ := os.Stat(path)
		replaceFile(t, path, 1000)
		_ = os.Chtimes(path, st.ModTime(), st.ModTime())
	}
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 1000 {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
	for _, p := range []string{tempPath(path), backupPath(path)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind", p)
		}
	}
}

func TestSwapDoesNotClobberFileAppearingInGap(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.afterBackup = func() { _ = os.WriteFile(path, make([]byte, 777), 0o644) } // *arr imports into the gap
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 777 {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
	if size(t, backupPath(path)) != 1000 {
		t.Error("previous file not kept as backup")
	}
}

func TestSwapLeavesNoBackupOnSuccess(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Done || size(t, path) != 300 {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
	if _, err := os.Stat(backupPath(path)); !os.IsNotExist(err) {
		t.Error("backup left behind")
	}
}

func TestConcurrentSavesStayValid(t *testing.T) {
	dir := t.TempDir()
	m := New(&fakeRunner{}, dir)
	m.SetOptions(Options{Enabled: true, Concurrency: 1})
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Submit(Item{Path: fmt.Sprintf("/a/%d.mkv", i)})
			m.save()
		}(i)
	}
	wg.Wait()
	m.save()
	if st := New(&fakeRunner{}, dir).Status(); st.Counts.Queued != 40 {
		t.Errorf("reloaded %d queued jobs, want 40", st.Counts.Queued)
	}
}

func TestSwapWithoutHardLinksRenamesIn(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.linkFn = func(a, b string) error { return &os.LinkError{Op: "link", Old: a, New: b, Err: syscall.ENOTSUP} }
	m.noReplaceFn = lstatRename
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Done || size(t, path) != 300 {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
	for _, p := range []string{tempPath(path), backupPath(path)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind", p)
		}
	}
}

func TestSwapFailureWithoutHardLinksRestoresOriginal(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.linkFn = func(a, b string) error { return syscall.ENOTSUP }
	m.noReplaceFn = func(a, b string) error { // moving the result in fails; everything else works
		if a == tempPath(path) {
			return errors.New("disk error")
		}
		return lstatRename(a, b)
	}
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 1000 {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
	if _, err := os.Stat(backupPath(path)); !os.IsNotExist(err) {
		t.Error("backup left behind after restore")
	}
}

// lstatRename stands in for renameNoReplace on any platform: it is not
// atomic, but tests that use it do not race.
func lstatRename(a, b string) error {
	if _, err := os.Lstat(b); err == nil {
		return &os.LinkError{Op: "rename", Old: a, New: b, Err: syscall.EEXIST}
	}
	return os.Rename(a, b)
}

func noHardLinkOnResult(path string) func(a, b string) error {
	return func(a, b string) error {
		if a == tempPath(path) {
			return &os.LinkError{Op: "link", Old: a, New: b, Err: syscall.ENOTSUP}
		}
		return os.Link(a, b)
	}
}

func TestSwapWithoutHardLinksKeepsFileAppearingAtInstall(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.linkFn = noHardLinkOnResult(path)
	m.noReplaceFn = func(a, b string) error { // *arr imports after the check, before the move
		if a == tempPath(path) {
			_ = os.WriteFile(b, make([]byte, 777), 0o644)
		}
		return lstatRename(a, b)
	}
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 777 {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
	if size(t, backupPath(path)) != 1000 {
		t.Error("previous file not kept as backup")
	}
}

func TestSwapWithoutNoReplaceRenameRestoresViaLink(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.linkFn = noHardLinkOnResult(path)
	m.noReplaceFn = func(a, b string) error { return errNoReplaceUnsupported }
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 1000 {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
	if _, err := os.Stat(backupPath(path)); !os.IsNotExist(err) {
		t.Error("backup left behind after restore")
	}
}

func TestSwapWithoutAnyNoReplaceMoveKeepsBackup(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.linkFn = func(a, b string) error { return syscall.ENOTSUP }
	m.noReplaceFn = func(a, b string) error { return errNoReplaceUnsupported }
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || !strings.Contains(j.Message, backupPath(path)) {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
	if size(t, backupPath(path)) != 1000 {
		t.Error("original not kept as backup")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Error("path was refilled without a safe move")
	}
}

func TestSwapRefusesWhenBackupExists(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	if err := os.WriteFile(backupPath(path), make([]byte, 555), 0o644); err != nil {
		t.Fatal(err)
	}
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || !strings.Contains(j.Message, backupPath(path)) {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
	if size(t, backupPath(path)) != 555 || size(t, path) != 1000 {
		t.Error("backup or original touched")
	}
}
