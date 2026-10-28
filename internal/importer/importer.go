package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/release"
)

// Config is where the library lives and how files get there.
type Config struct {
	TVRoot    string
	MovieRoot string
	Mode      Mode
}

// Request asks for one placement to be imported.
type Request struct {
	Title     Title
	Placement Placement
	Release   string // torrent / release name, for quality and history
	Hash      string // torrent info-hash
	// Replace lets the file take the place of what is already in the
	// library even when it is not better: the user picked this release
	// for these episodes.
	Replace bool
}

// Result is what an import did.
type Result struct {
	Path      string   `json:"path,omitempty"`
	Subtitles []string `json:"subtitles,omitempty"`
	Method    string   `json:"method,omitempty"` // hardlink | copy | move
	Replaced  []string `json:"replaced,omitempty"`
	Skipped   bool     `json:"skipped,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	// SubtitleErrors lists subtitles that could not be placed, by their
	// path in the torrent. The video is imported regardless.
	SubtitleErrors map[string]string `json:"subtitleErrors,omitempty"`
}

// Importer places download files into the library.
type Importer struct {
	cfg   Config
	store *Store
	hooks *Hooks
	mu    sync.Mutex // one import at a time: destinations are checked then written
}

// New returns an importer. hooks may be nil.
func New(cfg Config, store *Store, hooks *Hooks) *Importer {
	if hooks == nil {
		hooks = &Hooks{}
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeHardlink
	}
	return &Importer{cfg: cfg, store: store, hooks: hooks}
}

// Store returns the importer's store.
func (im *Importer) Store() *Store { return im.store }

// Config returns the importer's configuration.
func (im *Importer) Config() Config { return im.cfg }

// Destination returns the library path (with extension) a placement goes to.
func (im *Importer) Destination(t Title, p Placement) (string, error) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSuffix(p.Video.Rel, ".!qB")))
	if t.Kind == "movie" {
		if im.cfg.MovieRoot == "" {
			return "", errors.New("no movie library folder configured (MOVIES_ROOT)")
		}
		return MoviePath(im.cfg.MovieRoot, t.Title, t.Year) + ext, nil
	}
	if im.cfg.TVRoot == "" {
		return "", errors.New("no TV library folder configured (TV_ROOT)")
	}
	if len(p.Episodes) == 0 {
		return "", errors.New("the file is not mapped to an episode")
	}
	return EpisodePath(im.cfg.TVRoot, t.Title, t.Year, p.Season, p.Episodes) + ext, nil
}

// QualityRank orders releases by resolution, then source, then
// proper/repack. -1 means the name says nothing about quality (a file
// already renamed to "Show - S01E02.mkv").
func QualityRank(name string) int {
	i := release.Parse(name)
	src := map[release.Source]int{
		release.SourceCam: 1, release.SourceTS: 2, release.SourceScreen: 3, release.SourceDVD: 4, release.SourceHDTV: 5,
		release.SourceWEBRip: 6, release.SourceWEB: 7, release.SourceWEBDL: 7, release.SourceBluRay: 8, release.SourceRemux: 9,
	}[i.Source]
	if i.Resolution == 0 && src == 0 {
		return -1
	}
	r := i.Resolution*100 + src*10
	if i.Proper || i.Repack {
		r++
	}
	return r
}

// Existing lists the library videos that already hold any of the given
// episodes (TV) or the movie: what roarr imported and still exists, plus
// anything else in the target folder (placed by hand, Sonarr or Radarr).
func (im *Importer) Existing(t Title, season int, episodes []int) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			if _, err := os.Stat(p); err == nil {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	key := t.Key()
	if t.Kind == "movie" {
		for _, f := range im.store.Files(key) {
			add(f.Path)
		}
		if im.cfg.MovieRoot != "" {
			dir := filepath.Dir(MoviePath(im.cfg.MovieRoot, t.Title, t.Year))
			for _, p := range videosIn(dir) {
				if Classify(filepath.Base(p)) == ClassVideo {
					add(p)
				}
			}
		}
		return out
	}
	for _, e := range episodes {
		for _, f := range im.store.ForEpisode(key, season, e) {
			add(f.Path)
		}
	}
	if im.cfg.TVRoot != "" {
		dir := filepath.Dir(EpisodePath(im.cfg.TVRoot, t.Title, t.Year, season, []int{1}))
		want := map[int]bool{}
		for _, e := range episodes {
			want[e] = true
		}
		for _, p := range videosIn(dir) {
			i := release.Parse(filepath.Base(p))
			if len(i.Seasons) == 0 || i.Seasons[0] != season {
				continue
			}
			for _, e := range i.Episodes {
				if want[e] {
					add(p)
					break
				}
			}
		}
	}
	return out
}

func videosIn(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.Type().IsRegular() && IsVideo(e.Name()) && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// Import places one video and its subtitles. A file that would not improve
// on what is already there comes back as Skipped with a reason, not an
// error. Errors wrapping ErrRetryLater mean a before-import hook asked to
// try again later.
func (im *Importer) Import(ctx context.Context, req Request) (Result, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	t, p := req.Title, req.Placement
	dest, err := im.Destination(t, p)
	if err != nil {
		return Result{}, err
	}
	src := p.Video.Path
	si, err := os.Stat(src)
	if err != nil {
		return Result{}, fmt.Errorf("download file is not readable from roarr (check QBIT_PATH_MAP): %w", err)
	}

	newRank := QualityRank(filepath.Base(p.Video.Rel))
	if newRank < 0 {
		newRank = QualityRank(req.Release)
	}
	existing := im.Existing(t, p.Season, p.Episodes)
	var replace []string
	for _, old := range existing {
		if oi, err := os.Stat(old); err == nil && os.SameFile(si, oi) {
			return Result{Path: old, Skipped: true, Reason: "already in the library"}, nil
		}
		if t.Kind == "tv" {
			if extra := extraEpisodes(old, p.Season, p.Episodes); extra != "" {
				return Result{Skipped: true, Reason: fmt.Sprintf("%s also holds %s; not replacing it", filepath.Base(old), extra)}, nil
			}
		}
		oldRank := im.rankOf(old)
		switch {
		case req.Replace:
		case oldRank < 0:
			return Result{Skipped: true, Reason: filepath.Base(old) + " is already there and its quality is unknown"}, nil
		case newRank <= oldRank:
			return Result{Skipped: true, Reason: filepath.Base(old) + " is already there and is as good or better"}, nil
		}
		replace = append(replace, old)
	}

	cand := Candidate{Title: t, Season: p.Season, Episodes: p.Episodes, Source: src, Dest: dest, Release: req.Release, Hash: req.Hash, Replacing: replace}
	if err := im.hooks.runBefore(ctx, cand); err != nil {
		return Result{}, err
	}

	// Move the videos being replaced out of the way first (one may have
	// the very name the new file needs), and put them back if placing
	// fails. Their sidecars (subtitles someone may have added by hand)
	// are not touched until the new video is in place.
	type moved struct{ from, to string }
	var aside []moved
	restore := func() {
		for _, m := range aside {
			_ = os.Rename(m.to, m.from)
		}
	}
	for _, old := range replace {
		bak := filepath.Join(filepath.Dir(old), ".roarr-replaced-"+filepath.Base(old))
		if err := os.Rename(old, bak); err != nil {
			restore()
			return Result{}, fmt.Errorf("moving %s aside: %w", old, err)
		}
		aside = append(aside, moved{old, bak})
	}
	method, err := Transfer(src, dest, im.cfg.Mode)
	if err != nil {
		restore()
		return Result{}, err
	}
	for _, m := range aside {
		if err := os.Remove(m.to); err != nil {
			slog.Warn("import: could not delete replaced file", "path", m.to, "err", err)
		}
	}
	res := Result{Path: dest, Method: method, Replaced: replace}

	destBase := strings.TrimSuffix(dest, filepath.Ext(dest))
	for _, old := range replace {
		// Sidecars follow only a video that holds the same episodes: E01's
		// subtitle must not become the subtitle of a new E01-E02 file.
		if t.Kind == "movie" || im.sameEpisodes(old, p.Season, p.Episodes) {
			keepSidecars(old, destBase)
		}
	}
	res.Subtitles, res.SubtitleErrors = im.placeSubtitles(destBase, p.Subtitles, map[string]bool{})

	_ = im.store.Forget(replace...)
	rec := FileRecord{TitleKey: t.Key(), Season: p.Season, Episodes: p.Episodes, Path: dest, Subtitles: res.Subtitles, Size: si.Size(),
		Release: req.Release, Hash: req.Hash, Source: src, Rank: newRank, Method: method, ImportedAt: time.Now().UTC()}
	if t.Kind == "movie" {
		rec.Season, rec.Episodes = 0, nil
	}
	if err := im.store.Put(rec); err != nil {
		slog.Warn("import: could not save library.json", "err", err)
	}
	im.hooks.runAfter(ctx, Event{Candidate: cand, Path: dest, Subtitles: res.Subtitles, Method: method, Replaced: replace, At: rec.ImportedAt})
	return res, nil
}

// ErrVideoReplaced means the library video is no longer the one the
// subtitles were downloaded with: another release took its place.
var ErrVideoReplaced = errors.New("the video was replaced by another release")

// AddSubtitles places subtitles next to a video roarr already imported:
// subtitles whose download finished after the video, or whose first
// transfer failed. hash and release identify the import the subtitles
// belong to; when the library record at video is from another import,
// nothing is placed and ErrVideoReplaced is returned. It returns the
// placed paths and, by subtitle path in the torrent, the ones that failed.
func (im *Importer) AddSubtitles(video, hash, release string, subs []SubtitleRef) ([]string, map[string]string, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	if _, err := os.Stat(video); err != nil {
		return nil, nil, err
	}
	im.store.mu.RLock()
	r, ok := im.store.d.Files[video]
	var rec FileRecord
	if ok {
		rec = *r
	}
	im.store.mu.RUnlock()
	if !ok || !strings.EqualFold(rec.Hash, hash) || rec.Release != release {
		return nil, nil, ErrVideoReplaced
	}
	ours := map[string]bool{}
	for _, p := range rec.Subtitles {
		ours[p] = true
	}
	placed, errs := im.placeSubtitles(strings.TrimSuffix(video, filepath.Ext(video)), subs, ours)
	if len(placed) > 0 && rec.Path != "" {
		rec.Subtitles = append(rec.Subtitles, placed...)
		if err := im.store.Put(rec); err != nil {
			slog.Warn("import: could not save library.json", "err", err)
		}
	}
	return placed, errs, nil
}

// placeSubtitles puts subtitles next to the video at destBase. ours holds
// subtitles this import already placed: a second English subtitle gets
// "<base>.en.2.srt" instead of overwriting the first. An older subtitle
// with the same name is replaced only once the new one is fully in place.
func (im *Importer) placeSubtitles(destBase string, subs []SubtitleRef, ours map[string]bool) ([]string, map[string]string) {
	var placed []string
	errs := map[string]string{}
	for _, s := range subs {
		ext := filepath.Ext(s.File.Rel)
		name := SubtitleName(destBase, s.Info, ext)
		for n := 2; ours[name]; n++ {
			name = SubtitleName(destBase, s.Info, "."+fmt.Sprint(n)+ext)
		}
		var err error
		if _, statErr := os.Stat(name); statErr == nil {
			tmp := filepath.Join(filepath.Dir(name), ".roarr-new-"+filepath.Base(name))
			_ = os.Remove(tmp)
			if _, err = Transfer(s.File.Path, tmp, im.cfg.Mode); err == nil {
				if err = os.Rename(tmp, name); err != nil {
					_ = os.Remove(tmp)
				}
			}
		} else {
			_, err = Transfer(s.File.Path, name, im.cfg.Mode)
		}
		if err != nil {
			slog.Warn("import: subtitle not placed", "file", s.File.Rel, "err", err)
			errs[s.File.Rel] = err.Error()
			continue
		}
		ours[name] = true
		placed = append(placed, name)
	}
	return placed, errs
}

// sameEpisodes reports whether the library video old holds exactly the
// given episodes, by roarr's record or else by its name.
func (im *Importer) sameEpisodes(old string, season int, eps []int) bool {
	im.store.mu.RLock()
	r, ok := im.store.d.Files[old]
	var oldSeason int
	var oldEps []int
	if ok {
		oldSeason, oldEps = r.Season, append([]int(nil), r.Episodes...)
	}
	im.store.mu.RUnlock()
	if !ok {
		i := release.Parse(filepath.Base(old))
		if len(i.Seasons) == 0 {
			return false
		}
		oldSeason, oldEps = i.Seasons[0], i.Episodes
	}
	if oldSeason != season {
		return false
	}
	a, b := append([]int(nil), oldEps...), append([]int(nil), eps...)
	sort.Ints(a)
	sort.Ints(b)
	return slices.Equal(a, b)
}

// keepSidecars moves the sidecars of a replaced video (subtitles added by
// hand, for example) to the new video's name, so they stay with it. A
// sidecar whose new name is taken stays where it is.
func keepSidecars(oldVideo, newBase string) {
	oldStem := strings.TrimSuffix(filepath.Base(oldVideo), filepath.Ext(oldVideo))
	for _, sc := range sidecars(oldVideo) {
		to := newBase + strings.TrimPrefix(filepath.Base(sc), oldStem)
		if to == sc {
			continue
		}
		if _, err := os.Stat(to); err == nil {
			continue
		}
		if err := os.Rename(sc, to); err != nil {
			slog.Warn("import: could not move sidecar to the new video", "path", sc, "err", err)
		}
	}
}

// rankOf is the quality rank of a library file: what roarr recorded when
// it imported it, else what its name says.
func (im *Importer) rankOf(p string) int {
	im.store.mu.RLock()
	r, ok := im.store.d.Files[p]
	im.store.mu.RUnlock()
	if ok {
		return r.Rank
	}
	return QualityRank(filepath.Base(p))
}

// extraEpisodes names episodes an existing file holds beyond the new
// placement ("S01E03"), or "" when it holds nothing more.
func extraEpisodes(old string, season int, eps []int) string {
	i := release.Parse(filepath.Base(old))
	if len(i.Seasons) == 0 || len(i.Episodes) <= 1 {
		return ""
	}
	have := map[int]bool{}
	for _, e := range eps {
		have[e] = true
	}
	var more []int
	for _, e := range i.Episodes {
		if !have[e] {
			more = append(more, e)
		}
	}
	if len(more) == 0 {
		return ""
	}
	return EpisodeToken(i.Seasons[0], more)
}

// sidecars lists subtitle (and other) files next to a video that share its
// name: "Show - S01E02.en.srt" for "Show - S01E02.mkv".
func sidecars(video string) []string {
	dir := filepath.Dir(video)
	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.Type().IsRegular() && n != filepath.Base(video) && strings.HasPrefix(n, stem+".") && !IsVideo(n) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}
