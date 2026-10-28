package importer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/noxaur/roarr/internal/release"
)

// LibraryFile is one video of a title that is on disk.
type LibraryFile struct {
	Path     string
	Size     int64
	Season   int
	Episodes []int
	// Record is roarr's import record; nil for a video that roarr did not
	// import (placed by hand, by Sonarr or by Radarr). Only recorded
	// videos can be deleted through roarr.
	Record *FileRecord
}

// OnDisk lists the videos of a title that exist: the files roarr imported
// and, in the title's library folder, other videos (for shows: videos in
// the season folders whose names give an episode).
func (im *Importer) OnDisk(t Title) []LibraryFile {
	seen := map[string]bool{}
	var out []LibraryFile
	for _, r := range im.store.Files(t.Key()) {
		st, err := os.Stat(r.Path)
		if err != nil {
			continue
		}
		rec := r
		seen[r.Path] = true
		out = append(out, LibraryFile{Path: r.Path, Size: st.Size(), Season: r.Season, Episodes: r.Episodes, Record: &rec})
	}
	dir := im.TitleFolder(t)
	if dir == "" {
		return out
	}
	add := func(p string, season int, eps []int) {
		if seen[p] {
			return
		}
		st, err := os.Stat(p)
		if err != nil {
			return
		}
		seen[p] = true
		out = append(out, LibraryFile{Path: p, Size: st.Size(), Season: season, Episodes: eps})
	}
	if t.Kind == "movie" {
		for _, p := range videosIn(dir) {
			if Classify(filepath.Base(p)) == ClassVideo {
				add(p, 0, nil)
			}
		}
		return out
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "Season ") {
			continue
		}
		for _, p := range videosIn(filepath.Join(dir, e.Name())) {
			i := release.Parse(filepath.Base(p))
			if len(i.Seasons) == 0 || len(i.Episodes) == 0 {
				continue
			}
			add(p, i.Seasons[0], i.Episodes)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
