package importer

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type zipEntry struct {
	name string
	body string
	mode fs.FileMode
	size uint64 // a header size that lies, when set
}

func makeZip(t *testing.T, p string, entries []zipEntry) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(e.body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	for _, e := range entries {
		if e.size != 0 {
			b = lieAboutSize(t, b, e.name, e.size)
		}
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// lieAboutSize rewrites the uncompressed size of an entry in the central
// directory.
func lieAboutSize(t *testing.T, b []byte, name string, size uint64) []byte {
	t.Helper()
	sig := []byte{'P', 'K', 1, 2}
	for i := bytes.Index(b, sig); i >= 0; {
		nameLen := int(b[i+28]) | int(b[i+29])<<8
		if string(b[i+46:i+46+nameLen]) == name {
			s := uint32(size)
			b[i+24], b[i+25], b[i+26], b[i+27] = byte(s), byte(s>>8), byte(s>>16), byte(s>>24)
			return b
		}
		j := bytes.Index(b[i+4:], sig)
		if j < 0 {
			break
		}
		i += 4 + j
	}
	t.Fatalf("entry %s not found", name)
	return nil
}

func TestUnzipSafeEntriesOnly(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "Movie.2010.1080p.zip")
	makeZip(t, arc, []zipEntry{
		{name: "Movie.2010.1080p/Movie.2010.1080p.mkv", body: "video"},
		{name: "Movie.2010.1080p/Subs/English.srt", body: "sub"},
		{name: `Movie.2010.1080p\Win.Path.mkv`, body: "win"},
		{name: "../escape.mkv", body: "x"},
		{name: "a/../../escape2.mkv", body: "x"},
		{name: "/abs.mkv", body: "x"},
		{name: "C:/drive.mkv", body: "x"},
		{name: "run.exe", body: "x"},
		{name: "readme.nfo", body: "x"},
		{name: "link.mkv", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777},
	})
	out := filepath.Join(dir, "dl", UnpackDir("ABCDEF0123456789"))
	names, err := Unzip(arc, out, UnpackLimit(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Movie.2010.1080p/Movie.2010.1080p.mkv", "Movie.2010.1080p/Subs/English.srt", "Movie.2010.1080p/Win.Path.mkv"}
	if !slices.Equal(names, want) {
		t.Fatalf("unpacked %v, want %v", names, want)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "Movie.2010.1080p", "Movie.2010.1080p.mkv")); string(b) != "video" {
		t.Fatal("video not unpacked")
	}
	for _, p := range []string{filepath.Join(dir, "escape.mkv"), filepath.Join(dir, "dl", "escape.mkv"), filepath.Join(dir, "escape2.mkv"), "/abs.mkv", filepath.Join(out, "link.mkv")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s was written", p)
		}
	}
	if !strings.HasPrefix(filepath.Base(out), ".roarr-unpacked-abcdef012345") {
		t.Fatalf("unpack folder %s", out)
	}
	// A second pass keeps what is there.
	if again, err := Unzip(arc, out, UnpackLimit(1<<20)); err != nil || len(again) != 3 {
		t.Fatalf("second pass: %v %v", again, err)
	}
}

func TestUnzipSizeLimit(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "bomb.zip")
	big := strings.Repeat("0", 1<<20) // compresses to almost nothing
	makeZip(t, arc, []zipEntry{{name: "a.mkv", body: "small"}, {name: "b.mkv", body: big}})
	out := filepath.Join(dir, "out")
	if _, err := Unzip(arc, out, 1000); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("got %v, want ErrArchiveTooLarge", err)
	}
	if _, err := os.Stat(filepath.Join(out, "a.mkv")); err == nil {
		t.Fatal("files of a refused archive stay")
	}
	// The header claims 10 bytes; the data is 1 MiB.
	liar := filepath.Join(dir, "liar.zip")
	makeZip(t, liar, []zipEntry{{name: "c.mkv", body: big, size: 10}})
	if _, err := Unzip(liar, filepath.Join(dir, "out2"), 1000); err == nil {
		t.Fatal("an entry larger than its header was unpacked")
	}
	if _, err := os.Stat(filepath.Join(dir, "out2", "c.mkv")); err == nil {
		t.Fatal("the lying entry was written")
	}
}

func TestUnzipRefusesLinkedFolder(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "a.zip")
	makeZip(t, arc, []zipEntry{{name: "a.mkv", body: "x"}})
	outside := filepath.Join(dir, "outside")
	_ = os.Mkdir(outside, 0o755)
	link := filepath.Join(dir, UnpackDir("h"))
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Unzip(arc, link, 1<<20); err == nil {
		t.Fatal("unpacked through a link")
	}
	if _, err := os.Stat(filepath.Join(outside, "a.mkv")); err == nil {
		t.Fatal("file written through a link")
	}
}

func TestArchiveKinds(t *testing.T) {
	for name, want := range map[string][2]bool{
		"x.zip": {true, false}, "x.ZIP.!qB": {true, false}, "x.rar": {false, true}, "x.r00": {false, true},
		"x.7z": {false, true}, "x.z01": {false, true}, "x.mkv": {false, false}, "x.r": {false, false},
	} {
		if got := [2]bool{IsArchive(name), IsUnsupportedArchive(name)}; got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}
