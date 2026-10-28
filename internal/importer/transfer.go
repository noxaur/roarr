package importer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Mode is how a file gets from the download folder into the library.
type Mode string

const (
	// ModeHardlink links the file (no extra space, the torrent keeps
	// seeding) and copies when linking is impossible: different
	// filesystems or Docker volumes, or a filesystem without links.
	ModeHardlink Mode = "hardlink"
	// ModeCopy always copies; the torrent keeps seeding.
	ModeCopy Mode = "copy"
	// ModeMove renames the file into the library (copy + delete across
	// filesystems). The torrent can no longer seed that file.
	ModeMove Mode = "move"
)

// ParseMode reads a mode setting; anything unknown is hardlink.
func ParseMode(s string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(s))) {
	case ModeCopy:
		return ModeCopy
	case ModeMove:
		return ModeMove
	}
	return ModeHardlink
}

// linkFile is os.Link; tests replace it to simulate filesystems that
// refuse links.
var linkFile = os.Link

// ErrExists means the destination is already taken by another file.
var ErrExists = errors.New("destination already exists")

// Transfer places src at dst using mode and returns the method that was
// actually used ("hardlink", "copy" or "move"). Parent folders are created.
// dst must not exist, unless it already is the same file as src (a second
// import of the same hardlink), which counts as done.
func Transfer(src, dst string, mode Mode) (string, error) {
	si, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !si.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", src)
	}
	if di, err := os.Stat(dst); err == nil {
		if os.SameFile(si, di) {
			return "hardlink", nil
		}
		return "", fmt.Errorf("%w: %s", ErrExists, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	switch mode {
	case ModeMove:
		if err := os.Rename(src, dst); err == nil {
			return "move", nil
		}
		if err := copyFile(src, dst, si); err != nil {
			return "", err
		}
		if err := os.Remove(src); err != nil {
			return "copy", fmt.Errorf("copied, but could not remove the source: %w", err)
		}
		return "move", nil
	case ModeCopy:
		return "copy", copyFile(src, dst, si)
	default:
		if err := linkFile(src, dst); err == nil {
			return "hardlink", nil
		}
		// EXDEV (another filesystem/volume), EPERM/ENOTSUP (no links on
		// this filesystem): a copy always works.
		return "copy", copyFile(src, dst, si)
	}
}

// copyFile copies through a temporary file in the destination folder and
// renames it into place, so Jellyfin never sees a half-written file.
func copyFile(src, dst string, si os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".roarr-*")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmp.Name(), si.Mode().Perm()|0o644)
	_ = os.Chtimes(tmp.Name(), si.ModTime(), si.ModTime())
	if _, err := os.Stat(dst); err == nil {
		os.Remove(tmp.Name())
		done = true
		return fmt.Errorf("%w: %s", ErrExists, dst)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return err
	}
	done = true
	return nil
}
