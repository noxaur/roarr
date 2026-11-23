package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/seerr"
)

// The native library: with IMPORT_MODE=native and no Sonarr (shows) or
// Radarr (movies), the library list, the title pages and the management
// actions come from roarr's own library.json, with metadata from TMDB
// through Seerr. The API shapes are the same as in arr mode:
//
//   - A title's library id is its TMDB id (without one: tvdbIDBase plus
//     its TVDB id).
//   - An episode's id is season*10000 + number (importer.EpisodeID).
//   - Monitoring: a title is monitored unless switched off. Its monitored
//     episodes are the ones grabbed or monitored on purpose, and every
//     episode that airs after roarr started to track the title.

// ErrNotFound means a title or file is not known.
var ErrNotFound = errors.New("not found")

// tvdbIDBase offsets the library id of a title that has no TMDB id.
const tvdbIDBase = 1 << 30

// metaMaxAge is how long a stored episode list is used before it is loaded
// again.
const metaMaxAge = 6 * time.Hour

// nativeLib reports whether the library of kind ("tv" or "movie") comes from
// roarr's library.json.
func (a *App) nativeLib(kind string) bool {
	if a.Native == nil || a.Settings == nil || !a.Settings.Get().Import.Native() {
		return false
	}
	c := a.C()
	if kind == "movie" {
		return c.Radarr == nil
	}
	return c.Sonarr == nil
}

// NativeID is the library id of a title in the native library, 0 when it
// has neither a TMDB nor a TVDB id.
func NativeID(t importer.Title) int {
	switch {
	case t.TmdbID > 0:
		return t.TmdbID
	case t.TvdbID > 0:
		return tvdbIDBase + t.TvdbID
	}
	return 0
}

// titleByID finds a tracked title by its library id.
func (n *Native) titleByID(kind string, id int) (importer.Title, error) {
	if id > 0 {
		for _, t := range n.store.Titles() {
			if t.Kind == kind && NativeID(t) == id {
				return t, nil
			}
		}
	}
	return importer.Title{}, fmt.Errorf("%s %d is not in roarr's library: %w", kind, id, ErrNotFound)
}

func today() string { return time.Now().Format(time.DateOnly) }

// aired reports whether an air date is today or earlier. An unknown date
// counts as not aired.
func aired(date string) bool { return date != "" && date <= today() }

// episodeMonitored applies the monitoring rules of a title to one episode.
func episodeMonitored(t importer.Title, season, number int, airDate string) bool {
	if t.Unmonitored {
		return false
	}
	id := importer.EpisodeID(season, number)
	if slices.Contains(t.Ignore, id) {
		return false
	}
	if slices.Contains(t.Monitor, id) {
		return true
	}
	return season > 0 && t.Added != "" && airDate != "" && airDate > t.Added
}

// queued returns the episodes (season, number) of a title that a native
// job is still downloading or importing.
func (n *Native) queued(key string) map[[2]int]bool {
	out := map[[2]int]bool{}
	for _, j := range n.Jobs() {
		if j.TitleKey != key || j.final() {
			continue
		}
		if j.Kind == "movie" {
			out[[2]int{}] = true
			continue
		}
		for _, w := range j.Wanted {
			out[w] = true
		}
		for _, f := range j.Files {
			if !f.Skipped && !f.Imported {
				for _, e := range f.Placement.Episodes {
					out[[2]int{f.Placement.Season, e}] = true
				}
			}
		}
	}
	return out
}

// nativeFileInfo describes a library video for the title pages.
func nativeFileInfo(f importer.LibraryFile) *FileInfo {
	name := filepath.Base(f.Path)
	fi := &FileInfo{Path: f.Path, Size: f.Size}
	if f.Record != nil {
		if f.Record.Release != "" {
			name = f.Record.Release
		}
		fi.Added = f.Record.ImportedAt.Format(time.RFC3339)
	}
	i := release.Parse(name)
	if i.Resolution == 0 && f.Record != nil && f.Record.Source != "" {
		i = release.Parse(filepath.Base(f.Record.Source))
	}
	q := string(i.Source)
	if i.Resolution > 0 {
		q = strings.TrimPrefix(q+"-"+strconv.Itoa(i.Resolution)+"p", "-")
	}
	fi.Quality, fi.Resolution, fi.Group, fi.Codec, fi.HDR, fi.Audio = q, i.Resolution, i.Group, i.Codec, i.HDR, i.Audio
	return fi
}

// showState is a native show's episodes with their disk, queue and
// monitoring state.
type showState struct {
	seasons []SeasonDetail
	have    int
	total   int
	size    int64
}

func (n *Native) showState(t importer.Title) showState {
	files := map[[2]int]importer.LibraryFile{}
	var st showState
	for _, f := range n.Importer().OnDisk(t) {
		st.size += f.Size
		for _, e := range f.Episodes {
			files[[2]int{f.Season, e}] = f
		}
	}
	queued := n.queued(t.Key())
	type ep struct {
		importer.Episode
		known bool
	}
	eps := map[[2]int]ep{}
	for _, e := range t.Episodes {
		eps[[2]int{e.Season, e.Number}] = ep{e, true}
	}
	for se := range files {
		if _, ok := eps[se]; !ok {
			eps[se] = ep{Episode: importer.Episode{Season: se[0], Number: se[1]}}
		}
	}
	bySeason := map[int][]EpisodeDetail{}
	for se, e := range eps {
		f, has := files[se]
		title := e.Title
		if title == "" {
			title = fmt.Sprintf("Episode %d", e.Number)
		}
		d := EpisodeDetail{ID: importer.EpisodeID(se[0], se[1]), Season: se[0], Number: se[1], Absolute: e.Absolute, Title: title, AirDate: e.AirDate,
			Aired: aired(e.AirDate) || has, HasFile: has, Monitored: episodeMonitored(t, se[0], se[1], e.AirDate), Queued: !has && queued[se]}
		if has {
			d.File = nativeFileInfo(f)
			st.have++
		}
		if has || (d.Monitored && d.Aired) {
			st.total++
		}
		bySeason[se[0]] = append(bySeason[se[0]], d)
	}
	for num, list := range bySeason {
		sort.Slice(list, func(i, j int) bool { return list[i].Number < list[j].Number })
		sd := SeasonDetail{Number: num, Episodes: list}
		for _, e := range list {
			sd.Monitored = sd.Monitored || e.Monitored
		}
		st.seasons = append(st.seasons, sd)
	}
	sortSeasons(st.seasons)
	return st
}

// movieFile returns the largest video of a native movie that is on disk.
func (n *Native) movieFile(t importer.Title) (importer.LibraryFile, bool) {
	var best importer.LibraryFile
	ok := false
	for _, f := range n.Importer().OnDisk(t) {
		if !ok || f.Size > best.Size || (best.Record == nil && f.Record != nil) {
			best, ok = f, true
		}
	}
	return best, ok
}

// libraryItem is a native title as a library list entry.
func (n *Native) libraryItem(t importer.Title) LibraryItem {
	id := NativeID(t)
	it := LibraryItem{Key: t.Kind + ":" + strconv.Itoa(id), Kind: t.Kind, ID: id, Title: t.Title, Year: t.Year, Poster: t.Poster,
		Monitored: !t.Unmonitored, TvdbID: t.TvdbID, TmdbID: t.TmdbID, ImdbID: t.ImdbID, Aliases: t.Aliases, Anime: t.Anime}
	if t.Kind == "movie" {
		it.Total = 1
		if f, ok := n.movieFile(t); ok {
			it.Have, it.SizeOnDisk = 1, f.Size
		}
		return it
	}
	st := n.showState(t)
	it.Have, it.Total, it.SizeOnDisk = st.have, st.total, st.size
	return it
}

// LibraryApp is who keeps titles of kind: "roarr" (the native library),
// "sonarr" or "radarr".
func (a *App) LibraryApp(kind string) string {
	switch {
	case a.nativeLib(kind):
		return "roarr"
	case kind == "movie":
		return "radarr"
	}
	return "sonarr"
}

// nativeTitleID is the library id of a title roarr's own library tracks,
// or 0.
func (a *App) nativeTitleID(kind string, tvdbID, tmdbID int) int {
	if !a.nativeLib(kind) || a.Native == nil || (tvdbID == 0 && tmdbID == 0) {
		return 0
	}
	for _, t := range a.Native.store.Titles() {
		if t.Kind == kind && (tmdbID != 0 && t.TmdbID == tmdbID || tvdbID != 0 && t.TvdbID == tvdbID) {
			return NativeID(t)
		}
	}
	return 0
}

// libraryItems lists the native titles of kind for the library list.
func (n *Native) libraryItems(kind string) []LibraryItem {
	var out []LibraryItem
	for _, t := range n.store.Titles() {
		if t.Kind == kind && NativeID(t) != 0 {
			out = append(out, n.libraryItem(t))
		}
	}
	return out
}

// nativeSeriesDetail is the show page of a native library show.
func (a *App) nativeSeriesDetail(ctx context.Context, id int, fresh bool) (*SeriesDetail, error) {
	n := a.Native
	t, err := n.titleByID("tv", id)
	if err != nil {
		return nil, err
	}
	c := a.C()
	var tv *seerr.TV
	if c.Seerr != nil && t.TmdbID != 0 {
		if x, err := c.Seerr.TV(ctx, t.TmdbID); err == nil {
			tv = &x
		} else {
			slog.Warn("native library: TMDB lookup failed; using stored metadata", "title", t.Title, "err", err)
		}
	}
	if tv != nil && (fresh || len(t.Episodes) == 0 || t.Partial || metaStale(t.MetaAt)) {
		t = a.refreshShowMeta(ctx, t, tv)
	}
	st := n.showState(t)
	it := n.libraryItem(t)
	d := &SeriesDetail{LibraryItem: it, InLibrary: true, Seasons: st.seasons, Path: n.Importer().TitleFolder(t)}
	d.Have, d.Total, d.SizeOnDisk = st.have, st.total, st.size
	if tv != nil {
		d.Overview, d.Fanart, d.Status = tv.Overview, seerr.Poster(tv.BackdropPath, "w1280"), tv.Status
		if d.Poster == "" {
			d.Poster = seerr.Poster(tv.PosterPath, "w342")
		}
		for _, g := range tv.Genres {
			d.Genres = append(d.Genres, g.Name)
		}
	}
	if d.Seasons == nil {
		d.Seasons = []SeasonDetail{}
	}
	a.detailMu.Lock()
	a.details["tv:"+strconv.Itoa(id)] = cachedDetail{at: time.Now(), series: d}
	a.detailMu.Unlock()
	return d, nil
}

// keepNumbering guards a show's episode numbers when TMDB loads a new
// list. TMDB can number a show differently from the scene/TVDB numbers its
// files carry (a list taken over from Sonarr, some anime, re-split
// seasons). When the new list lacks an episode that has a file, the stored
// numbers stay, and only episodes that air after the last stored one are
// added from TMDB; otherwise the files would go to other episodes and the
// missing search would grab the same episodes again under the new numbers.
func (n *Native) keepNumbering(t importer.Title, fresh []importer.Episode) []importer.Episode {
	if len(t.Episodes) == 0 {
		return fresh
	}
	have := map[[2]int]bool{}
	for _, e := range fresh {
		have[[2]int{e.Season, e.Number}] = true
	}
	lost := 0
	for _, f := range n.store.Files(t.Key()) {
		for _, e := range f.Episodes {
			if !have[[2]int{f.Season, e}] {
				lost++
			}
		}
	}
	if lost == 0 {
		return fresh
	}
	out := append([]importer.Episode(nil), t.Episodes...)
	last, known := "", map[[2]int]bool{}
	for _, e := range out {
		known[[2]int{e.Season, e.Number}] = true
		last = max(last, e.AirDate)
	}
	added := 0
	for _, e := range fresh {
		if last != "" && e.AirDate > last && !known[[2]int{e.Season, e.Number}] {
			out = append(out, e)
			added++
		}
	}
	slog.Info("native library: TMDB numbers this show differently; kept its episode numbers", "title", t.Title, "filesNotInTMDB", lost, "newEpisodes", added)
	return out
}

func metaStale(at string) bool {
	t, err := time.Parse(time.RFC3339, at)
	return err != nil || time.Since(t) > metaMaxAge
}

// refreshShowMeta loads a native show's episode list from TMDB again, so
// new episodes and air dates are known. The name and year stay.
func (a *App) refreshShowMeta(ctx context.Context, t importer.Title, tv *seerr.TV) importer.Title {
	var seasons []int
	for _, s := range tv.Seasons {
		seasons = append(seasons, s.SeasonNumber)
	}
	eps, err := seerrEpisodes(ctx, a.C().Seerr, t.TmdbID, seasons)
	if err != nil {
		slog.Warn("native library: episode list not refreshed", "title", t.Title, "err", err)
		return t
	}
	eps = a.Native.keepNumbering(t, eps)
	poster := seerr.Poster(tv.PosterPath, "w342")
	_, err = a.Native.store.UpdateTitle(t.Key(), func(x *importer.Title) {
		x.Episodes, x.Partial, x.MetaAt = eps, false, time.Now().UTC().Format(time.RFC3339)
		if x.TvdbID == 0 {
			x.TvdbID = tv.ExternalIDs.TvdbID
		}
		if poster != "" {
			x.Poster = poster
		}
		t = *x
	})
	if err != nil {
		slog.Warn("native library: could not save library.json", "err", err)
	}
	return t
}

// nativeMovieDetail is the movie page of a native library movie.
func (a *App) nativeMovieDetail(ctx context.Context, id int) (*MovieDetail, error) {
	n := a.Native
	t, err := n.titleByID("movie", id)
	if err != nil {
		return nil, err
	}
	c := a.C()
	d := &MovieDetail{}
	if c.Seerr != nil && t.TmdbID != 0 {
		if m, err := c.Seerr.Movie(ctx, t.TmdbID); err == nil {
			d = a.movieFromSeerr(ctx, m)
			if t.Poster == "" && d.Poster != "" {
				_, _ = n.store.UpdateTitle(t.Key(), func(x *importer.Title) { x.Poster = d.Poster })
			}
		} else {
			slog.Warn("native library: TMDB lookup failed; using stored metadata", "title", t.Title, "err", err)
		}
	}
	it := n.libraryItem(t)
	if it.Poster == "" {
		it.Poster = d.Poster
	}
	it.Status, it.Network = d.Status, d.Network
	d.LibraryItem, d.InLibrary, d.Path = it, true, n.Importer().TitleFolder(t)
	d.HasFile, d.File = false, nil
	if f, ok := n.movieFile(t); ok {
		d.HasFile, d.File = true, nativeFileInfo(f)
	}
	d.Queued = !d.HasFile && n.queued(t.Key())[[2]int{}]
	return d, nil
}

// ---------------------------------------------------------------------------
// Management actions

// nativeEpisodes applies an episode action to a native library show.
func (a *App) nativeEpisodes(ctx context.Context, id int, act EpisodeAction) (ActionResult, error) {
	n := a.Native
	t, err := n.titleByID("tv", id)
	if err != nil {
		return ActionResult{}, err
	}
	ids := positive(act.EpisodeIDs)
	if len(ids) == 0 {
		return ActionResult{}, errors.New("no episodes given")
	}
	defer a.touchedNative("tv", id)
	want := set(ids)
	switch act.Action {
	case "delete":
		var paths []string
		var notOurs int
		for _, f := range n.Importer().OnDisk(t) {
			hit := false
			for _, e := range f.Episodes {
				hit = hit || want[importer.EpisodeID(f.Season, e)]
			}
			switch {
			case !hit:
			case f.Record == nil:
				notOurs++
			default:
				paths = append(paths, f.Path)
			}
		}
		if len(paths) == 0 {
			if notOurs > 0 {
				return ActionResult{}, fmt.Errorf("roarr did not import %s, so it does not delete them", plural(notOurs, "file"))
			}
			return ActionResult{OK: true, Message: "Nothing to delete; none of those are on disk."}, nil
		}
		res, err := n.deleteFiles(ctx, t, paths, act.RemoveTorrent)
		if err != nil {
			return ActionResult{}, err
		}
		if notOurs > 0 {
			res.Message += fmt.Sprintf("; %s not imported by roarr stayed", plural(notOurs, "file"))
		}
		return res, nil
	case "monitor", "unmonitor":
		on := act.Action == "monitor"
		_, err := n.store.UpdateTitle(t.Key(), func(x *importer.Title) {
			for _, id := range ids {
				if on {
					x.Monitor, x.Ignore = addInt(x.Monitor, id), removeInt(x.Ignore, id)
				} else {
					x.Monitor, x.Ignore = removeInt(x.Monitor, id), addInt(x.Ignore, id)
				}
			}
			if on {
				x.Unmonitored = false
			}
		})
		if err != nil {
			return ActionResult{}, err
		}
		verb := "Monitoring"
		if !on {
			verb = "Stopped monitoring"
		}
		return ActionResult{OK: true, Count: len(ids), Message: fmt.Sprintf("%s %s", verb, plural(len(ids), "episode"))}, nil
	case "search":
		req := AutoRequest{Kind: "tv", SeriesID: id, TvdbID: t.TvdbID, TmdbID: t.TmdbID, EpisodeIDs: ids}
		if !n.startSearch(t.Key(), func(ctx context.Context) { a.logAuto(ctx, t, req) }) {
			return ActionResult{OK: true, Message: "roarr is already searching for " + t.Title}, nil
		}
		return ActionResult{OK: true, Count: len(ids), Message: "roarr is searching for " + plural(len(ids), "episode")}, nil
	}
	return ActionResult{}, fmt.Errorf("unknown action %q", act.Action)
}

// nativePatch changes the monitoring of a native library title.
func (a *App) nativePatch(kind string, id int, p TitlePatch) error {
	n := a.Native
	t, err := n.titleByID(kind, id)
	if err != nil {
		return err
	}
	if p.ProfileID != nil {
		return errors.New("quality profiles are a Sonarr/Radarr feature; native mode uses the release preferences in Settings")
	}
	if p.Monitored == nil {
		return nil
	}
	defer a.touchedNative(kind, id)
	on := *p.Monitored
	if p.Season == nil || kind == "movie" {
		_, err = n.store.UpdateTitle(t.Key(), func(x *importer.Title) { x.Unmonitored = !on })
		return err
	}
	season := *p.Season
	var ids []int
	for _, s := range n.showState(t).seasons {
		if s.Number != season {
			continue
		}
		for _, e := range s.Episodes {
			ids = append(ids, e.ID)
		}
	}
	_, err = n.store.UpdateTitle(t.Key(), func(x *importer.Title) {
		for _, id := range ids {
			if on {
				x.Monitor, x.Ignore = addInt(x.Monitor, id), removeInt(x.Ignore, id)
			} else {
				x.Monitor, x.Ignore = removeInt(x.Monitor, id), addInt(x.Ignore, id)
			}
		}
		if on {
			x.Unmonitored = false
		}
	})
	return err
}

// nativeCommand runs "refresh" (metadata and a Jellyfin rescan) or
// "search" (a search for what is missing) for a native library title.
func (a *App) nativeCommand(ctx context.Context, kind string, id int, name string) (ActionResult, error) {
	n := a.Native
	t, err := n.titleByID(kind, id)
	if err != nil {
		return ActionResult{}, err
	}
	defer a.touchedNative(kind, id)
	switch name {
	case "refresh":
		if kind == "tv" {
			if _, err := a.nativeSeriesDetail(ctx, id, true); err != nil {
				return ActionResult{}, err
			}
		}
		n.refreshTitle(t, 0, false)
		return ActionResult{OK: true, Message: "Metadata reloaded; Jellyfin is rescanning " + t.Title}, nil
	case "search":
		return a.SearchMissing(ctx, t, true)
	}
	return ActionResult{}, fmt.Errorf("unknown command %q", name)
}

// nativeDeleteMovieFile deletes the movie files roarr imported.
func (a *App) nativeDeleteMovieFile(ctx context.Context, id int, removeTorrent bool) (ActionResult, error) {
	n := a.Native
	t, err := n.titleByID("movie", id)
	if err != nil {
		return ActionResult{}, err
	}
	defer a.touchedNative("movie", id)
	var paths []string
	notOurs := 0
	for _, f := range n.Importer().OnDisk(t) {
		if f.Record == nil {
			notOurs++
		} else {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		if notOurs > 0 {
			return ActionResult{}, errors.New("roarr did not import the file on disk, so it does not delete it")
		}
		return ActionResult{OK: true, Message: "Nothing on disk to delete."}, nil
	}
	return n.deleteFiles(ctx, t, paths, removeTorrent)
}

// nativeRemoveTitle stops tracking a title. Downloads of the title that are
// not finished are removed from qBittorrent with their data. With
// deleteFiles, the library files roarr imported are deleted; with
// removeTorrent, the finished torrents of the title are removed too.
func (a *App) nativeRemoveTitle(ctx context.Context, kind string, id int, deleteFiles, removeTorrent bool) error {
	n := a.Native
	t, err := n.titleByID(kind, id)
	if err != nil {
		return err
	}
	key := t.Key()
	c := a.C()
	for _, j := range n.Jobs() {
		if j.TitleKey != key {
			continue
		}
		active := !j.final()
		if (active || removeTorrent) && c.QBit != nil {
			if err := c.QBit.Delete(ctx, j.Hash, true); err != nil {
				return fmt.Errorf("removing the torrent %s: %w", j.Release, err)
			}
		}
		n.Remove(j.Hash)
	}
	if err := n.store.RemoveTitle(key); err != nil {
		return err
	}
	if deleteFiles {
		n.libMu.Lock() // an import that is running finishes first
		var paths []string
		for _, f := range n.store.Files(key) {
			paths = append(paths, f.Path)
		}
		res := n.Importer().Delete(paths)
		n.libMu.Unlock()
		for p, why := range res.Refused {
			slog.Warn("native library: file not deleted", "path", p, "reason", why)
		}
	}
	slog.Info("native library: title removed", "title", t.Title, "deleteFiles", deleteFiles, "removeTorrent", removeTorrent)
	n.forgetShows()
	n.refreshTitle(t, 0, deleteFiles)
	a.touchedNative(kind, id)
	if a.Library != nil {
		a.Library.Load(ctx)
	}
	return nil
}

// deleteFiles deletes library files of a title (see importer.Delete),
// optionally removes their torrents, and asks Jellyfin to rescan.
func (n *Native) deleteFiles(ctx context.Context, t importer.Title, paths []string, removeTorrent bool) (ActionResult, error) {
	n.libMu.Lock()
	res := n.Importer().Delete(paths)
	n.libMu.Unlock()
	for p, why := range res.Refused {
		slog.Warn("native library: file not deleted", "path", p, "reason", why)
	}
	if len(res.Deleted) == 0 {
		for _, why := range res.Refused {
			return ActionResult{}, errors.New("nothing deleted: " + why)
		}
		return ActionResult{OK: true, Message: "Nothing to delete."}, nil
	}
	slog.Info("native library: files deleted", "title", t.Title, "files", len(res.Deleted), "subtitles", len(res.Subtitles))
	msg := plural(len(res.Deleted), "file") + " deleted"
	if len(res.Refused) > 0 {
		msg += fmt.Sprintf("; %d not deleted (see the log)", len(res.Refused))
	}
	if removeTorrent {
		removed, kept := n.removeTorrents(ctx, res.Deleted)
		if removed > 0 {
			msg += "; " + plural(removed, "torrent") + " removed"
		}
		if kept > 0 {
			msg += "; " + plural(kept, "torrent") + " kept: other library files come from it"
		}
	}
	n.forgetShows()
	full := false
	if dir := n.Importer().TitleFolder(t); dir != "" && !dirExists(dir) {
		full = true
	}
	n.refreshTitle(t, 0, full)
	return ActionResult{OK: true, Count: len(res.Deleted), Message: msg}, nil
}

// removeTorrents removes the torrents the deleted files came from, with
// their data, unless the torrent is still downloading or other library
// files came from it.
func (n *Native) removeTorrents(ctx context.Context, deleted []importer.FileRecord) (removed, kept int) {
	c := n.app.C()
	if c.QBit == nil {
		return 0, 0
	}
	hashes := map[string]bool{}
	for _, r := range deleted {
		if r.Hash != "" {
			hashes[strings.ToLower(r.Hash)] = true
		}
	}
	inUse := map[string]bool{}
	for _, r := range n.store.Files("") {
		inUse[strings.ToLower(r.Hash)] = true
	}
	for _, j := range n.Jobs() {
		if !j.final() {
			inUse[j.Hash] = true
		}
	}
	for h := range hashes {
		if inUse[h] {
			kept++
			continue
		}
		if err := c.QBit.Delete(ctx, h, true); err != nil {
			slog.Warn("native library: could not remove torrent", "hash", h, "err", err)
			kept++
			continue
		}
		n.Remove(h)
		removed++
	}
	return removed, kept
}

// touchedNative forgets the cached page of a native title and reloads the
// library list soon.
func (a *App) touchedNative(kind string, id int) {
	a.detailMu.Lock()
	delete(a.details, fmt.Sprintf("%s:%d", kind, id))
	a.detailMu.Unlock()
	if a.Library != nil {
		a.Library.Refresh(context.Background())
	}
}

// ---------------------------------------------------------------------------
// Missing search

// missingRequest is the auto-grab request for what a native title misses:
// monitored, aired episodes that are not on disk and not downloading, or
// the movie when it is not on disk. all includes a movie that is not
// monitored (a search the user asked for).
func (a *App) missingRequest(ctx context.Context, t importer.Title, all bool) (AutoRequest, int, error) {
	if cur, ok := a.Native.store.Title(t.Key()); ok {
		t = cur
	}
	id := NativeID(t)
	if t.Kind == "movie" {
		if t.Unmonitored && !all {
			return AutoRequest{}, 0, nil
		}
		if _, ok := a.Native.movieFile(t); ok || a.Native.queued(t.Key())[[2]int{}] {
			return AutoRequest{}, 0, nil
		}
		return AutoRequest{Kind: "movie", MovieTmdb: []int{t.TmdbID}}, 1, nil
	}
	d, err := a.nativeSeriesDetail(ctx, id, false)
	if err != nil {
		return AutoRequest{}, 0, err
	}
	var ids []int
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			if e.Monitored && e.Aired && !e.HasFile && !e.Queued {
				ids = append(ids, e.ID)
			}
		}
	}
	if len(ids) == 0 {
		return AutoRequest{}, 0, nil
	}
	return AutoRequest{Kind: "tv", SeriesID: id, TvdbID: d.TvdbID, TmdbID: d.TmdbID, EpisodeIDs: ids}, len(ids), nil
}

// SearchMissing starts a search for what a native library title misses
// and grabs the best releases (the same planner as "Get"). It returns at
// once; grabs show in the activity feed.
func (a *App) SearchMissing(ctx context.Context, t importer.Title, manual bool) (ActionResult, error) {
	if t.Kind == "movie" && t.TmdbID == 0 {
		return ActionResult{}, errors.New("this movie has no TMDB id, so roarr cannot search for it")
	}
	req, count, err := a.missingRequest(ctx, t, manual)
	if err != nil {
		return ActionResult{}, err
	}
	if count == 0 {
		return ActionResult{OK: true, Message: "Nothing monitored is missing."}, nil
	}
	if !a.Native.startSearch(t.Key(), func(ctx context.Context) { a.logAuto(ctx, t, req) }) {
		return ActionResult{OK: true, Message: "roarr is already searching for " + t.Title}, nil
	}
	what := t.Title
	if t.Kind == "tv" {
		what = plural(count, "missing episode") + " of " + t.Title
	}
	return ActionResult{OK: true, Count: count, Message: "roarr is searching for " + what}, nil
}

// startSearch runs fn in the background unless a search for key is running.
func (n *Native) startSearch(key string, fn func(ctx context.Context)) bool {
	if !n.claimSearch(key) {
		return false
	}
	go func() {
		defer n.releaseSearch(key)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		fn(ctx)
	}()
	return true
}

func (a *App) logAuto(ctx context.Context, t importer.Title, req AutoRequest) {
	res, err := a.Auto(ctx, req)
	if err != nil {
		slog.Warn("missing search failed", "title", t.Title, "err", err)
		return
	}
	slog.Info("missing search", "title", t.Title, "result", res.Message)
}

// missingLoop searches every MissingSearchHours for what the monitored
// native titles miss. One title at a time, so indexers are not flooded.
// The first search runs 10 minutes after the start.
func (n *Native) missingLoop(ctx context.Context) {
	last := time.Now().Add(-24 * time.Hour * 365)
	start := time.Now()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		hours := n.app.Settings.Get().Import.MissingSearchHours
		if hours <= 0 || !n.app.Settings.Get().Import.Native() || time.Since(start) < 10*time.Minute || time.Since(last) < time.Duration(hours)*time.Hour {
			continue
		}
		last = time.Now()
		n.searchAllMissing(ctx)
	}
}

// searchAllMissing runs one missing search over every monitored title of
// the native library. A title with files below the quality cutoff is also
// searched for upgrades, at most once every upgradeEvery.
func (n *Native) searchAllMissing(ctx context.Context) {
	a := n.app
	slog.Info("missing search: start")
	for _, t := range n.store.Titles() {
		if ctx.Err() != nil {
			return
		}
		if t.Unmonitored || NativeID(t) == 0 || !a.nativeLib(t.Kind) {
			continue
		}
		run := func(req AutoRequest) {
			tctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			if n.claimSearch(t.Key()) {
				a.logAuto(tctx, t, req)
				n.releaseSearch(t.Key())
			}
			select {
			case <-ctx.Done():
			case <-time.After(missingPause):
			}
		}
		mctx, cancel := context.WithTimeout(ctx, time.Minute)
		req, count, err := a.missingRequest(mctx, t, false)
		cancel()
		if err != nil {
			slog.Warn("missing search", "title", t.Title, "err", err)
		} else if count > 0 {
			run(req)
		}
		if at, err := time.Parse(time.RFC3339, t.UpgradeAt); err == nil && time.Since(at) < upgradeEvery {
			continue
		}
		_, _ = n.store.UpdateTitle(t.Key(), func(x *importer.Title) { x.UpgradeAt = time.Now().UTC().Format(time.RFC3339) })
		if req, count := a.upgradeRequest(t); count > 0 {
			run(req)
		}
	}
	slog.Info("missing search: done")
}

// upgradeEvery is how often a title is searched for upgrades. Upgrades
// are rarely urgent, and a library taken over from Sonarr can hold
// hundreds of titles below the cutoff: once a week keeps indexers calm.
const upgradeEvery = 7 * 24 * time.Hour

// upgradeRequest is the upgrade search of a native title: its monitored
// episodes (or the movie) whose file is below the quality cutoff. A file
// of unknown quality (renamed, no release recorded) is left alone, so
// roarr never replaces it in a loop.
func (a *App) upgradeRequest(t importer.Title) (AutoRequest, int) {
	n := a.Native
	prefs := a.Settings.Get().Prefs
	below := func(f importer.LibraryFile) bool {
		name := filepath.Base(f.Path)
		if f.Record != nil && f.Record.Release != "" {
			name = f.Record.Release
		}
		i := release.Parse(name)
		return i.Resolution > 0 && !prefs.AtCutoff(i)
	}
	queued := n.queued(t.Key())
	if t.Unmonitored {
		return AutoRequest{}, 0
	}
	if t.Kind == "movie" {
		if f, ok := n.movieFile(t); ok && t.TmdbID != 0 && below(f) && !queued[[2]int{}] {
			return AutoRequest{Kind: "movie", MovieTmdb: []int{t.TmdbID}, Upgrade: true}, 1
		}
		return AutoRequest{}, 0
	}
	airs := map[[2]int]string{}
	for _, e := range t.Episodes {
		airs[[2]int{e.Season, e.Number}] = e.AirDate
	}
	var ids []int
	for _, f := range n.Importer().OnDisk(t) {
		if !below(f) {
			continue
		}
		for _, e := range f.Episodes {
			se := [2]int{f.Season, e}
			if episodeMonitored(t, f.Season, e, airs[se]) && !queued[se] {
				ids = append(ids, importer.EpisodeID(f.Season, e))
			}
		}
	}
	if len(ids) == 0 {
		return AutoRequest{}, 0
	}
	sort.Ints(ids)
	ids = slices.Compact(ids) // two files can hold one episode
	return AutoRequest{Kind: "tv", SeriesID: NativeID(t), TvdbID: t.TvdbID, TmdbID: t.TmdbID, EpisodeIDs: ids, Upgrade: true}, len(ids)
}

// airLoop grabs new episodes soon after they air, and keeps the episode
// lists of monitored shows current so new episodes are known at all. Air
// dates are days and releases appear hours after a broadcast, so a missing
// episode is searched from its air date on: often at first, then less
// often (airedSearchEvery), for two weeks. After that the periodic missing
// search covers it. The schedule is in memory; after a restart every
// recent episode is searched once more.
func (n *Native) airLoop(ctx context.Context) {
	start := time.Now()
	last := map[string]time.Time{}
	var metaAt time.Time
	t := time.NewTicker(airTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !n.app.Settings.Get().Import.Native() || !n.app.nativeLib("tv") || time.Since(start) < 2*time.Minute {
			continue
		}
		if time.Since(metaAt) > metaMaxAge {
			metaAt = time.Now()
			n.refreshShows(ctx)
		}
		n.searchAired(ctx, last, time.Now())
	}
}

// airTick is how often airLoop looks for new episodes; it searches only
// when airedSearchEvery says so.
var airTick = 15 * time.Minute

// airedSearchEvery is how often a show is searched for an episode that
// aired days ago and is missing; 0 means not any more.
func airedSearchEvery(days int) time.Duration {
	switch {
	case days <= 1:
		return time.Hour
	case days <= 6:
		return 4 * time.Hour
	case days <= 14:
		return 12 * time.Hour
	}
	return 0
}

// refreshShows reloads the episode lists of monitored native shows whose
// list is older than metaMaxAge, one show at a time.
func (n *Native) refreshShows(ctx context.Context) {
	for _, t := range n.store.Titles() {
		if t.Kind != "tv" || t.Unmonitored || NativeID(t) == 0 || !metaStale(t.MetaAt) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		tctx, cancel := context.WithTimeout(ctx, time.Minute)
		if _, err := n.app.nativeSeriesDetail(tctx, NativeID(t), false); err != nil {
			slog.Warn("native library: episode list not refreshed", "title", t.Title, "err", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(missingPause):
		}
	}
	n.forgetShows()
}

// searchAired searches every monitored native show for its recently aired,
// missing episodes that are due (airedSearchEvery). last holds when each
// show was last searched this way.
func (n *Native) searchAired(ctx context.Context, last map[string]time.Time, now time.Time) {
	a := n.app
	for _, t := range n.store.Titles() {
		if t.Kind != "tv" || t.Unmonitored || NativeID(t) == 0 {
			continue
		}
		var ids []int
		every := time.Duration(0)
		for _, s := range n.showState(t).seasons {
			for _, e := range s.Episodes {
				aired, err := time.ParseInLocation(time.DateOnly, e.AirDate, time.Local)
				if err != nil || !e.Monitored || e.HasFile || e.Queued || aired.After(now) {
					continue
				}
				if d := airedSearchEvery(int(now.Sub(aired).Hours() / 24)); d > 0 {
					ids = append(ids, e.ID)
					if every == 0 || d < every {
						every = d
					}
				}
			}
		}
		key := t.Key()
		if len(ids) == 0 || now.Sub(last[key]) < every || !n.claimSearch(key) {
			continue
		}
		last[key] = now
		tctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		a.logAuto(tctx, t, AutoRequest{Kind: "tv", SeriesID: NativeID(t), TvdbID: t.TvdbID, TmdbID: t.TmdbID, EpisodeIDs: ids})
		cancel()
		n.releaseSearch(key)
		select {
		case <-ctx.Done():
			return
		case <-time.After(missingPause):
		}
	}
}

// missingPause is the wait between two titles of a missing search.
var missingPause = 5 * time.Second

func (n *Native) claimSearch(key string) bool {
	n.searchMu.Lock()
	defer n.searchMu.Unlock()
	if n.searching[key] {
		return false
	}
	n.searching[key] = true
	return true
}

func (n *Native) releaseSearch(key string) {
	n.searchMu.Lock()
	delete(n.searching, key)
	n.searchMu.Unlock()
}

// ---------------------------------------------------------------------------

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func addInt(xs []int, v int) []int {
	if slices.Contains(xs, v) {
		return xs
	}
	xs = append(xs, v)
	sort.Ints(xs)
	return xs
}

func removeInt(xs []int, v int) []int {
	return slices.DeleteFunc(xs, func(x int) bool { return x == v })
}
