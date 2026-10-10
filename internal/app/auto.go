package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/release"
)

// AutoRequest asks roarr to choose and grab releases on its own.
type AutoRequest struct {
	Kind       string `json:"kind"` // tv | movie
	SeriesID   int    `json:"seriesId"`
	TvdbID     int    `json:"tvdbId"`
	TmdbID     int    `json:"tmdbId"`
	EpisodeIDs []int  `json:"episodeIds"` // empty = every missing aired episode
	Seasons    []int  `json:"seasons"`    // alternative to EpisodeIDs
	MovieTmdb  []int  `json:"movies"`     // one or more movies (collections)
}

// AutoResult reports the plan and what was grabbed.
type AutoResult struct {
	Grabs   []AutoGrab `json:"grabs"`
	Missing []int      `json:"missing,omitempty"`
	Waited  int        `json:"waitedMs"`
	Message string     `json:"message"`
}

// AutoGrab is one grabbed release.
type AutoGrab struct {
	Release string     `json:"release"`
	Fills   int        `json:"fills"`
	Result  GrabResult `json:"result"`
	Error   string     `json:"error,omitempty"`
}

// Auto searches, waits until the best plan is stable, and grabs it.
//
// It does not wait for the slowest indexer when it does not have to: once
// every wanted episode is covered and the plan has not changed for a short
// quiet period, the plan is taken. Slow indexers keep filling the cache.
func (a *App) Auto(ctx context.Context, r AutoRequest) (AutoResult, error) {
	start := time.Now()
	if r.Kind == "movie" {
		return a.autoMovies(ctx, r, start)
	}
	var d *SeriesDetail
	var err error
	if r.SeriesID != 0 {
		d, err = a.Series(ctx, r.SeriesID, true)
	} else {
		d, err = a.SeriesByTvdb(ctx, r.TvdbID, r.TmdbID)
	}
	if err != nil {
		return AutoResult{}, err
	}
	wanted := r.EpisodeIDs
	seasonSet := map[int]bool{}
	for _, s := range r.Seasons {
		seasonSet[s] = true
	}
	if len(wanted) == 0 {
		for _, s := range d.Seasons {
			if s.Number == 0 || (len(seasonSet) > 0 && !seasonSet[s.Number]) {
				continue
			}
			for _, e := range s.Episodes {
				if e.Aired && !e.HasFile && !e.Queued {
					wanted = append(wanted, e.ID)
				}
			}
		}
	}
	if len(wanted) == 0 {
		return AutoResult{Message: "Nothing is missing."}, nil
	}
	sc := scopeFor(d, wanted)
	sess, waited, err := a.searchUntilStable(ctx, Target{Kind: "tv", Series: d}, sc, func(rels []Release) any {
		return release.PlanEpisodes(wanted, options(rels))
	})
	if err != nil {
		return AutoResult{}, err
	}
	rels := sess.All()
	byKey := map[string]Release{}
	for _, rel := range rels {
		byKey[rel.Key] = rel
	}
	res := AutoResult{Waited: waited}
	// Grab the plan; when the *arr declines a pick (size limits, already
	// has it, ...), mark it rejected and re-plan what it would have filled.
	declined := map[string]bool{}
	remaining := wanted
	for round := 0; round < 4 && len(remaining) > 0; round++ {
		opts := options(rels)
		for i := range opts {
			if declined[opts[i].Key] {
				opts[i].Rejected = true
			}
		}
		plan := release.PlanEpisodes(remaining, opts)
		res.Missing = plan.Missing
		var retry []int
		for _, p := range plan.Picks {
			rel := byKey[p.Key]
			g := GrabRequest{Kind: "tv", SeriesID: d.ID, TvdbID: d.TvdbID, EpisodeIDs: p.Fills, Release: rel}
			if d.Synthetic {
				g.SeriesID = 0
			}
			gr, err := a.Grab(ctx, g)
			ag := AutoGrab{Release: rel.Title, Fills: len(p.Fills), Result: gr}
			if err != nil {
				ag.Error = err.Error()
			}
			if gr.SeriesID != 0 && d.Synthetic {
				// The show now exists in Sonarr; switch to real episode ids.
				if nd, err := a.Series(ctx, gr.SeriesID, true); err == nil {
					d = nd
				}
			}
			res.Grabs = append(res.Grabs, ag)
			if !gr.OK {
				declined[p.Key] = true
				// Reasons about the episodes themselves (already have them,
				// already queued) apply to every alternative too.
				if !episodeLevel(gr.Rejections) {
					retry = append(retry, p.Fills...)
				}
			}
		}
		remaining = retry
	}
	res.Message = summarize(res)
	return res, nil
}

func (a *App) autoMovies(ctx context.Context, r AutoRequest, start time.Time) (AutoResult, error) {
	var movies []*MovieDetail
	for _, id := range r.MovieTmdb {
		m, err := a.MovieByTmdb(ctx, id)
		if err != nil {
			return AutoResult{}, err
		}
		if !m.HasFile && !m.Queued {
			movies = append(movies, m)
		}
	}
	if len(movies) == 0 {
		return AutoResult{Message: "Nothing is missing."}, nil
	}
	pick := func(rels []Release) any {
		best := map[int]string{}
		bestScore := map[int]float64{}
		for _, rel := range rels {
			if rel.Rejected || rel.MovieID == 0 {
				continue
			}
			if rel.Score > bestScore[rel.MovieID] {
				best[rel.MovieID], bestScore[rel.MovieID] = rel.Key, rel.Score
			}
		}
		return best
	}
	sess, waited, err := a.searchUntilStable(ctx, Target{Kind: "movie", Movies: movies}, Scope{}, pick)
	if err != nil {
		return AutoResult{}, err
	}
	rels := sess.All()
	best := pick(rels).(map[int]string)
	byKey := map[string]Release{}
	for _, rel := range rels {
		byKey[rel.Key] = rel
	}
	res := AutoResult{Waited: waited}
	for _, m := range movies {
		key, ok := best[m.TmdbID]
		if !ok {
			res.Missing = append(res.Missing, m.TmdbID)
			continue
		}
		rel := byKey[key]
		gr, err := a.Grab(ctx, GrabRequest{Kind: "movie", MovieID: m.ID, TmdbID: m.TmdbID, Release: rel})
		ag := AutoGrab{Release: rel.Title, Fills: 1, Result: gr}
		if err != nil {
			ag.Error = err.Error()
		}
		res.Grabs = append(res.Grabs, ag)
	}
	res.Message = summarize(res)
	return res, nil
}

// episodeLevel reports whether *arr rejections are about the episodes rather
// than the release, so trying another release would be declined as well.
func episodeLevel(rej []string) bool {
	for _, r := range rej {
		l := strings.ToLower(r)
		for _, k := range []string{"cutoff", "not an upgrade", "already in queue", "already imported", "not monitored", "not wanted"} {
			if strings.Contains(l, k) {
				return true
			}
		}
	}
	return false
}

func summarize(r AutoResult) string {
	ok := 0
	reason := ""
	for _, g := range r.Grabs {
		if g.Result.OK {
			ok++
		} else if reason == "" {
			reason = g.Error
			if len(g.Result.Rejections) > 0 {
				reason = g.Result.Rejections[0]
			} else if reason == "" {
				reason = g.Result.Message
			}
		}
	}
	if ok == 0 && reason != "" {
		return "Nothing grabbed: " + reason
	}
	msg := fmt.Sprintf("Grabbed %d of %d planned release(s)", ok, len(r.Grabs))
	if len(r.Missing) > 0 {
		msg += fmt.Sprintf("; nothing suitable for %d item(s)", len(r.Missing))
	}
	return msg + "."
}

func options(rels []Release) []release.Option {
	out := make([]release.Option, 0, len(rels))
	for _, r := range rels {
		out = append(out, release.Option{Key: r.Key, Score: r.Score, Rejected: r.Rejected, Covers: r.Covers})
	}
	return out
}

// scopeFor queries seasons for wanted episodes (and episodes when few).
func scopeFor(d *SeriesDetail, wanted []int) Scope {
	want := map[int]bool{}
	for _, id := range wanted {
		want[id] = true
	}
	sc := Scope{}
	seasons := map[int]bool{}
	total := 0
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			if s.Number > 0 {
				total++
			}
			if want[e.ID] {
				seasons[s.Number] = true
			}
		}
	}
	for s := range seasons {
		sc.Seasons = append(sc.Seasons, s)
	}
	if len(wanted) <= 3 {
		sc.Episodes = wanted
	}
	if len(seasons) > 1 || len(wanted) > total/2 {
		sc.Show = true
	}
	return sc
}

// searchUntilStable runs a search and returns early once decide() has
// returned the same answer for a quiet period after every fast indexer
// answered. decide must return a comparable summary of the decision.
func (a *App) searchUntilStable(ctx context.Context, t Target, sc Scope, decide func([]Release) any) (*Session, int, error) {
	const (
		minWait = 2500 * time.Millisecond
		quiet   = 2 * time.Second
	)
	start := time.Now()
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu        sync.Mutex
		last      any
		changedAt = time.Now()
		live      = newSession()
	)
	done := make(chan error, 1)
	go func() {
		_, err := a.Search(sctx, t, sc, func(ev SearchEvent) {
			if ev.Type != "batch" {
				return
			}
			for _, r := range ev.Releases {
				live.merge(r)
			}
			d := decide(live.All())
			mu.Lock()
			if !reflect.DeepEqual(d, last) {
				last, changedAt = d, time.Now()
			}
			mu.Unlock()
		})
		done <- err
	}()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				return nil, 0, err
			}
			return live, int(time.Since(start).Milliseconds()), nil
		case <-tick.C:
			mu.Lock()
			stable := last != nil && time.Since(start) > minWait && time.Since(changedAt) > quiet && complete(last)
			mu.Unlock()
			if stable {
				cancel()
				<-done
				slog.Info("auto: plan stable, not waiting for slow indexers", "waited", time.Since(start).Round(time.Millisecond))
				return live, int(time.Since(start).Milliseconds()), nil
			}
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
}

// complete reports whether a decision covers everything it was asked for.
func complete(d any) bool {
	switch v := d.(type) {
	case release.Plan:
		return len(v.Picks) > 0 && len(v.Missing) == 0
	case map[int]string:
		return len(v) > 0
	}
	return false
}
