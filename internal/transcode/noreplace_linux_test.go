package transcode

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func writeSized(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRenameNoReplaceMovesIntoEmptyPath(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	writeSized(t, a, 10)
	if err := renameNoReplace(a, b); errors.Is(err, errNoReplaceUnsupported) {
		t.Skip("filesystem has no RENAME_NOREPLACE")
	} else if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) || size(t, b) != 10 {
		t.Error("file not moved")
	}
}

func TestRenameNoReplaceRefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	writeSized(t, a, 10)
	writeSized(t, b, 20)
	err := renameNoReplace(a, b)
	if errors.Is(err, errNoReplaceUnsupported) {
		t.Skip("filesystem has no RENAME_NOREPLACE")
	}
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	if size(t, a) != 10 || size(t, b) != 20 {
		t.Error("a file was changed")
	}
}

// With the real syscall: a file that appears between the existence check and
// the move is kept, and so is the backup.
func TestSwapWithoutHardLinksNoReplaceKeepsRacingFile(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.linkFn = noHardLinkOnResult(path)
	m.noReplaceFn = func(a, b string) error {
		if a == tempPath(path) {
			writeSized(t, b, 777)
		}
		return renameNoReplace(a, b)
	}
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Failed || size(t, path) != 777 {
		t.Fatalf("state %s (%s), size %d", j.State, j.Message, size(t, path))
	}
	if size(t, backupPath(path)) != 1000 {
		t.Error("previous file not kept as backup")
	}
}

// Restoring must not overwrite a file that appeared after the backup was made.
func TestRestoreNoReplaceKeepsRacingFile(t *testing.T) {
	dir := t.TempDir()
	path, bak := filepath.Join(dir, "m.mkv"), filepath.Join(dir, "bak")
	writeSized(t, bak, 1000)
	writeSized(t, path, 777)
	link := func(a, b string) error { return &os.LinkError{Op: "link", Old: a, New: b, Err: syscall.ENOTSUP} }
	err := restore(renameNoReplace, link, bak, path, "reason")
	if err == nil || size(t, path) != 777 || size(t, bak) != 1000 {
		t.Fatalf("err = %v, path %d, backup %d", err, size(t, path), size(t, bak))
	}
}
