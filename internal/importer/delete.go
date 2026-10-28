package importer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideLibrary means a path is not inside the library folder of its
// kind. roarr never deletes such a path.
var ErrOutsideLibrary = errors.New("path is outside the library folder")

// InsideRoot checks that p is a path below root, also after symbolic links
// in its folders are resolved. p itself may be a link: removing a link
// does not touch its target. root itself is never inside.
func InsideRoot(root, p string) error {
	if root == "" {
		return errors.New("no library folder is configured")
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(p) {
		return fmt.Errorf("%w: %s (relative path)", ErrOutsideLibrary, p)
	}
	root, p = filepath.Clean(root), filepath.Clean(p)
	if !below(root, p) {
		return fmt.Errorf("%w: %s", ErrOutsideLibrary, p)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("library folder: %w", err)
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	if realDir != realRoot && !below(realRoot, realDir) {
		return fmt.Errorf("%w: %s (through a link)", ErrOutsideLibrary, p)
	}
	return nil
}

// below reports whether p is strictly inside dir (both clean).
func below(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// rootOf is the library folder of a title key ("tv:..." or "movie:...").
func (im *Importer) rootOf(titleKey string) string {
	if strings.HasPrefix(titleKey, "movie:") {
		return im.cfg.MovieRoot
	}
	return im.cfg.TVRoot
}

// DeleteResult says what Delete removed.
type DeleteResult struct {
	// Deleted are the videos removed from the library (or already gone).
	Deleted []FileRecord `json:"deleted"`
	// Subtitles are the subtitle files removed with them.
	Subtitles []string `json:"subtitles,omitempty"`
	// Refused are paths that were not deleted, with the reason.
	Refused map[string]string `json:"refused,omitempty"`
	// Freed is the size of the deleted videos.
	Freed int64 `json:"freed"`
}

// Delete removes library videos that roarr imported, and the subtitles
// roarr placed with them. A path is deleted only when library.json has a
// record of it and it is inside the TV or movie folder of its title.
// Sidecars that roarr did not place (subtitles added by hand) stay.
// Folders that are empty afterwards are removed, up to the library folder.
func (im *Importer) Delete(paths []string) DeleteResult {
	im.mu.Lock()
	defer im.mu.Unlock()
	res := DeleteResult{Refused: map[string]string{}}
	var gone []string
	dirs := map[string]string{} // folder -> its library root
	for _, p := range paths {
		rec, ok := im.store.Record(p)
		if !ok {
			res.Refused[p] = "roarr did not import this file"
			continue
		}
		root := im.rootOf(rec.TitleKey)
		if err := InsideRoot(root, p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			res.Refused[p] = err.Error()
			continue
		}
		if err := removeFile(p); err != nil {
			res.Refused[p] = err.Error()
			continue
		}
		for _, s := range rec.Subtitles {
			if InsideRoot(root, s) != nil {
				continue
			}
			if err := removeFile(s); err == nil {
				res.Subtitles = append(res.Subtitles, s)
			}
		}
		gone = append(gone, p)
		res.Deleted = append(res.Deleted, rec)
		res.Freed += rec.Size
		dirs[filepath.Dir(p)] = root
	}
	if len(gone) > 0 {
		_ = im.store.Forget(gone...)
	}
	for dir, root := range dirs {
		pruneEmpty(dir, root)
	}
	return res
}

// removeFile deletes a regular file or a link. A missing file is not an
// error: it is already gone.
func removeFile(p string) error {
	st, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case st.IsDir():
		return fmt.Errorf("%s is a folder", p)
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// pruneEmpty removes dir and its parents while they are empty and below
// root.
func pruneEmpty(dir, root string) {
	root = filepath.Clean(root)
	for d := filepath.Clean(dir); below(root, d); d = filepath.Dir(d) {
		if os.Remove(d) != nil { // fails unless the folder is empty
			return
		}
	}
}

// TitleFolder is the library folder of a title: root/Title (Year).
func (im *Importer) TitleFolder(t Title) string {
	root := im.cfg.TVRoot
	if t.Kind == "movie" {
		root = im.cfg.MovieRoot
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, TitleWithYear(t.Title, t.Year))
}
