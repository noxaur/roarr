package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRealFFmpeg converts a generated clip with the real ffmpeg. It is
// skipped where ffmpeg has no AV1 encoder.
func TestRealFFmpeg(t *testing.T) {
	ctx := context.Background()
	enc, err := detectEncoder(ctx, ExecRunner{}, "ffmpeg", "ffprobe")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	srt := filepath.Join(dir, "s.srt")
	meta := filepath.Join(dir, "m.txt")
	_ = os.WriteFile(srt, []byte("1\n00:00:00,000 --> 00:00:01,000\nhi\n"), 0o644)
	_ = os.WriteFile(meta, []byte(";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1000\ntitle=a\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=1000\nEND=3000\ntitle=b\n"), 0o644)
	src := filepath.Join(dir, "clip.mkv")
	gen := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=3",
		"-f", "lavfi", "-i", "sine=duration=3", "-f", "lavfi", "-i", "sine=frequency=880:duration=3",
		"-i", srt, "-i", meta, "-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s", "-map_chapters", "4",
		"-c:v", "libx264", "-crf", "12", "-c:a", "aac", "-c:s", "srt", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot generate a clip: %v %s", err, out)
	}
	m := New(ExecRunner{}, t.TempDir())
	m.SetOptions(Options{Enabled: true, Concurrency: 1, Preset: 10, MinSavings: 5, Nice: 5})
	m.mu.Lock()
	m.enc = enc
	m.mu.Unlock()
	before := size(t, src)
	m.Submit(Item{Path: src})
	j := m.jobs[0]
	m.process(ctx, j, m.opts, enc)
	if j.State != Done {
		t.Fatalf("state %s: %s", j.State, j.Message)
	}
	info, err := Probe(ctx, ExecRunner{}, "ffprobe", src)
	if err != nil {
		t.Fatal(err)
	}
	if v := info.MainVideo(); len(v) != 1 || v[0].CodecName != "av1" || v[0].PixFmt != "yuv420p10le" {
		t.Errorf("video = %+v", v)
	}
	if info.Count("audio") != 2 || info.Count("subtitle") != 1 || len(info.Chapters) != 2 {
		t.Errorf("streams lost: audio %d subs %d chapters %d", info.Count("audio"), info.Count("subtitle"), len(info.Chapters))
	}
	t.Logf("%s: %d -> %d bytes", enc, before, size(t, src))
}

func TestRealFFmpegMP4(t *testing.T) {
	ctx := context.Background()
	enc, err := detectEncoder(ctx, ExecRunner{}, "ffmpeg", "ffprobe")
	if err != nil {
		t.Skip(err)
	}
	src := filepath.Join(t.TempDir(), "clip.mp4")
	gen := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=2",
		"-f", "lavfi", "-i", "sine=duration=2", "-c:v", "libx264", "-crf", "12", "-c:a", "aac", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot generate a clip: %v %s", err, out)
	}
	m := New(ExecRunner{}, t.TempDir())
	m.SetOptions(Options{Enabled: true, Concurrency: 1, Preset: 10, MinSavings: 5})
	m.Submit(Item{Path: src})
	j := m.jobs[0]
	m.process(ctx, j, m.opts, enc)
	if j.State != Done {
		t.Fatalf("state %s: %s", j.State, j.Message)
	}
}
