package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/release"
)

// Watch-first downloads turn a season/series pack into a playlist: files
// download in episode order (a rolling priority window plus sequential piece
// picking), and every episode is imported into Sonarr — and refreshed in
// Jellyfin — the moment its file is complete, while the rest of the pack is
// still downloading.

// WatchJob is one pack being downloaded watch-first.
type WatchJob struct {
	Hash        string      `json:"hash"`
	Title       string      `json:"title"`
	Kind        string      `json:"kind"`
	SeriesID    int         `json:"seriesId"`
	TvdbID      int         `json:"tvdbId"`
	SeriesTitle string      `json:"seriesTitle"`
	Wanted      []int       `json:"wanted"`
	Covers      []int       `json:"covers"`
	Created     time.Time   `json:"created"`
	Started     bool        `json:"started"`               // priorities applied
	AddedPaused bool        `json:"addedPaused,omitempty"` // roarr added it stopped; resume once planned
	Resumed     bool        `json:"resumed,omitempty"`
	State       string      `json:"state"` // waiting | downloading | done | failed
	Message     string      `json:"message,omitempty"`
	Files       []WatchFile `json:"files"`
	Progress    float64     `json:"progress"`
	Speed       int64       `json:"speed"`
	FirstReady  time.Time   `json:"firstReady,omitempty"` // when episode 1 became watchable
	CompletedAt time.Time   `json:"completedAt,omitempty"`
}

// WatchFile is one episode file inside the pack.
type WatchFile struct {
	Index      int     `json:"index"`
	Name       string  `json:"name"`
	Season     int     `json:"season"`
	Episode    int     `json:"episode"`
	EpisodeIDs []int   `json:"episodeIds"`
	Size       int64   `json:"size"`
	Progress   float64 `json:"progress"`
	Priority   int     `json:"priority"`
	Skipped    bool    `json:"skipped,omitempty"` // already in library, not downloaded
	Imported   bool    `json:"imported,omitempty"`
	ImportedAt string  `json:"importedAt,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// WatchFirst manages watch-first jobs.
type WatchFirst struct {
	app  *App
	path string

	mu        sync.Mutex
	jobs      map[string]*WatchJob // by hash, or by title while the hash is unknown
	kick      chan struct{}
	jfAt      map[int]time.Time
	jfPending map[int]bool
}

// Strategy knobs, chosen by the lab swarm experiment (lab/swarm).
const (
	watchSequential = true // pieces in order inside the focused file
	watchWindow     = 2    // files at elevated priority
)

func newWatchFirst(a *App, dir string) *WatchFirst {
	w := &WatchFirst{app: a, path: filepath.Join(dir, "watch.json"), jobs: map[string]*WatchJob{}, kick: make(chan struct{}, 1), jfAt: map[int]time.Time{}, jfPending: map[int]bool{}}
	if b, err := os.ReadFile(w.path); err == nil {
		var jobs []*WatchJob
		if json.Unmarshal(b, &jobs) == nil {
			for _, j := range jobs {
				w.jobs[jobKey(j)] = j
			}
		}
	}
	return w
}

// JobKey identifies a job in Remove.
func JobKey(j WatchJob) string { return jobKey(&j) }

func jobKey(j *WatchJob) string {
	if j.Hash != "" {
		return j.Hash
	}
	return "title:" + j.Title
}

// Track starts watching a pack.
func (w *WatchFirst) Track(j WatchJob) {
	j.Hash = strings.ToLower(j.Hash)
	j.Created = time.Now()
	if j.Files == nil {
		j.Files = []WatchFile{}
	}
	j.State = "waiting"
	w.mu.Lock()
	w.jobs[jobKey(&j)] = &j
	w.mu.Unlock()
	w.save()
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Jobs returns a snapshot of all jobs.
func (w *WatchFirst) Jobs() []WatchJob {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]WatchJob, 0, len(w.jobs))
	for _, j := range w.jobs {
		cp := *j
		cp.Files = append([]WatchFile(nil), j.Files...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out
}

// Remove forgets a job by its key (the hash, or "title:..." before the hash
// is known).
func (w *WatchFirst) Remove(key string) {
	w.mu.Lock()
	delete(w.jobs, key)
	delete(w.jobs, strings.ToLower(key))
	w.mu.Unlock()
	w.save()
}

func (w *WatchFirst) save() {
	w.mu.Lock()
	jobs := make([]*WatchJob, 0, len(w.jobs))
	for _, j := range w.jobs {
		jobs = append(jobs, j)
	}
	b, err := json.MarshalIndent(jobs, "", " ")
	w.mu.Unlock()
	if err != nil {
		return
	}
	tmp := w.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, w.path)
	}
}

func (w *WatchFirst) loop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.kick:
		}
		w.tick(ctx)
	}
}

func (w *WatchFirst) tick(ctx context.Context) {
	c := w.app.C()
	if c.QBit == nil || c.Sonarr == nil {
		return
	}
	w.mu.Lock()
	var active []*WatchJob
	for _, j := range w.jobs {
		if j.State == "done" || j.State == "failed" {
			if time.Since(j.Created) > 7*24*time.Hour {
				delete(w.jobs, jobKey(j))
			}
			continue
		}
		active = append(active, j)
	}
	w.mu.Unlock()
	changed := false
	for _, j := range active {
		tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if w.step(tctx, c, j) {
			changed = true
		}
		cancel()
	}
	if changed {
		w.save()
	}
}

// step advances one job; it returns true when persisted state changed.
func (w *WatchFirst) step(ctx context.Context, c *Clients, j *WatchJob) bool {
	w.mu.Lock()
	hash := j.Hash
	w.mu.Unlock()

	if hash == "" {
		// Sonarr grabbed it from a .torrent link: learn the hash from its queue.
		q, err := c.Sonarr.Queue(ctx, "")
		if err != nil {
			return false
		}
		for _, r := range q {
			if r.SeriesID == j.SeriesID && strings.EqualFold(r.Title, j.Title) && r.DownloadID != "" {
				w.mu.Lock()
				delete(w.jobs, jobKey(j))
				j.Hash = strings.ToLower(r.DownloadID)
				w.jobs[j.Hash] = j
				w.mu.Unlock()
				hash = j.Hash
				break
			}
		}
		if hash == "" {
			return w.timeout(j, "Sonarr never sent the release to qBittorrent")
		}
	}

	ts, err := c.QBit.Torrents(ctx, hash)
	if err != nil {
		w.setMsg(j, err.Error())
		return false
	}
	if len(ts) == 0 {
		return w.timeout(j, "torrent never appeared in qBittorrent")
	}
	t := ts[0]
	files, err := c.QBit.Files(ctx, hash)
	if err != nil || len(files) == 0 {
		// A magnet has no file list until peers send the metadata, and a
		// stopped torrent never asks for it: start it now, prioritise later.
		if st := strings.ToLower(t.State); j.AddedPaused && !j.Resumed && (strings.Contains(st, "stopped") || strings.Contains(st, "paused")) {
			_ = c.QBit.Start(ctx, hash)
		}
		w.setMsg(j, "waiting for torrent metadata from peers")
		return w.timeoutAfter(j, 30*time.Minute, "no peer ever sent the torrent's metadata")
	}

	w.mu.Lock()
	j.Progress, j.Speed = t.Progress, t.DLSpeed
	first := !j.Started
	w.mu.Unlock()

	if first {
		w.plan(ctx, c, j, files)
	}
	w.syncProgress(j, files)
	w.prioritise(ctx, c, j)
	if first {
		if err := c.QBit.SetSequential(ctx, hash, watchSequential); err != nil {
			slog.Warn("watch-first: sequential toggle failed", "err", err)
		}
		w.mu.Lock()
		j.Started, j.State, j.Message = true, "downloading", ""
		w.mu.Unlock()
	}
	// roarr added the torrent stopped so priorities apply before the first
	// byte. Keep resuming until qBittorrent reports it running (it may still
	// be checking files on the first attempt).
	if j.AddedPaused && !j.Resumed {
		st := strings.ToLower(t.State)
		if strings.Contains(st, "paused") || strings.Contains(st, "stopped") {
			if err := c.QBit.Start(ctx, hash); err != nil {
				slog.Warn("watch-first: could not start torrent", "err", err)
			}
		} else if !strings.Contains(st, "checking") {
			w.mu.Lock()
			j.Resumed = true
			w.mu.Unlock()
		}
	}
	w.importReady(ctx, c, j, t)
	if t.Progress >= 1 {
		// The *arr's own completed-download handling may have imported the
		// rest already; count those as done.
		w.markImportedBySonarr(ctx, j)
		w.mu.Lock()
		if j.CompletedAt.IsZero() {
			j.CompletedAt = time.Now()
		}
		w.mu.Unlock()
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	pending := 0
	for _, f := range j.Files {
		if !f.Skipped && !f.Imported && f.Error == "" {
			pending++
		}
	}
	switch {
	case pending == 0:
		j.State, j.Message = "done", ""
	case !j.CompletedAt.IsZero() && time.Since(j.CompletedAt) > 10*time.Minute:
		j.State, j.Message = "failed", fmt.Sprintf("%d episode(s) were never imported; check Sonarr's queue", pending)
	}
	return true
}

// markImportedBySonarr flags files whose episodes Sonarr already has.
func (w *WatchFirst) markImportedBySonarr(ctx context.Context, j *WatchJob) {
	d, err := w.app.Series(ctx, j.SeriesID, true)
	if err != nil {
		return
	}
	has := map[int]bool{}
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			has[e.ID] = e.HasFile
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range j.Files {
		f := &j.Files[i]
		if f.Imported || f.Skipped || len(f.EpisodeIDs) == 0 {
			continue
		}
		all := true
		for _, id := range f.EpisodeIDs {
			all = all && has[id]
		}
		if all {
			f.Imported = true
			f.ImportedAt = time.Now().Format(time.RFC3339)
		}
	}
}

func (w *WatchFirst) timeout(j *WatchJob, msg string) bool {
	return w.timeoutAfter(j, 10*time.Minute, msg)
}

func (w *WatchFirst) timeoutAfter(j *WatchJob, d time.Duration, msg string) bool {
	if time.Since(j.Created) < d {
		return false
	}
	w.mu.Lock()
	j.State, j.Message = "failed", msg
	w.mu.Unlock()
	return true
}

func (w *WatchFirst) setMsg(j *WatchJob, msg string) {
	w.mu.Lock()
	j.Message = msg
	w.mu.Unlock()
}

var videoExt = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".m4v": true, ".ts": true, ".wmv": true, ".mov": true, ".webm": true}

// plan maps torrent files to episodes and decides which to skip.
func (w *WatchFirst) plan(ctx context.Context, c *Clients, j *WatchJob, files []qbit.File) {
	d, err := w.app.Series(ctx, j.SeriesID, true)
	if err != nil {
		return
	}
	show := showForMatching(d)
	have := map[int]bool{}
	for _, e := range show.Episodes {
		have[e.ID] = e.HasFile
	}
	wanted := map[int]bool{}
	for _, id := range j.Wanted {
		wanted[id] = true
	}
	packInfo := release.Parse(j.Title)
	var wf []WatchFile
	var skip []int
	for _, f := range files {
		// qBittorrent marks unfinished files "name.mkv.!qB" until each completes.
		name := strings.TrimSuffix(f.Name, ".!qB")
		base := path.Base(name)
		ext := strings.ToLower(path.Ext(base))
		if !videoExt[ext] {
			continue
		}
		info := release.Parse(base)
		if info.Sample || strings.Contains(strings.ToLower(name), "/sample") {
			skip = append(skip, f.Index)
			continue
		}
		// Files inside a season folder often carry only "E05"; borrow the
		// season from the pack name.
		if len(info.Seasons) == 0 && len(info.Absolute) == 0 && len(packInfo.Seasons) == 1 {
			if n := episodeOnly(base); n > 0 {
				info.Seasons, info.Episodes = packInfo.Seasons, []int{n}
			}
		}
		ids := release.Covers(info, show)
		if len(ids) == 0 {
			continue // extras, featurettes
		}
		file := WatchFile{Index: f.Index, Name: name, Size: f.Size, EpisodeIDs: ids}
		for _, e := range show.Episodes {
			if e.ID == ids[0] {
				file.Season, file.Episode = e.Season, e.Number
			}
		}
		need := false
		for _, id := range ids {
			if wanted[id] || !have[id] {
				need = true
			}
		}
		if !need {
			file.Skipped = true
			skip = append(skip, f.Index)
		}
		wf = append(wf, file)
	}
	sort.Slice(wf, func(a, b int) bool {
		if wf[a].Season != wf[b].Season {
			return wf[a].Season < wf[b].Season
		}
		return wf[a].Episode < wf[b].Episode
	})
	if err := c.QBit.SetFilePriority(ctx, j.Hash, skip, qbit.PrioSkip); err != nil {
		slog.Warn("watch-first: could not skip files", "err", err)
	}
	if wf == nil {
		wf = []WatchFile{}
	}
	w.mu.Lock()
	j.Files = wf
	w.mu.Unlock()
}

func episodeOnly(name string) int {
	l := strings.ToLower(name)
	for i := 0; i+2 < len(l); i++ {
		if l[i] == 'e' && (i == 0 || l[i-1] < 'a' || l[i-1] > 'z') && l[i+1] >= '0' && l[i+1] <= '9' {
			n := 0
			k := i + 1
			for k < len(l) && l[k] >= '0' && l[k] <= '9' && k < i+5 {
				n = n*10 + int(l[k]-'0')
				k++
			}
			return n
		}
	}
	return 0
}

func (w *WatchFirst) syncProgress(j *WatchJob, files []qbit.File) {
	byIdx := map[int]qbit.File{}
	for _, f := range files {
		byIdx[f.Index] = f
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range j.Files {
		if f, ok := byIdx[j.Files[i].Index]; ok {
			j.Files[i].Progress = f.Progress
			j.Files[i].Priority = f.Priority
		}
	}
}

// prioritise applies the rolling window: the next unfinished episode gets
// maximum priority, the one after it high, the rest normal.
func (w *WatchFirst) prioritise(ctx context.Context, c *Clients, j *WatchJob) {
	w.mu.Lock()
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
	hash := j.Hash
	w.mu.Unlock()
	byPrio := map[int][]int{}
	for idx, p := range want {
		byPrio[p] = append(byPrio[p], idx)
	}
	for p, ids := range byPrio {
		if err := c.QBit.SetFilePriority(ctx, hash, ids, p); err != nil {
			slog.Warn("watch-first: priority update failed", "err", err)
		}
	}
}

// importReady imports every completed, not yet imported episode file.
func (w *WatchFirst) importReady(ctx context.Context, c *Clients, j *WatchJob, t qbit.Torrent) {
	w.mu.Lock()
	var ready []WatchFile
	for _, f := range j.Files {
		if !f.Skipped && !f.Imported && f.Error == "" && f.Progress >= 1 {
			ready = append(ready, f)
		}
	}
	hash, seriesID := j.Hash, j.SeriesID
	w.mu.Unlock()
	if len(ready) == 0 {
		return
	}
	w.setMsg(j, "")
	// Sonarr knows downloads it grabbed (or has seen in its category) by
	// id; for anything else scan the torrent's folder directly. That works
	// when Sonarr and qBittorrent see the same paths (the usual /data layout)
	// or Sonarr has a remote path mapping for qBittorrent's.
	items, err := c.Sonarr.ManualImportCandidates(ctx, strings.ToUpper(hash), "")
	tracked := err == nil && len(items) > 0
	if !tracked && t.ContentPath != "" {
		folder := t.ContentPath
		if len(j.Files) > 0 && !strings.Contains(j.Files[0].Name, "/") {
			folder = t.SavePath // single-folder-less torrent
		}
		if ms, mErr := c.Sonarr.RemotePathMappings(ctx); mErr == nil && len(ms) > 0 {
			host := ""
			if u, pErr := url.Parse(c.QBit.Base); pErr == nil {
				host = u.Hostname()
			}
			folder = arr.MapRemotePath(ms, host, folder)
		}
		items, err = c.Sonarr.ManualImportCandidates(ctx, "", folder)
	}
	if err != nil {
		w.setMsg(j, "Sonarr cannot see the download yet: "+err.Error())
		return
	}
	var batch []arr.ManualImportItem
	var batchFiles []int
	results := map[int]string{}
	for _, f := range ready {
		it, ok := matchImportItem(items, f.Name)
		if !ok {
			continue // Sonarr has not seen the finished file yet
		}
		if reason := permanentRejection(it); reason != "" {
			results[f.Index] = reason
			continue
		}
		if len(it.Episodes) == 0 {
			ids := f.EpisodeIDs
			for _, id := range ids {
				it.Episodes = append(it.Episodes, arr.Episode{ID: id})
			}
		}
		// Sonarr imports the file but then fails the command with a null
		// reference when the download id names a download it does not track.
		if tracked {
			it.DownloadID = strings.ToUpper(hash)
		}
		batch = append(batch, it)
		batchFiles = append(batchFiles, f.Index)
	}
	if len(batch) > 0 {
		cmd, err := c.Sonarr.ImportFiles(ctx, batch, seriesID)
		if err != nil {
			slog.Warn("watch-first: import failed", "err", err)
			return
		}
		wctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		done, err := c.Sonarr.WaitCommand(wctx, cmd.ID)
		cancel()
		if err != nil || done.Status != "completed" {
			msg := "import failed"
			if done.Message != "" {
				msg = done.Message
			}
			for _, idx := range batchFiles {
				results[idx] = msg
			}
		} else {
			for _, idx := range batchFiles {
				results[idx] = ""
			}
			w.refreshJellyfin(j)
		}
	}
	w.mu.Lock()
	now := time.Now()
	for i := range j.Files {
		msg, ok := results[j.Files[i].Index]
		if !ok {
			continue
		}
		if msg == "" {
			j.Files[i].Imported = true
			j.Files[i].ImportedAt = now.Format(time.RFC3339)
			if j.FirstReady.IsZero() {
				j.FirstReady = now
			}
		} else {
			j.Files[i].Error = msg
		}
	}
	w.mu.Unlock()
	if len(batchFiles) > 0 {
		slog.Info("watch-first: imported episodes", "title", j.SeriesTitle, "files", len(batchFiles))
		w.app.forget(GrabRequest{SeriesID: seriesID})
	}
}

func matchImportItem(items []arr.ManualImportItem, name string) (arr.ManualImportItem, bool) {
	base := path.Base(name)
	for _, it := range items {
		p := filepath.ToSlash(it.Path)
		if strings.HasSuffix(p, "/"+name) || strings.HasSuffix(p, "/"+base) || it.RelativePath == name || it.Name == strings.TrimSuffix(base, path.Ext(base)) {
			return it, true
		}
	}
	return arr.ManualImportItem{}, false
}

func permanentRejection(it arr.ManualImportItem) string {
	for _, r := range it.Rejections {
		if strings.EqualFold(r.Type, "permanent") {
			return r.Reason
		}
	}
	return ""
}

// refreshJellyfin nudges Jellyfin to pick up new episodes. Refreshes are
// coalesced per show (at most one every 15s), but a refresh is always
// scheduled after the latest import so the last episode is never missed.
func (w *WatchFirst) refreshJellyfin(j *WatchJob) {
	c := w.app.C()
	if c.Jellyfin == nil || w.app.Settings.Get().Jellyfin.APIKey == "" {
		return
	}
	id, title, tvdb := j.SeriesID, j.SeriesTitle, j.TvdbID
	w.mu.Lock()
	if w.jfPending[id] {
		w.mu.Unlock()
		return // a refresh is already scheduled and will see this file
	}
	// Give Sonarr a moment to finish moving the file into place.
	wait := 2 * time.Second
	if since := time.Since(w.jfAt[id]); since < 15*time.Second {
		wait += 15*time.Second - since
	}
	w.jfPending[id] = true
	w.mu.Unlock()
	go func() {
		time.Sleep(wait)
		w.mu.Lock()
		w.jfPending[id] = false
		w.jfAt[id] = time.Now()
		w.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		how, err := c.Jellyfin.Refresh(ctx, "Series", title, "Tvdb", strconv.Itoa(tvdb))
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("jellyfin refresh failed", "err", err)
			return
		}
		slog.Info("jellyfin refreshed", "title", title, "how", how)
	}()
}
