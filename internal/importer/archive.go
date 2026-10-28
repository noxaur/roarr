package importer

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// UnpackDir is the folder, next to an archive, that roarr unpacks the
// archive of a torrent into. The info-hash in the name keeps the torrent
// itself from placing files (or links) there.
func UnpackDir(hash string) string {
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return ".roarr-unpacked-" + strings.ToLower(hash)
}

// IsArchive reports whether roarr can unpack a download file: ZIP only.
func IsArchive(name string) bool {
	return strings.EqualFold(path.Ext(strings.TrimSuffix(name, ".!qB")), ".zip")
}

// IsUnsupportedArchive reports whether a download file is an archive that
// roarr cannot unpack (RAR, 7-Zip, split ZIP).
func IsUnsupportedArchive(name string) bool {
	ext := strings.ToLower(path.Ext(strings.TrimSuffix(name, ".!qB")))
	switch {
	case ext == ".rar" || ext == ".7z":
		return true
	case len(ext) == 4 && (ext[1] == 'r' || ext[1] == 'z') && ext[2] >= '0' && ext[2] <= '9' && ext[3] >= '0' && ext[3] <= '9':
		return true // .r00, .z01: volumes
	}
	return false
}

// ErrArchiveTooLarge means an archive unpacks to more than the limit.
var ErrArchiveTooLarge = errors.New("the archive unpacks to more than the size limit")

// maxArchiveEntries bounds the number of files roarr unpacks from one
// archive.
const maxArchiveEntries = 5000

// UnpackLimit is the most roarr unpacks from an archive of the given size:
// twice its size plus 64 MiB. Video does not compress, so a real release
// stays far below it; a ZIP bomb does not.
func UnpackLimit(archiveSize int64) int64 { return 2*archiveSize + 64<<20 }

// Unzip unpacks the videos and subtitles of a ZIP archive into dir and
// returns their paths relative to dir, with forward slashes. Entries that
// are not videos or subtitles are skipped, and so are links and names that
// would leave dir (absolute, "..", drive letters). When the unpacked size
// would pass maxBytes, Unzip stops, removes what it unpacked and returns
// ErrArchiveTooLarge. A file that is already unpacked with the right size
// (a second pass after a restart) is kept.
func Unzip(archive, dir string, maxBytes int64) ([]string, error) {
	if st, err := os.Lstat(dir); err == nil && !st.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if len(r.File) > maxArchiveEntries {
		return nil, fmt.Errorf("the archive has %d files; roarr unpacks at most %d", len(r.File), maxArchiveEntries)
	}
	var out, written []string
	fail := func(err error) ([]string, error) {
		for _, p := range written {
			_ = os.Remove(p)
		}
		return nil, err
	}
	left := maxBytes
	for _, f := range r.File {
		name, ok := entryName(f)
		if !ok {
			continue
		}
		if f.UncompressedSize64 > uint64(left) {
			return fail(ErrArchiveTooLarge)
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if st, err := os.Lstat(dst); err == nil && st.Mode().IsRegular() && uint64(st.Size()) == f.UncompressedSize64 {
			left -= st.Size()
			out = append(out, name)
			continue
		}
		n, err := unzipFile(f, dir, dst, left)
		if err != nil {
			return fail(err)
		}
		left -= n
		written = append(written, dst)
		out = append(out, name)
	}
	return out, nil
}

// entryName is the safe, slash-separated name of a ZIP entry roarr
// unpacks, or false to skip the entry.
func entryName(f *zip.File) (string, bool) {
	if !f.Mode().IsRegular() { // folders, links, devices
		return "", false
	}
	name := path.Clean(strings.ReplaceAll(f.Name, `\`, "/"))
	if !filepath.IsLocal(filepath.FromSlash(name)) || strings.Contains(name, ":") || strings.HasPrefix(name, "/") {
		return "", false
	}
	if !IsVideo(name) && !IsSubtitle(name) {
		return "", false
	}
	return name, true
}

// unzipFile writes one entry to dst (inside dir) through a temporary file,
// reading at most limit bytes. It returns the size written.
func unzipFile(f *zip.File, dir, dst string, limit int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	if err := InsideRoot(dir, dst); err != nil { // no link out of dir
		return 0, err
	}
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".roarr-unzip-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	// Read one byte more than allowed: the header size can lie.
	n, err := io.Copy(tmp, io.LimitReader(rc, limit+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return 0, fmt.Errorf("unpacking %s: %w", f.Name, err)
	case n > limit:
		return 0, ErrArchiveTooLarge
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return 0, err
	}
	return n, nil
}
