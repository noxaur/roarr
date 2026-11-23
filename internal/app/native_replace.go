package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/importer"
)

// errSearchBusy: another search holds the title; the tick offers the job
// again.
var errSearchBusy = errors.New("another search for this title is running; roarr tries again shortly")

// replaceLoop replaces jobs that failed because of their release, one at
// a time, so a burst of dead torrents does not start a burst of searches.
func (n *Native) replaceLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case hash := <-n.bad:
			rctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			switch _, err := n.Replace(rctx, hash, "", false); {
			case errors.Is(err, errSearchBusy):
				slog.Debug("native: replacement waits for a running search", "hash", hash)
			case err != nil:
				slog.Warn("native: could not replace a failed download", "hash", hash, "err", err)
			}
			cancel()
		}
	}
}

// Replace throws a download away and grabs the next best release for what
// it was meant to fill. The release goes on the blocklist, the torrent and
// its data leave qBittorrent, and the planner picks again for the episodes
// (or the movie) that are still missing. Library files the download
// already placed stay unless discard is set: a fake never gets that far,
// but a person who throws a download away wants its files gone too.
// reason is the blocklist reason; empty uses the job's message.
func (n *Native) Replace(ctx context.Context, hash, reason string, discard bool) (AutoResult, error) {
	hash = strings.ToLower(hash)
	n.mu.Lock()
	j, ok := n.jobs[hash]
	var job NativeJob
	if ok {
		job = *j
		job.Files = append([]NativeFile(nil), j.Files...)
		n.replacing[hash] = true
	}
	n.mu.Unlock()
	if !ok {
		return AutoResult{}, fmt.Errorf("download %s not found", hash)
	}
	defer func() {
		n.mu.Lock()
		delete(n.replacing, hash)
		n.mu.Unlock()
	}()
	if reason == "" {
		reason = job.Message
	}
	// Claim the title before anything changes: a search that runs now has
	// skipped these episodes as queued, so the job must stay replaceable
	// (Replacement empty) until a search of its own can run.
	if !n.claimSearch(job.TitleKey) {
		return AutoResult{}, errSearchBusy
	}
	defer n.releaseSearch(job.TitleKey)
	n.blocklist.Add(Blocked{Release: job.Release, Hash: hash, Indexer: job.Indexer, Kind: job.Kind, TitleKey: job.TitleKey, Title: job.TitleName, Reason: reason})
	if c := n.app.C(); c.QBit != nil {
		if err := c.QBit.Delete(ctx, hash, true); err != nil {
			slog.Warn("native: could not remove a failed torrent", "release", job.Release, "err", err)
		}
	}
	title, _ := n.store.Title(job.TitleKey)
	if discard {
		var paths []string
		for _, f := range n.store.Files(job.TitleKey) {
			if strings.EqualFold(f.Hash, hash) {
				paths = append(paths, f.Path)
			}
		}
		if len(paths) > 0 {
			if _, err := n.deleteFiles(ctx, title, paths, false); err != nil {
				return AutoResult{}, fmt.Errorf("the download is removed, but its library files could not be deleted: %w", err)
			}
		}
	}
	req, err := n.app.replaceRequest(ctx, job, title)
	var res AutoResult
	switch {
	case err != nil:
	case req.Kind == "":
		res.Message = "Nothing is missing."
	default:
		res, err = n.app.Auto(ctx, req)
	}
	var got []string
	for _, g := range res.Grabs {
		if g.Result.OK {
			got = append(got, g.Release)
		}
	}
	state, outcome := "failed", ""
	switch {
	case err != nil:
		outcome = "Blocklisted. No replacement: " + err.Error()
	case len(got) > 0:
		state, outcome = "replaced", "Blocklisted and replaced by "+strings.Join(got, ", ")
	case req.Kind == "":
		state, outcome = "replaced", "Blocklisted. Nothing is missing any more."
	default:
		outcome = "Blocklisted. " + res.Message
	}
	n.set(j, func() { j.State, j.Replacement = state, outcome })
	n.save()
	n.forgetShows()
	slog.Info("native: replaced a failed download", "release", job.Release, "reason", reason, "result", outcome)
	return res, err
}

// replaceRequest is the auto-grab request for what a failed job was meant
// to fill and the library still misses. An empty Kind means nothing.
func (a *App) replaceRequest(ctx context.Context, j NativeJob, t importer.Title) (AutoRequest, error) {
	if j.Kind == "movie" {
		if t.TmdbID == 0 {
			return AutoRequest{}, errors.New("the movie has no TMDB id")
		}
		return AutoRequest{Kind: "movie", MovieTmdb: []int{t.TmdbID}}, nil
	}
	want := map[[2]int]bool{}
	for _, w := range j.Wanted {
		want[w] = true
	}
	if len(want) == 0 {
		for _, f := range j.Files {
			for _, e := range f.Placement.Episodes {
				want[[2]int{f.Placement.Season, e}] = true
			}
		}
	}
	id := j.ArrID
	if a.nativeLib("tv") {
		id = NativeID(t)
	}
	if id == 0 || len(want) == 0 {
		return AutoRequest{}, errors.New("roarr does not know which episodes the download was for")
	}
	d, err := a.Series(ctx, id, true)
	if err != nil {
		return AutoRequest{}, err
	}
	var ids []int
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			if want[[2]int{e.Season, e.Number}] && !e.HasFile && !e.Queued {
				ids = append(ids, e.ID)
			}
		}
	}
	if len(ids) == 0 {
		return AutoRequest{}, nil
	}
	return AutoRequest{Kind: "tv", SeriesID: id, TvdbID: d.TvdbID, TmdbID: d.TmdbID, EpisodeIDs: ids}, nil
}
