//go:build !linux

package transcode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNoReplaceIsUnsupportedHere(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(a, b); !errors.Is(err, errNoReplaceUnsupported) {
		t.Fatalf("err = %v, want errNoReplaceUnsupported", err)
	}
	if _, err := os.Stat(a); err != nil {
		t.Error("source moved although unsupported")
	}
}
