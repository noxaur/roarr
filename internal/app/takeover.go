package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/seerr"
)

// Taking over the library from Sonarr and Radarr. One pass copies every
// series and movie they manage into library.json (ids, names, the folder
// as it is, monitoring, and a record of each file so roarr can upgrade and
// delete it), adds title folders on disk that neither knows (identified
// through Seerr), and then switches roarr to run the library itself
// (IMPORT_MODE=native, standalone). Nothing on disk moves.

// TakeoverState is the progress of a takeover.
type TakeoverState struct {
	Running  bool      `json:"running"`
	Started  time.Time `json:"started,omitzero"`
	Finished time.Time `json:"finished,omitzero"`
	Step     string    `json:"step,omitempty"`
	Shows    int       `json:"shows"`
	Movies   int       `json:"movies"`
	Files    int       `json:"files"`
	// Skipped lists titles that were not taken over, with the reason.
	Skipped []string `json:"skipped,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// TakeoverCheck is one thing roarr needs before it can run the library.
type TakeoverCheck struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Need  bool   `json:"need"` // false: a warning, the takeover can run anyway
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// TakeoverSource is what Sonarr or Radarr would hand over.
type TakeoverSource struct {
	Titles int           `json:"titles"`
	Roots  []TakeoverDir `json:"roots"`
	Error  string        `json:"error,omitempty"`
}

// TakeoverDir is an *arr root folder and where roarr sees it.
type TakeoverDir struct {
	Path  string `json:"path"`
	Local string `json:"local,omitempty"`
	OK    bool   `json:"ok"`
}

// TakeoverPlan is the takeover page: what would move and what is missing.
type TakeoverPlan struct {
	Standalone bool            `json:"standalone"`
	Sonarr     *TakeoverSource `json:"sonarr,omitempty"`
	Radarr     *TakeoverSource `json:"radarr,omitempty"`
	Checks     []TakeoverCheck `json:"checks"`
	Ready      bool            `json:"ready"`
	State      TakeoverState   `json:"state"`
}

// arrSources returns Sonarr and Radarr to read from: the running clients,
// or (once roarr stopped using them) the configured or discovered ones.
func (a *App) arrSources() (*arr.Sonarr, *arr.Radarr) {
	c, s := a.C(), a.Settings.Get()
	son, rad := c.Sonarr, c.Radarr
	if son == nil && s.Sonarr.URL != "" {
		son = arr.NewSonarr(s.Sonarr.URL, s.Sonarr.APIKey)
	}
	if rad == nil && s.Radarr.URL != "" {
		rad = arr.NewRadarr(s.Radarr.URL, s.Radarr.APIKey)
	}
	if (son == nil || rad == nil) && c.Prowlarr != nil {
		s.Import.Standalone, s.QBit.Password = false, ""
		d, _ := discoverOnce(&Clients{Prowlarr: c.Prowlarr, Sonarr: son, Radarr: rad}, s)
		son, rad = d.Sonarr, d.Radarr
	}
	return son, rad
}

// TakeoverPlan checks what a takeover needs and counts what it would move.
func (a *App) TakeoverPlan(ctx context.Context) TakeoverPlan {
	s := a.Settings.Get()
	c := a.C()
	p := TakeoverPlan{Standalone: s.Import.OwnsLibrary(), State: a.Native.takeoverState()}
	son, rad := a.arrSources()
	maps := a.arrPathMaps()
	if son != nil {
		p.Sonarr = &TakeoverSource{}
		if series, err := son.AllSeries(ctx); err != nil {
			p.Sonarr.Error = err.Error()
		} else {
			p.Sonarr.Titles = len(series)
		}
		p.Sonarr.Roots = rootsView(ctx, son.Client, s.Import.TVRoot, maps)
	}
	if rad != nil {
		p.Radarr = &TakeoverSource{}
		if movies, err := rad.Movies(ctx); err != nil {
			p.Radarr.Error = err.Error()
		} else {
			p.Radarr.Titles = len(movies)
		}
		p.Radarr.Roots = rootsView(ctx, rad.Client, s.Import.MovieRoot, maps)
	}
	add := func(id string, ok, need bool, label, hint string) {
		ck := TakeoverCheck{ID: id, OK: ok, Need: need, Label: label}
		if !ok {
			ck.Hint = hint
		}
		p.Checks = append(p.Checks, ck)
	}
	qok := false
	if c.QBit != nil {
		_, err := c.QBit.Version(ctx)
		qok = err == nil
	}
	add("qbit", qok, true, "qBittorrent is connected", "roarr downloads through qBittorrent: set its URL and password under Connections.")
	add("seerr", c.Seerr != nil, true, "Seerr is connected", "Without Sonarr and Radarr, show and movie details come from TMDB through Seerr.")
	wantTV := p.Sonarr != nil && p.Sonarr.Titles > 0
	wantMovies := p.Radarr != nil && p.Radarr.Titles > 0
	tvOK := writableDir(s.Import.TVRoot)
	add("tvRoot", tvOK, wantTV || p.Sonarr == nil, "The TV library folder is writable", "Set the TV library folder to where Sonarr keeps shows, as roarr sees it.")
	movieOK := writableDir(s.Import.MovieRoot)
	add("movieRoot", movieOK, wantMovies || p.Radarr == nil, "The movie library folder is writable", "Set the movie library folder to where Radarr keeps movies, as roarr sees it.")
	if p.Sonarr != nil {
		add("sonarrRoots", allOK(p.Sonarr.Roots), wantTV, "roarr sees every Sonarr root folder inside the TV library folder",
			"Sonarr's folders must be inside the TV library folder. If roarr mounts them elsewhere, add the mapping to the AV1 path map (Sonarr/Radarr path = roarr path).")
	}
	if p.Radarr != nil {
		add("radarrRoots", allOK(p.Radarr.Roots), wantMovies, "roarr sees every Radarr root folder inside the movie library folder",
			"Radarr's folders must be inside the movie library folder. If roarr mounts them elsewhere, add the mapping to the AV1 path map.")
	}
	dl, dlErr := a.downloadDir(ctx)
	add("downloads", dlErr == nil, true, "roarr sees qBittorrent's download folder", fmt.Sprint(dlErr))
	if dlErr == nil && s.Import.Transfer == "hardlink" {
		root := s.Import.TVRoot
		if !tvOK {
			root = s.Import.MovieRoot
		}
		err := canHardlink(dl, root)
		add("hardlink", err == nil, false, "Downloads can be hardlinked into the library", "Imports copy instead, which takes time and space: "+fmt.Sprint(err)+". Put downloads and the library on one filesystem and one Docker mount.")
	}
	add("jellyfin", c.Jellyfin != nil && s.Jellyfin.APIKey != "", false, "Jellyfin is connected", "Without Jellyfin, new files show up at its next library scan.")
	if a.Settings.Locked()["IMPORT_MODE"] && !s.Import.Native() {
		add("mode", false, true, "Import mode can be changed", "IMPORT_MODE=arr is set in the environment; remove it or set it to native.")
	}
	p.Ready = true
	for _, ck := range p.Checks {
		if ck.Need && !ck.OK {
			p.Ready = false
		}
	}
	return p
}

// allOK reports whether roarr sees every root folder; no root folders (or
// none that could be listed) is not OK.
func allOK(ds []TakeoverDir) bool {
	if len(ds) == 0 {
		return false
	}
	for _, d := range ds {
		if !d.OK {
			return false
		}
	}
	return true
}

func rootsView(ctx context.Context, c *arr.Client, local string, maps []importer.PathMap) []TakeoverDir {
	roots, err := c.RootFolders(ctx)
	if err != nil {
		return nil
	}
	out := []TakeoverDir{}
	for _, r := range roots {
		p, ok := mapArrPath(r.Path, []string{r.Path}, local, maps)
		out = append(out, TakeoverDir{Path: r.Path, Local: p, OK: ok && local != ""})
	}
	return out
}

// arrPathMaps are the maps from Sonarr/Radarr paths to roarr's.
func (a *App) arrPathMaps() []importer.PathMap {
	s := a.Settings.Get()
	return []importer.PathMap{pathMap("TRANSCODE_PATH_MAP", s.Transcode.PathMap), pathMap("VERIFY_PATH_MAP", s.Verify.PathMap)}
}

// mapArrPath finds where roarr sees a Sonarr/Radarr path inside its local
// library root: the same path, a path map, or the path relative to one of
// the *arr's root folders. It reports false when no candidate exists on
// disk, and then returns the first candidate inside the root, if any.
func mapArrPath(p string, roots []string, local string, maps []importer.PathMap) (string, bool) {
	if local == "" {
		return "", false
	}
	p = filepath.Clean(filepath.FromSlash(p))
	cands := []string{p}
	for _, m := range maps {
		if q, ok := m.Match(p); ok {
			cands = append(cands, filepath.Clean(filepath.FromSlash(q)))
		}
	}
	for _, r := range roots {
		if rel, ok := inside(filepath.Clean(filepath.FromSlash(r)), p); ok {
			cands = append(cands, filepath.Join(local, rel))
		}
	}
	fallback := ""
	for _, c := range cands {
		if _, ok := inside(local, c); !ok {
			continue
		}
		if dirExists(c) {
			return c, true
		}
		if fallback == "" {
			fallback = c
		}
	}
	return fallback, false
}

// inside reports whether p is root or below it, and p relative to root.
func inside(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func writableDir(p string) bool {
	if p == "" || !dirExists(p) {
		return false
	}
	f, err := os.CreateTemp(p, ".roarr-write-*")
	if err != nil {
		return false
	}
	f.Close()
	_ = os.Remove(f.Name())
	return true
}

// downloadDir is qBittorrent's folder for roarr's downloads, as roarr sees
// it.
func (a *App) downloadDir(ctx context.Context) (string, error) {
	c, s := a.C(), a.Settings.Get().Import
	if c.QBit == nil {
		return "", errors.New("qBittorrent is not connected")
	}
	p := s.SavePath
	if p == "" {
		var err error
		if p, err = c.QBit.DefaultSavePath(ctx); err != nil {
			return "", err
		}
	}
	pm := pathMap("QBIT_PATH_MAP", s.PathMap)
	local := filepath.FromSlash(normClientPath(pm.Map(p)))
	if !dirExists(local) {
		return "", fmt.Errorf("qBittorrent saves to %s, which roarr cannot see at %s: set the path map (qBittorrent path = roarr path)", p, local)
	}
	return local, nil
}

// canHardlink tries one link from the download folder into the library.
func canHardlink(from, to string) error {
	if to == "" {
		return errors.New("no library folder")
	}
	f, err := os.CreateTemp(from, ".roarr-link-*")
	if err != nil {
		return err
	}
	f.Close()
	defer os.Remove(f.Name())
	dst := filepath.Join(to, filepath.Base(f.Name()))
	if err := os.Link(f.Name(), dst); err != nil {
		return err
	}
	return os.Remove(dst)
}

// TakeoverState is the progress of the latest takeover.
func (a *App) TakeoverState() TakeoverState { return a.Native.takeoverState() }

func (n *Native) takeoverState() TakeoverState {
	n.mu.Lock()
	defer n.mu.Unlock()
	st := n.takeover
	st.Skipped = append([]string(nil), st.Skipped...)
	return st
}

func (n *Native) setTakeover(fn func(*TakeoverState)) {
	n.mu.Lock()
	fn(&n.takeover)
	n.mu.Unlock()
}

// StartTakeover runs a takeover in the background. It refuses while one
// runs or while a needed check fails.
func (a *App) StartTakeover(ctx context.Context) (TakeoverPlan, error) {
	p := a.TakeoverPlan(ctx)
	if p.State.Running {
		return p, errors.New("a takeover is already running")
	}
	if !p.Ready {
		for _, ck := range p.Checks {
			if ck.Need && !ck.OK {
				return p, errors.New(ck.Hint)
			}
		}
	}
	n := a.Native
	n.setTakeover(func(st *TakeoverState) { *st = TakeoverState{Running: true, Started: time.Now(), Step: "Starting"} })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		err := a.runTakeover(ctx)
		n.setTakeover(func(st *TakeoverState) {
			st.Running, st.Finished, st.Step = false, time.Now(), ""
			if err != nil {
				st.Error = err.Error()
			}
		})
		st := n.takeoverState()
		slog.Info("takeover done", "shows", st.Shows, "movies", st.Movies, "files", st.Files, "skipped", len(st.Skipped), "err", err)
	}()
	return a.TakeoverPlan(ctx), nil
}

func (a *App) runTakeover(ctx context.Context) error {
	n := a.Native
	s := a.Settings.Get().Import
	son, rad := a.arrSources()
	maps := a.arrPathMaps()
	skip := func(what string, err error) {
		slog.Warn("takeover: skipped", "title", what, "err", err)
		n.setTakeover(func(st *TakeoverState) { st.Skipped = append(st.Skipped, what+": "+err.Error()) })
	}
	if son != nil {
		if err := a.takeOverSonarr(ctx, son, s.TVRoot, maps, skip); err != nil {
			return fmt.Errorf("sonarr: %w", err)
		}
	}
	if rad != nil {
		if err := a.takeOverRadarr(ctx, rad, s.MovieRoot, maps, skip); err != nil {
			return fmt.Errorf("radarr: %w", err)
		}
	}
	n.setTakeover(func(st *TakeoverState) { st.Step = "Looking for folders on disk" })
	a.takeOverFolders(ctx, skip)
	if _, err := a.Settings.Update(func(cs *config.Settings) {
		cs.Import.Mode, cs.Import.Standalone = "native", true
	}); err != nil {
		return err
	}
	n.forgetShows()
	return nil
}

func (a *App) takeOverSonarr(ctx context.Context, son *arr.Sonarr, root string, maps []importer.PathMap, skip func(string, error)) error {
	n := a.Native
	series, err := son.AllSeries(ctx)
	if err != nil {
		return err
	}
	roots, err := rootPaths(ctx, son.Client, len(series))
	if err != nil {
		return err
	}
	for i, sr := range series {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n.setTakeover(func(st *TakeoverState) { st.Step = fmt.Sprintf("Sonarr: %s (%d of %d)", sr.Title, i+1, len(series)) })
		files, err := a.takeOverShow(ctx, son, sr, roots, root, maps)
		if err != nil {
			skip(sr.Title, err)
			continue
		}
		n.setTakeover(func(st *TakeoverState) { st.Shows++; st.Files += files })
	}
	return nil
}

func (a *App) takeOverShow(ctx context.Context, son *arr.Sonarr, sr arr.Series, roots []string, root string, maps []importer.PathMap) (int, error) {
	n := a.Native
	if sr.TmdbID == 0 && sr.TvdbID == 0 {
		return 0, errors.New("no TMDB or TVDB id")
	}
	eps, err := son.Episodes(ctx, sr.ID)
	if err != nil {
		return 0, err
	}
	files, err := son.EpisodeFiles(ctx, sr.ID)
	if err != nil {
		return 0, err
	}
	folder, seen := mapArrPath(sr.Path, roots, root, maps)
	if folder == "" || folder == root || !seen && len(files) > 0 {
		return 0, fmt.Errorf("roarr cannot see its folder %s inside %s", sr.Path, root)
	}
	t := importer.Title{Kind: "tv", Title: sr.Title, Year: sr.Year, TmdbID: sr.TmdbID, TvdbID: sr.TvdbID, ImdbID: sr.ImdbID,
		Anime: sr.SeriesType == "anime", Poster: arr.Poster(sr.Images), MetaAt: time.Now().UTC().Format(time.RFC3339)}
	t.Folder = folderName(root, folder, t)
	for _, alt := range sr.AlternateTitles {
		t.Aliases = append(t.Aliases, alt.Title)
	}
	var monitor, ignore []int
	byFile := map[int][]arr.Episode{}
	for _, e := range eps {
		ep := importer.Episode{Season: e.SeasonNumber, Number: e.EpisodeNumber, Absolute: e.AbsoluteEpisodeNumber, Title: e.Title}
		if !e.AirDateUTC.IsZero() {
			ep.AirDate = e.AirDateUTC.Local().Format(time.DateOnly)
		}
		t.Episodes = append(t.Episodes, ep)
		id := importer.EpisodeID(e.SeasonNumber, e.EpisodeNumber)
		if e.Monitored {
			monitor = append(monitor, id)
		} else {
			ignore = append(ignore, id)
		}
		if e.EpisodeFileID > 0 {
			byFile[e.EpisodeFileID] = append(byFile[e.EpisodeFileID], e)
		}
	}
	sort.Slice(t.Episodes, func(i, k int) bool {
		if t.Episodes[i].Season != t.Episodes[k].Season {
			return t.Episodes[i].Season < t.Episodes[k].Season
		}
		return t.Episodes[i].Number < t.Episodes[k].Number
	})
	var recs []importer.FileRecord
	for _, f := range files {
		cov := byFile[f.ID]
		if len(cov) == 0 {
			continue
		}
		p, ok := a.arrFile(folder, sr.Path, f, roots, root, maps)
		if !ok {
			continue
		}
		var nums []int
		for _, e := range cov {
			nums = append(nums, e.EpisodeNumber)
		}
		sort.Ints(nums)
		recs = append(recs, adoptedRecord(t, f, p, cov[0].SeasonNumber, nums))
	}
	return len(recs), n.adopt(t, !sr.Monitored, monitor, ignore, recs)
}

func (a *App) takeOverRadarr(ctx context.Context, rad *arr.Radarr, root string, maps []importer.PathMap, skip func(string, error)) error {
	n := a.Native
	movies, err := rad.Movies(ctx)
	if err != nil {
		return err
	}
	roots, err := rootPaths(ctx, rad.Client, len(movies))
	if err != nil {
		return err
	}
	for i, m := range movies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n.setTakeover(func(st *TakeoverState) { st.Step = fmt.Sprintf("Radarr: %s (%d of %d)", m.Title, i+1, len(movies)) })
		if m.TmdbID == 0 {
			skip(m.Title, errors.New("no TMDB id"))
			continue
		}
		folder, seen := mapArrPath(m.Path, roots, root, maps)
		if folder == "" || folder == root || !seen && m.MovieFile != nil {
			skip(m.Title, fmt.Errorf("roarr cannot see its folder %s inside %s", m.Path, root))
			continue
		}
		t := importer.Title{Kind: "movie", Title: m.Title, Year: m.Year, TmdbID: m.TmdbID, ImdbID: m.ImdbID, Poster: arr.Poster(m.Images)}
		t.Folder = folderName(root, folder, t)
		for _, alt := range m.AlternateTitles {
			t.Aliases = append(t.Aliases, alt.Title)
		}
		var recs []importer.FileRecord
		if f := m.MovieFile; f != nil {
			if p, ok := a.arrFile(folder, m.Path, *f, roots, root, maps); ok {
				recs = append(recs, adoptedRecord(t, *f, p, 0, nil))
			}
		}
		if err := n.adopt(t, !m.Monitored, nil, nil, recs); err != nil {
			skip(m.Title, err)
			continue
		}
		n.setTakeover(func(st *TakeoverState) { st.Movies++; st.Files += len(recs) })
	}
	return nil
}

// rootPaths lists an *arr's root folders. Titles without a root folder
// cannot be placed, so the takeover stops (and roarr keeps using the *arr)
// rather than finish without them.
func rootPaths(ctx context.Context, c *arr.Client, titles int) ([]string, error) {
	rs, err := c.RootFolders(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing root folders: %w", err)
	}
	if len(rs) == 0 && titles > 0 {
		return nil, errors.New("it has titles but no root folders")
	}
	var out []string
	for _, r := range rs {
		out = append(out, r.Path)
	}
	return out, nil
}

// arrFile is where roarr sees a Sonarr/Radarr file: in the title folder
// under its relative path, or its own path mapped.
func (a *App) arrFile(folder, arrFolder string, f arr.MediaFile, roots []string, root string, maps []importer.PathMap) (string, bool) {
	rel := f.RelativePath
	if f.Path != "" {
		if r, ok := inside(filepath.Clean(filepath.FromSlash(arrFolder)), filepath.Clean(filepath.FromSlash(f.Path))); ok {
			rel = r
		}
	}
	if rel != "" {
		if p := filepath.Join(folder, filepath.FromSlash(rel)); fileExists(p) {
			return p, true
		}
	}
	if f.Path != "" {
		for _, m := range maps {
			if p, ok := m.Match(f.Path); ok && fileExists(p) {
				return p, true
			}
		}
	}
	return "", false
}

// folderName is the title's folder relative to the library root, or ""
// when it is the name roarr would give it anyway.
func folderName(root, folder string, t importer.Title) string {
	rel, ok := inside(root, folder)
	if !ok || rel == "." || rel == importer.TitleWithYear(t.Title, t.Year) {
		return ""
	}
	return rel
}

// adoptedRecord records a file Sonarr or Radarr placed. Its quality comes
// from the release it was grabbed as (sceneName) or the *arr's quality, so
// roarr upgrades it only to something better.
func adoptedRecord(t importer.Title, f arr.MediaFile, p string, season int, eps []int) importer.FileRecord {
	name := f.SceneName
	if name == "" {
		name = strings.TrimSpace(filepath.Base(f.RelativePath) + " " + f.Quality.Quality.Name)
	}
	at, _ := time.Parse(time.RFC3339, f.DateAdded)
	size := f.Size
	if st, err := os.Stat(p); err == nil {
		size = st.Size()
	}
	return importer.FileRecord{TitleKey: t.Key(), Season: season, Episodes: eps, Path: p, Size: size, Release: name,
		Rank: importer.QualityRank(name), Method: "adopted", ImportedAt: at}
}

// adopt stores a title taken over and its file records. A title roarr
// already tracks keeps its own monitoring, and files roarr placed keep
// their records.
func (n *Native) adopt(t importer.Title, unmonitored bool, monitor, ignore []int, recs []importer.FileRecord) error {
	_, had := n.store.Title(t.Key())
	if err := n.store.PutTitle(t); err != nil {
		return err
	}
	if !had {
		if _, err := n.store.UpdateTitle(t.Key(), func(x *importer.Title) {
			x.Unmonitored, x.Monitor, x.Ignore = unmonitored, monitor, ignore
		}); err != nil {
			return err
		}
	}
	var fresh []importer.FileRecord
	for _, r := range recs {
		if _, ok := n.store.Record(r.Path); !ok {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	return n.store.Put(fresh...)
}

// idTag reads the ids some tools put in folder names: "[tmdbid-123]",
// "{tmdb-123}", "[tvdbid-456]", "{imdb-tt789}".
var idTag = regexp.MustCompile(`(?i)[\[{](tmdb|tvdb|imdb)(?:id)?[-=]((?:tt)?\d+)[\]}]`)

// folderYear is "Name (2019)".
var folderYear = regexp.MustCompile(`^(.*?)\s*\((\d{4})\)\s*$`)

// takeOverFolders adds title folders in the library roots that no tracked
// title owns. Each is identified through Seerr: by an id in its name, or
// by a search whose top hit has the same name and year. Folders roarr
// cannot identify are listed as skipped.
func (a *App) takeOverFolders(ctx context.Context, skip func(string, error)) {
	n, c, s := a.Native, a.C(), a.Settings.Get().Import
	im := n.Importer()
	owned := map[string]bool{}
	for _, t := range n.store.Titles() {
		owned[filepath.Clean(im.TitleFolder(t))] = true
	}
	for _, kind := range []string{"tv", "movie"} {
		root := s.TVRoot
		if kind == "movie" {
			root = s.MovieRoot
		}
		ents, _ := os.ReadDir(root)
		for _, e := range ents {
			dir := filepath.Join(root, e.Name())
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || owned[dir] {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			if c.Seerr == nil {
				skip(e.Name(), errors.New("Seerr is needed to identify a folder"))
				continue
			}
			t, err := a.identifyFolder(ctx, c.Seerr, kind, e.Name())
			if err != nil {
				skip(e.Name(), err)
				continue
			}
			t.Folder = folderName(root, dir, t)
			if _, ok := n.store.Title(t.Key()); ok {
				skip(e.Name(), errors.New("another folder already holds "+importer.TitleWithYear(t.Title, t.Year)))
				continue
			}
			var recs []importer.FileRecord
			for _, f := range im.OnDisk(t) {
				if f.Record == nil {
					name := filepath.Base(f.Path)
					recs = append(recs, importer.FileRecord{TitleKey: t.Key(), Season: f.Season, Episodes: f.Episodes, Path: f.Path, Size: f.Size,
						Release: name, Rank: importer.QualityRank(name), Method: "adopted", ImportedAt: time.Now().UTC()})
				}
			}
			if kind == "movie" && len(recs) > 1 {
				sort.Slice(recs, func(i, k int) bool { return recs[i].Size > recs[k].Size })
				recs = recs[:1] // the largest video is the movie; the rest are extras
			}
			if err := n.adopt(t, false, nil, nil, recs); err != nil {
				skip(e.Name(), err)
				continue
			}
			n.setTakeover(func(st *TakeoverState) {
				if kind == "tv" {
					st.Shows++
				} else {
					st.Movies++
				}
				st.Files += len(recs)
			})
		}
	}
}

// identifyFolder finds the TMDB title of a library folder.
func (a *App) identifyFolder(ctx context.Context, sc *seerr.Client, kind, name string) (importer.Title, error) {
	tmdbID := 0
	for _, m := range idTag.FindAllStringSubmatch(name, -1) {
		if strings.EqualFold(m[1], "tmdb") {
			tmdbID, _ = strconv.Atoi(m[2])
		}
	}
	clean := strings.TrimSpace(idTag.ReplaceAllString(name, ""))
	title, year := clean, 0
	if m := folderYear.FindStringSubmatch(clean); m != nil {
		title = m[1]
		year, _ = strconv.Atoi(m[2])
	}
	if tmdbID == 0 {
		res, err := sc.Search(ctx, title)
		if err != nil {
			return importer.Title{}, err
		}
		// Without a year in the folder name, the name alone must be
		// unambiguous: "The Office" is two shows.
		var hits []int
		for _, r := range res {
			if r.MediaType == kind && release.Normalize(r.DisplayTitle()) == release.Normalize(title) && (year == 0 || r.Year() == year) {
				hits = append(hits, r.ID)
			}
		}
		switch {
		case len(hits) == 0:
			return importer.Title{}, errors.New("no TMDB title has this name and year")
		case len(hits) > 1 && year == 0:
			return importer.Title{}, errors.New("several TMDB titles have this name; add the year to the folder name, as in \"Name (2005)\"")
		}
		tmdbID = hits[0]
	}
	if kind == "movie" {
		m, err := sc.Movie(ctx, tmdbID)
		if err != nil {
			return importer.Title{}, err
		}
		imdb := m.ImdbID
		if imdb == "" {
			imdb = m.ExternalIDs.ImdbID
		}
		return importer.Title{Kind: "movie", Title: m.Title, Year: m.Year(), TmdbID: m.ID, ImdbID: imdb, Poster: seerr.Poster(m.PosterPath, "w342")}, nil
	}
	tv, err := sc.TV(ctx, tmdbID)
	if err != nil {
		return importer.Title{}, err
	}
	var seasons []int
	for _, s := range tv.Seasons {
		seasons = append(seasons, s.SeasonNumber)
	}
	eps, err := seerrEpisodes(ctx, sc, tv.ID, seasons)
	t := importer.Title{Kind: "tv", Title: tv.Name, Year: tv.Year(), TmdbID: tv.ID, TvdbID: tv.ExternalIDs.TvdbID, ImdbID: tv.ExternalIDs.ImdbID,
		Anime: tv.IsAnime(), Episodes: eps, Partial: err != nil, Poster: seerr.Poster(tv.PosterPath, "w342")}
	if err == nil {
		t.MetaAt = time.Now().UTC().Format(time.RFC3339)
	}
	return t, nil
}
