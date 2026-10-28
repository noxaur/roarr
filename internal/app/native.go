package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/release"
)

// Native is roarr's own download + import backend (IMPORT_MODE=native).
// A grab goes straight to qBittorrent in roarr's category; this loop maps
// the torrent's files to episodes, skips samples, extras and episodes
// already in the library, downloads packs in episode order when
// watch-first is on, and imports every file into TV_ROOT / MOVIES_ROOT the
// moment it is complete. Jellyfin (and Sonarr/Radarr, when still
// configured) are asked to rescan afterwards.
type Native struct {
	app   *App
	path  string
	store *importer.Store

	mu     sync.Mutex
	jobs   map[string]*NativeJob // by info-hash
	kick   chan struct{}
	saveMu sync.Mutex // one snapshot+write+rename of native.json at a time

	refreshMu      sync.Mutex
	refreshAt      map[string]time.Time
	refreshPending map[string]bool
}

// NativeJob is one torrent roarr downloads and imports itself.
type NativeJob struct {
	Hash        string       `json:"hash"`
	Release     string       `json:"release"`
	Kind        string       `json:"kind"` // tv | movie
	TitleKey    string       `json:"titleKey"`
	TitleName   string       `json:"titleName"`
	Href        string       `json:"href,omitempty"`
	Poster      string       `json:"poster,omitempty"`
	ArrID       int          `json:"arrId,omitempty"`
	Wanted      [][2]int     `json:"wanted,omitempty"`
	Watch       bool         `json:"watch,omitempty"`
	AddedPaused bool         `json:"addedPaused,omitempty"`
	Resumed     bool         `json:"resumed,omitempty"`
	Planned     bool         `json:"planned,omitempty"`
	Seen        bool         `json:"seen,omitempty"` // qBittorrent has listed it
	Created     time.Time    `json:"created"`
	CompletedAt time.Time    `json:"completedAt,omitempty"`
	FirstReady  time.Time    `json:"firstReady,omitempty"`
	State       string       `json:"state"` // waiting | downloading | importing | done | failed
	Message     string       `json:"message,omitempty"`
	QState      string       `json:"qbitState,omitempty"`
	Progress    float64      `json:"progress"`
	Speed       int64        `json:"speed"`
	Size        int64        `json:"size"`
	Files       []NativeFile `json:"files"`
	Skipped     []string     `json:"skipped,omitempty"` // files left out, with reasons
}

// NativeFile is one video in a native job and its import state.
type NativeFile struct {
	Placement  importer.Placement `json:"placement"`
	Index      int                `json:"index"`
	SubIndexes []int              `json:"subIndexes,omitempty"`
	Size       int64              `json:"size"`
	Progress   float64            `json:"progress"`
	Priority   int                `json:"priority"`
	Skipped    bool               `json:"skipped,omitempty"` // not downloaded or not imported
	Imported   bool               `json:"imported,omitempty"`
	ImportedAt string             `json:"importedAt,omitempty"`
	Dest       string             `json:"dest,omitempty"`
	Note       string             `json:"note,omitempty"`
	Error      string             `json:"error,omitempty"`
	Attempts   int                `json:"attempts,omitempty"`
	// SubsPlaced lists the subtitles (by path in the torrent) already next
	// to the imported video. Subtitles that finish after their video, or
	// whose transfer failed, are placed on later passes.
	SubsPlaced  []string `json:"subsPlaced,omitempty"`
	SubAttempts int      `json:"subAttempts,omitempty"`
	SubError    string   `json:"subError,omitempty"`
}

// maxSubAttempts bounds retries of failed subtitle transfers (one per pass).
const maxSubAttempts = 30

// pendingSubs returns the indexes into Placement.Subtitles not placed yet.
func (f NativeFile) pendingSubs() []int {
	done := map[string]bool{}
	for _, r := range f.SubsPlaced {
		done[r] = true
	}
	var out []int
	for k, s := range f.Placement.Subtitles {
		if !done[s.File.Rel] {
			out = append(out, k)
		}
	}
	return out
}

// waitingForSubs reports whether an imported video still expects subtitles.
func (f NativeFile) waitingForSubs() bool {
	return f.Imported && f.Dest != "" && f.SubAttempts < maxSubAttempts && len(f.pendingSubs()) > 0
}

func newNative(a *App, dir string) *Native {
	n := &Native{app: a, path: filepath.Join(dir, "native.json"), jobs: map[string]*NativeJob{}, kick: make(chan struct{}, 1),
		refreshAt: map[string]time.Time{}, refreshPending: map[string]bool{}}
	st, err := importer.OpenStore(filepath.Join(dir, "library.json"))
	if err != nil {
		// Never overwrite a file we could not read: run from memory.
		slog.Error("native import: cannot read library.json; imports are not remembered until it is fixed", "err", err)
		st, _ = importer.OpenStore("")
	}
	n.store = st
	if b, err := os.ReadFile(n.path); err == nil {
		var jobs []*NativeJob
		if json.Unmarshal(b, &jobs) == nil {
			for _, j := range jobs {
				n.jobs[j.Hash] = j
			}
		}
	}
	return n
}

// Name implements Backend.
func (n *Native) Name() string { return "native" }

// Store returns the library store (titles and imported files).
func (n *Native) Store() *importer.Store { return n.store }

// Importer returns an importer for the current settings.
func (n *Native) Importer() *importer.Importer {
	s := n.app.Settings.Get().Import
	return importer.New(importer.Config{TVRoot: s.TVRoot, MovieRoot: s.MovieRoot, Mode: importer.ParseMode(s.Transfer)}, n.store, n.app.Hooks)
}

// keepName reuses the name and year a title was first imported under, so
// its library folder stays put when the metadata source changes.
func (n *Native) keepName(t *importer.Title) {
	if old, ok := n.store.Title(t.Key()); ok && old.Title != "" {
		t.Title, t.Year = old.Title, old.Year
	}
}

// markOnDisk flags episodes of a synthetic show detail that roarr
// imported (HasFile) or is downloading (Queued), so "missing" means
// missing in native mode too.
func (n *Native) markOnDisk(d *SeriesDetail) {
	key := importer.Title{Kind: "tv", TmdbID: d.TmdbID, TvdbID: d.TvdbID}.Key()
	if _, ok := n.store.Title(key); !ok {
		return
	}
	queued := map[[2]int]bool{}
	for _, j := range n.Jobs() {
		if j.TitleKey != key || j.State == "done" || j.State == "failed" {
			continue
		}
		for _, w := range j.Wanted {
			queued[w] = true
		}
		for _, f := range j.Files {
			if !f.Skipped && !f.Imported {
				for _, e := range f.Placement.Episodes {
					queued[[2]int{f.Placement.Season, e}] = true
				}
			}
		}
	}
	for si := range d.Seasons {
		for ei := range d.Seasons[si].Episodes {
			e := &d.Seasons[si].Episodes[ei]
			if !e.HasFile && n.store.HasEpisode(key, e.Season, e.Number) {
				e.HasFile = true
				d.Have++
			}
			if !e.HasFile && queued[[2]int{e.Season, e.Number}] {
				e.Queued = true
			}
		}
	}
}

func (n *Native) hasMovie(tmdbID int) bool {
	for _, f := range n.store.Files(importer.Title{Kind: "movie", TmdbID: tmdbID}.Key()) {
		if _, err := os.Stat(f.Path); err == nil {
			return true
		}
	}
	return false
}

// GrabEpisodes implements Backend.
func (n *Native) GrabEpisodes(ctx context.Context, g GrabRequest) (GrabResult, error) {
	if n.app.Settings.Get().Import.TVRoot == "" {
		return GrabResult{}, fmt.Errorf("native import needs a TV library folder: set TV_ROOT")
	}
	return n.grab(ctx, g)
}

// GrabMovie implements Backend.
func (n *Native) GrabMovie(ctx context.Context, g GrabRequest) (GrabResult, error) {
	if n.app.Settings.Get().Import.MovieRoot == "" {
		return GrabResult{}, fmt.Errorf("native import needs a movie library folder: set MOVIES_ROOT")
	}
	return n.grab(ctx, g)
}

func (n *Native) grab(ctx context.Context, g GrabRequest) (GrabResult, error) {
	a := n.app
	if a.C().QBit == nil {
		return GrabResult{}, fmt.Errorf("qbittorrent %w (native import downloads through qBittorrent: set QBIT_URL / QBIT_PASSWORD)", ErrNotConfigured)
	}
	if g.Release.Protocol == "usenet" {
		return GrabResult{}, errors.New("native import only handles torrents; switch IMPORT_MODE to arr for usenet releases")
	}
	tg, err := a.nativeTarget(ctx, g)
	if err != nil {
		return GrabResult{}, err
	}
	if err := n.store.PutTitle(tg.Title); err != nil {
		slog.Warn("native import: could not save library.json", "err", err)
	}
	st := a.Settings.Get()
	watch := st.Behaviour.WatchFirst
	if g.WatchFirst != nil {
		watch = *g.WatchFirst
	}
	info := release.Parse(g.Release.Title)
	watch = watch && g.Kind != "movie" && info.IsPack()
	hash, err := a.addToQBit(ctx, qbit.Add{Category: st.Import.Category, SavePath: st.Import.SavePath, Paused: watch}, g.Release)
	if err != nil {
		return GrabResult{}, err
	}
	if hash == "" {
		return GrabResult{}, errors.New("qBittorrent took the release but roarr could not learn its info-hash, so it cannot import it")
	}
	n.Track(NativeJob{Hash: hash, Release: g.Release.Title, Kind: tg.Title.Kind, TitleKey: tg.Title.Key(), TitleName: tg.Title.Title,
		Href: tg.Href, Poster: tg.Poster, ArrID: tg.ArrID, Wanted: tg.Wanted, Watch: watch, AddedPaused: watch})
	n.forgetShows()
	res := GrabResult{OK: true, Via: "roarr", Hash: hash, WatchFirst: watch, Message: "Sent to qBittorrent; roarr imports it into the library when it finishes."}
	if g.Kind == "movie" {
		res.MovieID = tg.ArrID
	} else {
		res.SeriesID = tg.ArrID
	}
	if watch {
		res.Message = "Sent to qBittorrent; episodes are imported one by one, in order, as they finish."
	}
	return res, nil
}

// Track starts following a torrent.
func (n *Native) Track(j NativeJob) {
	j.Hash = strings.ToLower(j.Hash)
	j.Created, j.State = time.Now(), "waiting"
	if j.Files == nil {
		j.Files = []NativeFile{}
	}
	n.mu.Lock()
	n.jobs[j.Hash] = &j
	n.mu.Unlock()
	n.save()
	select {
	case n.kick <- struct{}{}:
	default:
	}
}

// Jobs returns a snapshot of all jobs, newest first.
func (n *Native) Jobs() []NativeJob {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]NativeJob, 0, len(n.jobs))
	for _, j := range n.jobs {
		cp := *j
		cp.Files = append([]NativeFile(nil), j.Files...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out
}

// Remove forgets a job.
func (n *Native) Remove(hash string) {
	n.mu.Lock()
	delete(n.jobs, strings.ToLower(hash))
	n.mu.Unlock()
	n.save()
}

func (n *Native) save() {
	n.saveMu.Lock()
	defer n.saveMu.Unlock()
	n.mu.Lock()
	jobs := make([]*NativeJob, 0, len(n.jobs))
	for _, j := range n.jobs {
		jobs = append(jobs, j)
	}
	b, err := json.MarshalIndent(jobs, "", " ")
	n.mu.Unlock()
	if err != nil {
		return
	}
	tmp := n.path + ".tmp"
	if writeJobs(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, n.path)
	}
}

// writeJobs is os.WriteFile; tests slow it down to widen race windows.
var writeJobs = os.WriteFile

func (n *Native) loop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-n.kick:
		}
		n.tick(ctx)
	}
}

func (n *Native) tick(ctx context.Context) {
	c := n.app.C()
	if c.QBit == nil {
		return
	}
	n.mu.Lock()
	var active []*NativeJob
	for _, j := range n.jobs {
		if j.State == "done" || j.State == "failed" {
			if time.Since(j.Created) > 7*24*time.Hour {
				delete(n.jobs, j.Hash)
			}
			continue
		}
		active = append(active, j)
	}
	n.mu.Unlock()
	changed := false
	for _, j := range active {
		if n.step(ctx, c, j) {
			changed = true
		}
	}
	if changed {
		n.save()
	}
}

// step advances one job; it returns true when persisted state changed.
func (n *Native) step(ctx context.Context, c *Clients, j *NativeJob) bool {
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ts, err := c.QBit.Torrents(qctx, j.Hash)
	if err != nil {
		n.set(j, func() { j.Message = err.Error() })
		return false
	}
	if len(ts) == 0 {
		if j.Seen {
			return n.fail(j, "the torrent was removed from qBittorrent before everything was imported")
		}
		return n.failAfter(j, 10*time.Minute, "the torrent never appeared in qBittorrent")
	}
	t := ts[0]
	files, err := c.QBit.Files(qctx, j.Hash)
	n.set(j, func() { j.Seen, j.QState, j.Progress, j.Speed, j.Size = true, t.State, t.Progress, t.DLSpeed, t.Size })
	if err != nil || len(files) == 0 {
		// A magnet has no file list until peers send the metadata, and a
		// stopped torrent never asks for it.
		if st := strings.ToLower(t.State); j.AddedPaused && !j.Resumed && (strings.Contains(st, "stopped") || strings.Contains(st, "paused")) {
			_ = c.QBit.Start(qctx, j.Hash)
		}
		n.set(j, func() { j.Message = "waiting for torrent metadata from peers" })
		return n.failAfter(j, 30*time.Minute, "no peer ever sent the torrent's metadata")
	}
	if !j.Planned {
		n.plan(qctx, c, j, files)
	}
	n.syncProgress(j, files)
	if j.Watch {
		n.prioritise(qctx, c, j)
	}
	if j.AddedPaused && !j.Resumed {
		st := strings.ToLower(t.State)
		if strings.Contains(st, "paused") || strings.Contains(st, "stopped") {
			if err := c.QBit.Start(qctx, j.Hash); err != nil {
				slog.Warn("native: could not start torrent", "err", err)
			}
		} else if !strings.Contains(st, "checking") {
			n.set(j, func() { j.Resumed = true })
		}
	}
	n.importReady(ctx, j, t, files)

	n.mu.Lock()
	defer n.mu.Unlock()
	if t.Progress >= 1 && j.CompletedAt.IsZero() {
		j.CompletedAt = time.Now()
	}
	pending, failed, imported, subsWaiting := 0, 0, 0, 0
	var lastErr string
	for _, f := range j.Files {
		if f.waitingForSubs() {
			subsWaiting++
		}
		switch {
		case f.Error != "":
			failed++
			lastErr = f.Error
		case f.Imported:
			imported++
		case !f.Skipped:
			pending++
		}
	}
	switch {
	case !j.Planned:
	case pending == 0 && failed > 0:
		j.State, j.Message = "failed", fmt.Sprintf("%d of %d file(s) could not be imported: %s", failed, failed+imported, lastErr)
	case pending == 0 && subsWaiting > 0 && (j.CompletedAt.IsZero() || time.Since(j.CompletedAt) < 10*time.Minute):
		// Videos are in; subtitles are still downloading or being retried.
		j.State = "importing"
	case pending == 0:
		j.State, j.Message = "done", ""
	case !j.CompletedAt.IsZero() && time.Since(j.CompletedAt) > 30*time.Minute:
		j.State, j.Message = "failed", fmt.Sprintf("%d file(s) were never imported", pending)
	case t.Progress >= 1:
		j.State = "importing"
	default:
		j.State = "downloading"
	}
	return true
}

func (n *Native) set(j *NativeJob, fn func()) {
	n.mu.Lock()
	fn()
	n.mu.Unlock()
}

func (n *Native) fail(j *NativeJob, msg string) bool {
	n.set(j, func() { j.State, j.Message = "failed", msg })
	return true
}

func (n *Native) failAfter(j *NativeJob, d time.Duration, msg string) bool {
	if time.Since(j.Created) < d {
		return false
	}
	return n.fail(j, msg)
}

// plan maps the torrent's files to placements, decides what to skip and
// sets file priorities accordingly.
func (n *Native) plan(ctx context.Context, c *Clients, j *NativeJob, files []qbit.File) {
	title, ok := n.store.Title(j.TitleKey)
	if !ok {
		n.set(j, func() {
			j.Planned, j.State, j.Message = true, "failed", "roarr lost the metadata for this title (library.json)"
		})
		return
	}
	src := make([]importer.SourceFile, 0, len(files))
	byRel := map[string]qbit.File{}
	for _, f := range files {
		rel := strings.TrimSuffix(f.Name, ".!qB")
		src = append(src, importer.SourceFile{Rel: rel, Size: f.Size})
		byRel[rel] = f
	}
	var ps []importer.Placement
	var skips []importer.Skip
	if j.Kind == "movie" {
		ps, skips = importer.PlanMovie(src, j.Release, title)
	} else {
		ps, skips = importer.PlanTV(src, j.Release, title)
	}
	im := n.Importer()
	keep := map[int]bool{}
	var subs []int
	var nf []NativeFile
	for _, p := range ps {
		f := NativeFile{Placement: p, Index: byRel[p.Video.Rel].Index, Size: p.Video.Size}
		for _, s := range p.Subtitles {
			f.SubIndexes = append(f.SubIndexes, byRel[s.File.Rel].Index)
		}
		if j.Kind == "tv" && !n.wanted(j, p) {
			need := false
			for _, e := range p.Episodes {
				if len(im.Existing(title, p.Season, []int{e})) == 0 {
					need = true
				}
			}
			if !need {
				f.Skipped, f.Note = true, "already in the library"
			}
		}
		if !f.Skipped {
			keep[f.Index] = true
			for _, s := range f.SubIndexes {
				keep[s] = true
				subs = append(subs, s)
			}
		}
		nf = append(nf, f)
	}
	var skipIdx []int
	for _, f := range files {
		if !keep[f.Index] {
			skipIdx = append(skipIdx, f.Index)
		}
	}
	if len(skipIdx) > 0 {
		if err := c.QBit.SetFilePriority(ctx, j.Hash, skipIdx, qbit.PrioSkip); err != nil {
			slog.Warn("native: could not skip files", "err", err)
		}
	}
	if len(subs) > 0 {
		_ = c.QBit.SetFilePriority(ctx, j.Hash, subs, qbit.PrioMax) // tiny; ready before their video
	}
	if j.Watch {
		if err := c.QBit.SetSequential(ctx, j.Hash, watchSequential); err != nil {
			slog.Warn("native: sequential toggle failed", "err", err)
		}
	}
	var skipped []string
	for _, s := range skips {
		skipped = append(skipped, s.File.Rel+": "+s.Reason)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	j.Files, j.Skipped, j.Planned, j.Message = nf, skipped, true, ""
	if j.Files == nil {
		j.Files = []NativeFile{}
	}
	if len(ps) == 0 {
		j.State, j.Message = "failed", "nothing in this download matches "+title.Title
	}
}

// wanted reports whether the user asked for any episode of p.
func (n *Native) wanted(j *NativeJob, p importer.Placement) bool {
	for _, w := range j.Wanted {
		if w[0] != p.Season {
			continue
		}
		for _, e := range p.Episodes {
			if w[1] == e {
				return true
			}
		}
	}
	return false
}

func (n *Native) syncProgress(j *NativeJob, files []qbit.File) {
	byIdx := map[int]qbit.File{}
	for _, f := range files {
		byIdx[f.Index] = f
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := range j.Files {
		if f, ok := byIdx[j.Files[i].Index]; ok {
			j.Files[i].Progress, j.Files[i].Priority = f.Progress, f.Priority
		}
	}
}

// prioritise applies the watch-first rolling window (see watch.go).
func (n *Native) prioritise(ctx context.Context, c *Clients, j *NativeJob) {
	n.mu.Lock()
	want := map[int]int{}
	slot := 0
	for _, f := range j.Files {
		if f.Skipped || f.Progress >= 1 {
			continue
		}
		p := qbit.PrioNormal
		switch {
		case slot == 0:
			p = qbit.PrioMax
		case slot < watchWindow:
			p = qbit.PrioHigh
		}
		slot++
		if f.Priority != p {
			want[f.Index] = p
		}
	}
	n.mu.Unlock()
	byPrio := map[int][]int{}
	for idx, p := range want {
		byPrio[p] = append(byPrio[p], idx)
	}
	for p, ids := range byPrio {
		if err := c.QBit.SetFilePriority(ctx, j.Hash, ids, p); err != nil {
			slog.Warn("native: priority update failed", "err", err)
		}
	}
}

// resolve finds a torrent file on roarr's side: qBittorrent's save path
// (or its incomplete-files folder while the torrent is unfinished) joined
// with the file's path in the torrent, then rewritten by QBIT_PATH_MAP.
func (n *Native) resolve(t qbit.Torrent, rel string) string {
	pm, err := importer.ParsePathMap(n.app.Settings.Get().Import.PathMap)
	if err != nil {
		slog.Warn("native: QBIT_PATH_MAP is invalid", "err", err)
	}
	join := func(base string) string {
		return pm.Map(strings.TrimRight(base, `/\`) + "/" + rel)
	}
	bases := []string{t.SavePath}
	if t.DownloadPath != "" {
		bases = append(bases, t.DownloadPath)
	}
	for _, b := range bases {
		if b == "" {
			continue
		}
		if p := join(b); fileExists(p) {
			return p
		}
	}
	return join(t.SavePath)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// importReady imports every finished, not yet imported file, and places
// subtitles that finished after their video.
func (n *Native) importReady(ctx context.Context, j *NativeJob, t qbit.Torrent, files []qbit.File) {
	title, ok := n.store.Title(j.TitleKey)
	if !ok {
		return
	}
	progress := map[int]float64{}
	for _, f := range files {
		progress[f.Index] = f.Progress
	}
	n.mu.Lock()
	var ready, subsDue []int
	for i, f := range j.Files {
		switch {
		case !f.Skipped && !f.Imported && f.Error == "" && f.Progress >= 1:
			ready = append(ready, i)
		case f.waitingForSubs():
			subsDue = append(subsDue, i)
		}
	}
	n.mu.Unlock()
	if len(ready) == 0 && len(subsDue) == 0 {
		return
	}
	// finishedSubs are the pending subtitles of f whose download is complete.
	finishedSubs := func(f NativeFile) []importer.SubtitleRef {
		var out []importer.SubtitleRef
		for _, k := range f.pendingSubs() {
			if k < len(f.SubIndexes) && progress[f.SubIndexes[k]] < 1 {
				continue
			}
			s := f.Placement.Subtitles[k]
			s.File.Path = n.resolve(t, s.File.Rel)
			out = append(out, s)
		}
		return out
	}
	// placedSubs records which of subs made it, and counts a failed pass.
	placedSubs := func(ff *NativeFile, subs []importer.SubtitleRef, errs map[string]string) {
		for _, s := range subs {
			if _, failed := errs[s.File.Rel]; !failed {
				ff.SubsPlaced = append(ff.SubsPlaced, s.File.Rel)
			}
		}
		for rel, e := range errs {
			ff.SubAttempts++
			ff.SubError = rel + ": " + e
			break
		}
	}
	im := n.Importer()
	changed := 0
	for _, i := range ready {
		n.mu.Lock()
		f := j.Files[i]
		n.mu.Unlock()
		p := f.Placement
		p.Video.Path = n.resolve(t, p.Video.Rel)
		p.Subtitles = finishedSubs(f)
		replace := j.Kind == "movie" || n.wanted(j, p)
		res, err := im.Import(ctx, importer.Request{Title: title, Placement: p, Release: j.Release, Hash: j.Hash, Replace: replace})
		n.mu.Lock()
		ff := &j.Files[i]
		switch {
		case errors.Is(err, importer.ErrRetryLater):
			ff.Note = err.Error()
		case err != nil:
			// The file may not be visible yet (qBittorrent still moving it
			// out of its incomplete folder): retry for about a minute.
			ff.Attempts++
			ff.Note = err.Error()
			if ff.Attempts >= 30 {
				ff.Error = err.Error()
			}
		case res.Skipped:
			ff.Skipped, ff.Note, ff.Dest = true, res.Reason, res.Path
		default:
			ff.Imported, ff.Dest, ff.Note = true, res.Path, ""
			ff.ImportedAt = time.Now().Format(time.RFC3339)
			placedSubs(ff, p.Subtitles, res.SubtitleErrors)
			if j.FirstReady.IsZero() {
				j.FirstReady = time.Now()
			}
			changed++
		}
		n.mu.Unlock()
		if err == nil && !res.Skipped {
			slog.Info("native: imported", "title", title.Title, "file", p.Video.Rel, "to", res.Path, "method", res.Method, "replaced", len(res.Replaced))
		} else if err != nil && !errors.Is(err, importer.ErrRetryLater) {
			slog.Warn("native: import failed", "title", title.Title, "file", p.Video.Rel, "err", err)
		}
	}
	for _, i := range subsDue {
		n.mu.Lock()
		f := j.Files[i]
		n.mu.Unlock()
		subs := finishedSubs(f)
		if len(subs) == 0 {
			continue
		}
		placed, errs, err := im.AddSubtitles(f.Dest, j.Hash, j.Release, subs)
		n.mu.Lock()
		ff := &j.Files[i]
		switch {
		case errors.Is(err, importer.ErrVideoReplaced):
			ff.SubAttempts, ff.SubError = maxSubAttempts, err.Error() // give up: not ours any more
		case err != nil:
			ff.SubAttempts++
			ff.SubError = err.Error()
		default:
			placedSubs(ff, subs, errs)
		}
		n.mu.Unlock()
		if len(placed) > 0 {
			changed++
			slog.Info("native: placed late subtitles", "title", title.Title, "video", f.Dest, "subtitles", len(placed))
		}
	}
	if changed > 0 {
		n.refresh(j, title)
		n.forgetShows()
	}
}

// forgetShows drops cached show pages: their on-disk and queued state
// changed.
func (n *Native) forgetShows() {
	n.app.detailMu.Lock()
	defer n.app.detailMu.Unlock()
	for k := range n.app.details {
		if strings.HasPrefix(k, "tv") {
			delete(n.app.details, k)
		}
	}
}

// refresh asks Jellyfin (and Sonarr/Radarr, when they know the title) to
// pick up new files. Refreshes are coalesced per title, at most one every
// 15 s, and one always runs after the latest import.
func (n *Native) refresh(j *NativeJob, t importer.Title) {
	key := j.TitleKey
	n.refreshMu.Lock()
	if n.refreshPending[key] {
		n.refreshMu.Unlock()
		return
	}
	wait := time.Second
	if since := time.Since(n.refreshAt[key]); since < 15*time.Second {
		wait += 15*time.Second - since
	}
	n.refreshPending[key] = true
	n.refreshMu.Unlock()
	kind, arrID := j.Kind, j.ArrID
	go func() {
		time.Sleep(wait)
		n.refreshMu.Lock()
		n.refreshPending[key] = false
		n.refreshAt[key] = time.Now()
		n.refreshMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := n.app.C()
		if arrID > 0 {
			var err error
			switch {
			case kind == "tv" && c.Sonarr != nil:
				err = c.Sonarr.RescanSeries(ctx, arrID)
			case kind == "movie" && c.Radarr != nil:
				err = c.Radarr.RefreshMovie(ctx, arrID)
			}
			if err != nil {
				slog.Warn("native: *arr rescan failed", "title", t.Title, "err", err)
			}
		}
		if c.Jellyfin == nil || n.app.Settings.Get().Jellyfin.APIKey == "" {
			return
		}
		jkind, provider, id := "Series", "Tmdb", strconv.Itoa(t.TmdbID)
		if kind == "movie" {
			jkind = "Movie"
		}
		if t.TmdbID == 0 && t.TvdbID != 0 {
			provider, id = "Tvdb", strconv.Itoa(t.TvdbID)
		}
		how, err := c.Jellyfin.Refresh(ctx, jkind, t.Title, provider, id)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("jellyfin refresh failed", "err", err)
			return
		}
		slog.Info("jellyfin refreshed", "title", t.Title, "how", how)
	}()
}

// activity renders unfinished native jobs as activity rows. Failed jobs
// stay until removed, so the reason is visible.
func (n *Native) activity() []ActivityItem {
	var out []ActivityItem
	for _, j := range n.Jobs() {
		if j.State == "done" {
			continue
		}
		it := ActivityItem{ID: "roarr:" + j.Hash, Source: "roarr", Kind: j.Kind, Title: j.TitleName, Release: j.Release, Poster: j.Poster, Href: j.Href,
			Status: nativeStatus(j), Progress: j.Progress, Speed: j.Speed, Size: j.Size, DownloadID: j.Hash, Client: "qBittorrent"}
		it.SizeLeft = int64(float64(j.Size) * (1 - j.Progress))
		if j.Message != "" {
			it.Messages = append(it.Messages, j.Message)
		}
		for _, f := range j.Files {
			if f.Error != "" {
				it.Messages = append(it.Messages, f.Placement.Video.Rel+": "+f.Error)
			}
		}
		if j.Kind == "tv" {
			it.Subtitle = describeRelease(j.Release)
			w := j.watchView()
			it.Watch = &w
		}
		out = append(out, it)
	}
	return out
}

func nativeStatus(j NativeJob) string {
	if j.State == "failed" {
		return "failed"
	}
	if j.State == "importing" {
		return "importing"
	}
	st := strings.ToLower(j.QState)
	switch {
	case st == "error" || st == "missingfiles":
		return "failed"
	case strings.HasPrefix(st, "stalled") && j.Progress < 1:
		return "stalled"
	case strings.HasPrefix(st, "paused") || strings.HasPrefix(st, "stopped"):
		return "paused"
	case strings.HasPrefix(st, "queued"):
		return "queued"
	case j.State == "waiting":
		return "queued"
	}
	return "downloading"
}

// watchView shows a native TV job in the watch-first shape the activity
// UI already renders (per-episode progress and import trail).
func (j NativeJob) watchView() WatchJob {
	state := j.State
	if state == "importing" {
		state = "downloading"
	}
	w := WatchJob{Hash: j.Hash, Title: j.Release, Kind: j.Kind, SeriesID: j.ArrID, SeriesTitle: j.TitleName, Created: j.Created, Started: j.Planned,
		State: state, Message: j.Message, Progress: j.Progress, Speed: j.Speed, FirstReady: j.FirstReady, CompletedAt: j.CompletedAt, Files: []WatchFile{}}
	for _, f := range j.Files {
		wf := WatchFile{Index: f.Index, Name: f.Placement.Video.Rel, Season: f.Placement.Season, Size: f.Size, Progress: f.Progress, Priority: f.Priority,
			Skipped: f.Skipped, Imported: f.Imported, ImportedAt: f.ImportedAt, Error: f.Error}
		if len(f.Placement.Episodes) > 0 {
			wf.Episode = f.Placement.Episodes[0]
		}
		w.Files = append(w.Files, wf)
	}
	return w
}
