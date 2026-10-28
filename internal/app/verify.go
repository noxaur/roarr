package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/httpx"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/verify"
)

// Media verification: every download the *arrs queue is followed until its
// files are on disk, then checked with ffprobe/ffmpeg (internal/verify). A
// pack is judged by its first episode as soon as that file is complete, so a
// fake is caught long before the whole pack has downloaded. A release that
// fails is hidden from roarr's picks; it can be re-grabbed, automatically
// when Verify.AutoRegrab is on.

// VerifyRecord is one download followed by the verifier.
type VerifyRecord struct {
	Hash       string          `json:"hash"`   // download id, lowercase
	Source     string          `json:"source"` // sonarr | radarr
	Kind       string          `json:"kind"`   // tv | movie
	SeriesID   int             `json:"seriesId,omitempty"`
	MovieID    int             `json:"movieId,omitempty"`
	EpisodeIDs []int           `json:"episodeIds,omitempty"`
	Title      string          `json:"title"`
	Release    string          `json:"release"`
	Added      time.Time       `json:"added"`
	State      string          `json:"state"` // pending | checking | done
	Verdict    *verify.Verdict `json:"verdict,omitempty"`
	Dismissed  bool            `json:"dismissed,omitempty"` // a person accepted it despite the verdict
	Rejected   bool            `json:"rejected,omitempty"`  // thrown away and re-grabbed, whatever the verdict
	Regrab     *VerifyRegrab   `json:"regrab,omitempty"`

	outputPath string // the *arr's download folder, when the client is not qBittorrent
	seen       time.Time
	triedEarly bool // checked once before the torrent finished
}

// Failed reports a failing verdict nobody has dismissed.
func (r VerifyRecord) Failed() bool {
	return r.Verdict != nil && r.Verdict.Level == verify.Fail && !r.Dismissed
}

// Blocked reports a release roarr must not pick again.
func (r VerifyRecord) Blocked() bool { return r.Rejected || r.Failed() }

// VerifyRegrab records a replacement grab.
type VerifyRegrab struct {
	At      time.Time   `json:"at"`
	Auto    bool        `json:"auto"` // started by AutoRegrab, not a person
	Message string      `json:"message"`
	Note    string      `json:"note,omitempty"` // which library files were deleted or kept
	Error   string      `json:"error,omitempty"`
	Result  *AutoResult `json:"result,omitempty"`
}

// Verification follows downloads and verifies them.
type Verification struct {
	app   *App
	path  string
	tools verify.Tools
	sem   chan struct{} // one verification at a time: ffmpeg is heavy

	mu   sync.Mutex
	recs map[string]*VerifyRecord
	// saveMu makes snapshot, write and rename one step, so an older
	// snapshot never replaces a newer file.
	saveMu sync.Mutex
}

func newVerification(a *App, dir string) *Verification {
	v := &Verification{app: a, path: filepath.Join(dir, "verify.json"), tools: verify.FindTools(), sem: make(chan struct{}, 1), recs: map[string]*VerifyRecord{}}
	if b, err := os.ReadFile(v.path); err == nil {
		var recs []*VerifyRecord
		if json.Unmarshal(b, &recs) == nil {
			for _, r := range recs {
				if r.State == "checking" {
					r.State = "pending"
				}
				v.recs[r.Hash] = r
			}
		}
	}
	if v.tools.FFprobe == "" {
		slog.Info("media verification: ffprobe not found; off unless VERIFY_MEDIA=true and FFPROBE_PATH is set")
	}
	return v
}

// Tools reports the ffprobe/ffmpeg binaries in use.
func (v *Verification) Tools() verify.Tools { return v.tools }

// Enabled reports whether downloads are verified: the setting, or when it
// is unset, whether ffprobe is installed.
func (v *Verification) Enabled() bool {
	if v == nil {
		return false
	}
	st := v.app.Settings.Get().Verify
	if st.Enabled != nil {
		return *st.Enabled && v.tools.FFprobe != ""
	}
	return v.tools.FFprobe != ""
}

// Records lists records, newest first. seriesID/movieID filter when non-zero.
func (v *Verification) Records(seriesID, movieID int) []VerifyRecord {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]VerifyRecord, 0, len(v.recs))
	for _, r := range v.recs {
		if seriesID != 0 && r.SeriesID != seriesID || movieID != 0 && r.MovieID != movieID {
			continue
		}
		out = append(out, copyRecord(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Added.After(out[j].Added) })
	return out
}

// Record returns one record by download id.
func (v *Verification) Record(hash string) (VerifyRecord, bool) {
	if v == nil {
		return VerifyRecord{}, false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	r, ok := v.recs[strings.ToLower(hash)]
	if !ok {
		return VerifyRecord{}, false
	}
	return copyRecord(r), true
}

func copyRecord(r *VerifyRecord) VerifyRecord {
	cp := *r
	cp.EpisodeIDs = append([]int(nil), r.EpisodeIDs...)
	return cp
}

// annotate marks a search result whose release failed verification, so the
// planner skips it and the release list shows why.
func (v *Verification) annotate(rel Release) Release {
	if v == nil {
		return rel
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, r := range v.recs {
		if !r.Blocked() {
			continue
		}
		if strings.EqualFold(r.Release, rel.Title) || rel.InfoHash != "" && strings.EqualFold(r.Hash, rel.InfoHash) {
			reason := "thrown away after download"
			if r.Failed() {
				reason = "failed verification: " + r.Verdict.Summary
			}
			rel.Rejected = true
			rel.Reasons = append(rel.Reasons, reason)
			return rel
		}
	}
	return rel
}

// Dismiss accepts a download despite its verdict; the release is no longer
// hidden.
func (v *Verification) Dismiss(hash string) (VerifyRecord, error) {
	v.mu.Lock()
	r, ok := v.recs[strings.ToLower(hash)]
	if ok {
		r.Dismissed, r.Rejected = true, false
	}
	v.mu.Unlock()
	if !ok {
		return VerifyRecord{}, fmt.Errorf("download %s not verified", hash)
	}
	v.save()
	return v.mustRecord(hash), nil
}

func (v *Verification) mustRecord(hash string) VerifyRecord {
	r, _ := v.Record(hash)
	return r
}

func (v *Verification) save() {
	v.saveMu.Lock()
	defer v.saveMu.Unlock()
	v.mu.Lock()
	recs := make([]*VerifyRecord, 0, len(v.recs))
	for _, r := range v.recs {
		recs = append(recs, r)
	}
	b, err := json.MarshalIndent(recs, "", " ")
	v.mu.Unlock()
	if err != nil {
		return
	}
	tmp := v.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, v.path)
	}
}

func (v *Verification) loop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !v.Enabled() {
			continue
		}
		tctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
		v.tick(tctx)
		cancel()
	}
}

// tick learns new downloads from the queues and verifies the ready ones.
func (v *Verification) tick(ctx context.Context) {
	c := v.app.C()
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	if c.Sonarr != nil {
		if q, err := c.Sonarr.Queue(qctx, "includeSeries"); err == nil {
			v.learn("sonarr", q)
		}
	}
	if c.Radarr != nil {
		if q, err := c.Radarr.Queue(qctx, "includeMovie"); err == nil {
			v.learn("radarr", q)
		}
	}
	cancel()
	v.prune()

	v.mu.Lock()
	var pending []*VerifyRecord
	for _, r := range v.recs {
		if r.State == "pending" {
			cp := *r
			pending = append(pending, &cp)
		}
	}
	v.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	torrents := map[string]qbit.Torrent{}
	if c.QBit != nil {
		var hashes []string
		for _, r := range pending {
			if len(r.Hash) == 40 {
				hashes = append(hashes, r.Hash)
			}
		}
		if len(hashes) > 0 {
			tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			ts, err := c.QBit.Torrents(tctx, hashes...)
			cancel()
			if err == nil {
				for _, t := range ts {
					torrents[strings.ToLower(t.Hash)] = t
				}
			}
		}
	}
	pathMap := parsePathMap(v.app.Settings.Get().Verify.PathMap)
	for _, r := range pending {
		if ctx.Err() != nil {
			return
		}
		var files []verify.File
		ready, final := false, true
		if t, ok := torrents[r.Hash]; ok {
			files, ready = v.torrentFiles(ctx, c, t, r.Kind, pathMap)
			final = t.Progress >= 1
			if !final && r.triedEarly {
				ready = false
			}
			if t.AddedOn > 0 {
				v.update(r.Hash, func(rec *VerifyRecord) { rec.Added = time.Unix(t.AddedOn, 0) })
			}
		} else if r.outputPath != "" {
			files = walkFiles(mapPath(pathMap, r.outputPath))
			ready = len(files) > 0
		}
		if !ready {
			continue
		}
		vd := v.run(ctx, r.Hash, files, true)
		if vd != nil && vd.Level == verify.Skip && !final {
			// The finished first episode may still sit in qBittorrent's
			// incomplete folder; look again once the torrent is done.
			v.update(r.Hash, func(rec *VerifyRecord) { rec.State, rec.triedEarly = "pending", true })
		}
	}
}

// learn adds new downloads from an *arr queue.
func (v *Verification) learn(source string, q []arr.QueueRecord) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	for _, it := range q {
		hash := strings.ToLower(it.DownloadID)
		if hash == "" {
			continue
		}
		r, ok := v.recs[hash]
		if !ok {
			r = &VerifyRecord{Hash: hash, Source: source, Release: it.Title, Added: now, State: "pending"}
			if source == "sonarr" {
				r.Kind, r.SeriesID = "tv", it.SeriesID
				if it.Series != nil {
					r.Title = it.Series.Title
				}
			} else {
				r.Kind, r.MovieID = "movie", it.MovieID
				if it.Movie != nil {
					r.Title = it.Movie.Title
				}
			}
			v.recs[hash] = r
		}
		r.seen = now
		ep := it.EpisodeID
		if ep == 0 && it.Episode != nil {
			ep = it.Episode.ID
		}
		if ep != 0 && !containsInt(r.EpisodeIDs, ep) {
			r.EpisodeIDs = append(r.EpisodeIDs, ep)
		}
		if st := strings.ToLower(it.Status); st == "completed" && it.OutputPath != "" {
			r.outputPath = it.OutputPath
		}
	}
}

func containsInt(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// prune forgets verdicts after 30 days and downloads that never finished.
func (v *Verification) prune() {
	v.mu.Lock()
	changed := false
	for k, r := range v.recs {
		old := r.State == "done" && time.Since(r.Added) > 30*24*time.Hour ||
			r.State == "pending" && time.Since(r.Added) > 7*24*time.Hour && time.Since(r.seen) > time.Hour
		if old {
			delete(v.recs, k)
			changed = true
		}
	}
	v.mu.Unlock()
	if changed {
		v.save()
	}
}

func (v *Verification) update(hash string, fn func(*VerifyRecord)) {
	v.mu.Lock()
	if r, ok := v.recs[hash]; ok {
		fn(r)
	}
	v.mu.Unlock()
}

// torrentFiles lists a torrent's files as roarr sees them, and whether the
// download is ready to judge: finished, or its first episode is, or its file
// list alone already shows junk.
func (v *Verification) torrentFiles(ctx context.Context, c *Clients, t qbit.Torrent, kind string, pm []pathPair) ([]verify.File, bool) {
	fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	qf, err := c.QBit.Files(fctx, t.Hash)
	cancel()
	if err != nil || len(qf) == 0 {
		return nil, false
	}
	files := make([]verify.File, 0, len(qf))
	progress := map[string]float64{}
	for _, f := range qf {
		name := strings.TrimSuffix(filepath.ToSlash(f.Name), ".!qB")
		// Episodes watch-first skipped are never downloaded; judge the rest.
		// Other skipped files still count: a deselected .exe is still junk.
		if f.Priority == qbit.PrioSkip && videoExt[strings.ToLower(path.Ext(name))] {
			continue
		}
		files = append(files, verify.File{Name: name, Size: f.Size, Path: mapPath(pm, joinClientPath(t.SavePath, name))})
		progress[name] = f.Progress
	}
	if t.Progress >= 1 {
		return files, true
	}
	payload, videos := verify.CheckPayload(files)
	if payload.Level == verify.Fail {
		return files, true
	}
	if p, ok := verify.PickPrimary(videos, kind); ok && progress[p.Name] >= 1 {
		return files, true
	}
	return files, false
}

// run verifies one download and acts on the verdict.
func (v *Verification) run(ctx context.Context, hash string, files []verify.File, auto bool) *verify.Verdict {
	select {
	case v.sem <- struct{}{}:
	case <-ctx.Done():
		return nil
	}
	defer func() { <-v.sem }()

	r, ok := v.Record(hash)
	if !ok {
		return nil
	}
	v.update(hash, func(rec *VerifyRecord) { rec.State = "checking" })
	vf := verify.New(v.tools)
	vd := vf.Verify(ctx, verify.Target{Release: r.Release, Kind: r.Kind, RuntimeMin: v.runtime(ctx, r), Files: files})
	if ctx.Err() != nil {
		v.update(hash, func(rec *VerifyRecord) { rec.State = "pending" })
		return nil
	}
	v.update(hash, func(rec *VerifyRecord) { rec.State, rec.Verdict = "done", &vd })
	v.save()
	attrs := []any{"title", r.Title, "release", r.Release, "level", vd.Level, "summary", vd.Summary, "took", time.Duration(vd.TookMS) * time.Millisecond}
	switch vd.Level {
	case verify.Fail:
		slog.Warn("media verification failed", attrs...)
	case verify.Warn:
		slog.Info("media verification warning", attrs...)
	default:
		slog.Info("media verified", attrs...)
	}
	if auto && vd.Level == verify.Fail && v.app.Settings.Get().Verify.AutoRegrab {
		go func() {
			gctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if _, err := v.Regrab(gctx, hash, true); err != nil {
				slog.Warn("media verification: re-grab failed", "release", r.Release, "err", err)
			}
		}()
	}
	return &vd
}

// runtime is the expected minutes per episode or for the movie.
func (v *Verification) runtime(ctx context.Context, r VerifyRecord) int {
	c := v.app.C()
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch {
	case r.Kind == "tv" && r.SeriesID != 0 && c.Sonarr != nil:
		if s, err := c.Sonarr.Series(rctx, r.SeriesID); err == nil {
			return s.Runtime
		}
	case r.Kind == "movie" && r.MovieID != 0 && c.Radarr != nil:
		if m, err := c.Radarr.Movie(rctx, r.MovieID); err == nil {
			return m.Runtime
		}
	}
	return 0
}

// Recheck verifies a download again now.
func (v *Verification) Recheck(ctx context.Context, hash string) (VerifyRecord, error) {
	hash = strings.ToLower(hash)
	r, ok := v.Record(hash)
	if !ok {
		return VerifyRecord{}, fmt.Errorf("download %s not verified", hash)
	}
	if v.tools.FFprobe == "" {
		return VerifyRecord{}, fmt.Errorf("ffprobe %w", ErrNotConfigured)
	}
	c := v.app.C()
	pm := parsePathMap(v.app.Settings.Get().Verify.PathMap)
	var files []verify.File
	if c.QBit != nil && len(hash) == 40 {
		ts, err := c.QBit.Torrents(ctx, hash)
		if err != nil {
			return VerifyRecord{}, err
		}
		if len(ts) > 0 {
			files, _ = v.torrentFiles(ctx, c, ts[0], r.Kind, pm)
		}
	}
	if files == nil && r.outputPath != "" {
		files = walkFiles(mapPath(pm, r.outputPath))
	}
	if files == nil {
		return VerifyRecord{}, errors.New("the download's files are gone")
	}
	if v.run(ctx, hash, files, false) == nil {
		return VerifyRecord{}, ctx.Err()
	}
	return v.mustRecord(hash), nil
}

// Regrab throws a download away and grabs the next best release. In order,
// and stopping at the first failure: the download is removed from the *arr
// queue with blocklisting and from qBittorrent (both confirmed), library
// files the *arr's history shows it imported are deleted, and roarr's
// planner picks again. The failed release stays hidden from roarr's picks.
func (v *Verification) Regrab(ctx context.Context, hash string, auto bool) (VerifyRecord, error) {
	hash = strings.ToLower(hash)
	r, ok := v.Record(hash)
	if !ok {
		return VerifyRecord{}, fmt.Errorf("download %s not verified", hash)
	}
	c := v.app.C()
	// Hide the release from roarr's picks even if it only warned: a person
	// asked for something else.
	v.update(hash, func(rec *VerifyRecord) { rec.Rejected, rec.Dismissed = true, false })
	if err := v.removeDownload(ctx, c, r); err != nil {
		return v.finishRegrab(hash, auto, "", nil, err)
	}
	deleted, kept, err := v.deleteImported(ctx, c, r)
	if err != nil {
		err = fmt.Errorf("the download is removed, but its imported files could not be checked or deleted: %w", err)
		return v.finishRegrab(hash, auto, "", nil, err)
	}
	note := ""
	if deleted > 0 {
		note = fmt.Sprintf("Deleted %d imported file(s). ", deleted)
	}
	if kept > 0 {
		note += fmt.Sprintf("Kept %d library file(s): no import history links them to this download. ", kept)
	}
	note = strings.TrimSpace(note)
	finish := func(res *AutoResult, err error) (VerifyRecord, error) {
		return v.finishRegrab(hash, auto, note, res, err)
	}
	var req AutoRequest
	switch r.Kind {
	case "tv":
		if r.SeriesID == 0 || len(r.EpisodeIDs) == 0 {
			return finish(nil, errors.New("Sonarr did not say which episodes this download was for"))
		}
		req = AutoRequest{Kind: "tv", SeriesID: r.SeriesID, EpisodeIDs: r.EpisodeIDs}
	case "movie":
		if c.Radarr == nil {
			return finish(nil, fmt.Errorf("radarr %w", ErrNotConfigured))
		}
		m, err := c.Radarr.Movie(ctx, r.MovieID)
		if err != nil {
			return finish(nil, err)
		}
		req = AutoRequest{Kind: "movie", MovieTmdb: []int{m.TmdbID}}
	}
	if req.Kind == "tv" {
		// Episodes the library had before this download keep their files;
		// only what is missing now is grabbed again.
		if d, err := v.app.Series(ctx, r.SeriesID, true); err == nil {
			has := map[int]bool{}
			for _, s := range d.Seasons {
				for _, e := range s.Episodes {
					has[e.ID] = e.HasFile
				}
			}
			var missing []int
			for _, id := range r.EpisodeIDs {
				if !has[id] {
					missing = append(missing, id)
				}
			}
			if len(missing) == 0 {
				return finish(&AutoResult{Message: "Nothing is missing."}, nil)
			}
			req.EpisodeIDs = missing
		}
	}
	res, err := v.app.Auto(ctx, req)
	return finish(&res, err)
}

func (v *Verification) finishRegrab(hash string, auto bool, note string, res *AutoResult, err error) (VerifyRecord, error) {
	g := &VerifyRegrab{At: time.Now(), Auto: auto, Note: note, Result: res}
	if res != nil {
		g.Message = res.Message
	}
	if err != nil {
		g.Error = err.Error()
	}
	v.update(hash, func(rec *VerifyRecord) { rec.Regrab = g })
	v.save()
	rec := v.mustRecord(hash)
	if g.Error == "" {
		slog.Info("media verification: re-grabbed", "release", rec.Release, "auto", auto, "message", g.Message, "note", note)
	}
	return rec, err
}

// removalChecks are the waits before each look that confirms a removal;
// the *arr and qBittorrent delete asynchronously.
var removalChecks = []time.Duration{0, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}

// removeDownload takes the download out of the *arr queue (blocklisting it)
// and out of qBittorrent, and confirms both. Nothing in the library may be
// touched before it returns nil.
func (v *Verification) removeDownload(ctx context.Context, c *Clients, r VerifyRecord) error {
	var cl *arr.Client
	switch {
	case r.Source == "sonarr" && c.Sonarr != nil:
		cl = c.Sonarr.Client
	case r.Source == "radarr" && c.Radarr != nil:
		cl = c.Radarr.Client
	}
	if cl == nil {
		return fmt.Errorf("%s %w", r.Source, ErrNotConfigured)
	}
	app := cl.Service
	inQueue := func() ([]int, error) {
		q, err := cl.Queue(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("could not read the %s queue, so nothing was removed or deleted; retry when %s answers: %w", app, app, err)
		}
		var ids []int
		for _, it := range q {
			if strings.EqualFold(it.DownloadID, r.Hash) {
				ids = append(ids, it.ID)
			}
		}
		return ids, nil
	}
	ids, err := inQueue()
	if err != nil {
		return err
	}
	for i, id := range ids {
		// The first removal deletes the download and blocklists it.
		err := cl.RemoveQueue(ctx, id, i == 0, i == 0)
		var he *httpx.Error
		if err != nil && !(errors.As(err, &he) && he.Status == http.StatusNotFound) {
			return fmt.Errorf("%s would not remove the download from its queue, so no library files were deleted; remove it in %s and retry: %w", app, app, err)
		}
	}
	if len(ids) > 0 {
		if err := confirmGone(ctx, func() (bool, error) { ids, err := inQueue(); return len(ids) > 0, err }); err != nil {
			return fmt.Errorf("the download is still in the %s queue, so no library files were deleted; remove it in %s and retry: %w", app, app, err)
		}
	}
	if c.QBit != nil && len(r.Hash) == 40 {
		if err := c.QBit.Delete(ctx, r.Hash, true); err != nil {
			return fmt.Errorf("qBittorrent would not delete the torrent, so no library files were deleted; delete it in qBittorrent and retry: %w", err)
		}
		err := confirmGone(ctx, func() (bool, error) {
			ts, err := c.QBit.Torrents(ctx, r.Hash)
			if err != nil {
				return false, fmt.Errorf("could not ask qBittorrent: %w", err)
			}
			return len(ts) > 0, nil
		})
		if err != nil {
			return fmt.Errorf("the torrent is still in qBittorrent, so no library files were deleted; delete it there and retry: %w", err)
		}
	}
	v.app.Watch.Remove(r.Hash)
	return nil
}

// confirmGone polls present until it reports false.
func confirmGone(ctx context.Context, present func() (bool, error)) error {
	var last error
	for _, wait := range removalChecks {
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		still, err := present()
		if err == nil && !still {
			return nil
		}
		last = err
	}
	if last != nil {
		return last
	}
	return errors.New("still present after removal")
}

// deleteImported deletes library files that this download put there.
// Evidence is the *arr's import history: a file is deleted only when the
// latest import for its episode or movie came from this download id and
// the file is still the one that import placed. Anything else stays, and is
// counted in kept.
func (v *Verification) deleteImported(ctx context.Context, c *Clients, r VerifyRecord) (deleted, kept int, err error) {
	switch r.Kind {
	case "tv":
		if c.Sonarr == nil || r.SeriesID == 0 {
			return 0, 0, nil
		}
		eps, err := c.Sonarr.Episodes(ctx, r.SeriesID)
		if err != nil {
			return 0, 0, err
		}
		fileOf := map[int]int{}
		for _, e := range eps {
			if e.EpisodeFileID != 0 && containsInt(r.EpisodeIDs, e.ID) {
				fileOf[e.ID] = e.EpisodeFileID
			}
		}
		if len(fileOf) == 0 {
			return 0, 0, nil
		}
		files, err := c.Sonarr.EpisodeFiles(ctx, r.SeriesID)
		if err != nil {
			return 0, 0, err
		}
		pathOf := map[int]string{}
		for _, f := range files {
			pathOf[f.ID] = f.Path
		}
		del := map[int]bool{}
		for epID, fileID := range fileOf {
			hist, err := c.Sonarr.EpisodeHistory(ctx, r.SeriesID, epID)
			if err != nil {
				return 0, 0, err
			}
			if p, ok := importedBy(hist, r.Hash); ok && samePath(p, pathOf[fileID]) {
				del[fileID] = true
			} else {
				kept++
			}
		}
		ids := make([]int, 0, len(del))
		for id := range del {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		if err := c.Sonarr.DeleteEpisodeFiles(ctx, ids); err != nil {
			return 0, kept, err
		}
		v.app.forget(GrabRequest{SeriesID: r.SeriesID})
		return len(ids), kept, nil
	case "movie":
		if c.Radarr == nil || r.MovieID == 0 {
			return 0, 0, nil
		}
		m, err := c.Radarr.Movie(ctx, r.MovieID)
		if err != nil {
			return 0, 0, err
		}
		if m.MovieFile == nil || m.MovieFile.ID == 0 {
			return 0, 0, nil
		}
		hist, err := c.Radarr.MovieHistory(ctx, r.MovieID)
		if err != nil {
			return 0, 0, err
		}
		if p, ok := importedBy(hist, r.Hash); !ok || !samePath(p, m.MovieFile.Path) {
			return 0, 1, nil
		}
		if err := c.Radarr.DeleteMovieFile(ctx, m.MovieFile.ID); err != nil {
			return 0, 0, err
		}
		return 1, 0, nil
	}
	return 0, 0, nil
}

// importedBy returns the path of the file the latest import put in place,
// when that import came from downloadID.
func importedBy(hist []arr.HistoryRecord, downloadID string) (string, bool) {
	var latest *arr.HistoryRecord
	for i := range hist {
		h := &hist[i]
		switch h.EventType {
		case "downloadFolderImported", "seriesFolderImported", "movieFolderImported":
			if latest == nil || h.Date.After(latest.Date) {
				latest = h
			}
		}
	}
	if latest == nil || latest.EventType != "downloadFolderImported" || !strings.EqualFold(latest.DownloadID, downloadID) {
		return "", false
	}
	p := latest.Data["importedPath"]
	return p, p != ""
}

func samePath(a, b string) bool {
	return a != "" && normClientPath(a) == normClientPath(b)
}

// pathPair maps a download-client path prefix to roarr's.
type pathPair struct{ from, to string }

// parsePathMap reads "client=roarr,client2=roarr2".
func parsePathMap(s string) []pathPair {
	var out []pathPair
	for _, part := range strings.Split(s, ",") {
		from, to, ok := strings.Cut(part, "=")
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if !ok || from == "" || to == "" {
			continue
		}
		out = append(out, pathPair{normClientPath(from), to})
	}
	// Longest prefix first.
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].from) > len(out[j].from) })
	return out
}

func normClientPath(p string) string {
	return strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
}

// mapPath rewrites a client path with the first matching prefix.
func mapPath(pm []pathPair, p string) string {
	p = normClientPath(p)
	for _, m := range pm {
		if p == m.from || strings.HasPrefix(p, m.from+"/") {
			return filepath.FromSlash(strings.TrimRight(m.to, "/") + p[len(m.from):])
		}
	}
	return filepath.FromSlash(p)
}

func joinClientPath(dir, name string) string {
	return path.Join(normClientPath(dir), name)
}

// walkFiles lists a download folder (or a single file) on disk.
func walkFiles(root string) []verify.File {
	st, err := os.Stat(root)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		return []verify.File{{Name: filepath.Base(root), Path: root, Size: st.Size()}}
	}
	var out []verify.File
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= 5000 {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, verify.File{Name: filepath.ToSlash(rel), Path: p, Size: info.Size()})
		return nil
	})
	return out
}

// verifySettings is the effective configuration, for the API.
func (v *Verification) Settings() config.Verify { return v.app.Settings.Get().Verify }
