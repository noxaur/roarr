package transcode

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplaceOS(oldpath, newpath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL), errors.Is(err, unix.ENOTSUP):
		// Old kernel, or a filesystem that does not know RENAME_NOREPLACE.
		return errNoReplaceUnsupported
	}
	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
}
