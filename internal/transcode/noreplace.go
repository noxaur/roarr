package transcode

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// errNoReplaceUnsupported means this platform or filesystem has no atomic
// rename that refuses to overwrite. Nothing was moved.
var errNoReplaceUnsupported = errors.New("atomic rename without overwrite is not supported here")

// renameNoReplace moves oldpath to newpath in one step and fails with an
// error matching fs.ErrExist when newpath exists, even if it appears at the
// last moment. It returns errNoReplaceUnsupported, with nothing moved, when
// the platform or filesystem cannot do that.
func renameNoReplace(oldpath, newpath string) error { return renameNoReplaceOS(oldpath, newpath) }

// moveNoReplace puts src at dst without ever overwriting dst: an atomic
// no-replace rename where available, else a hard link plus removing src.
// When neither works, src is left in place and the error says so.
func moveNoReplace(noReplace, link func(a, b string) error, src, dst string) error {
	err := noReplace(src, dst)
	if !errors.Is(err, errNoReplaceUnsupported) {
		return err
	}
	if lerr := link(src, dst); lerr != nil {
		if errors.Is(lerr, fs.ErrExist) {
			return lerr
		}
		return fmt.Errorf("%w; hard links are not available either (%v)", errNoReplaceUnsupported, lerr)
	}
	_ = os.Remove(src)
	return nil
}
