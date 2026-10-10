package app

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/search"
)

// Release is a search result annotated with everything the UI and the
// planner need: what it is, which episodes it fills, and how good it is.
type Release struct {
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Indexer     string    `json:"indexer"`
	IndexerID   int       `json:"indexerId"`
	Indexers    []string  `json:"indexers"`
	Size        int64     `json:"size"`
	Seeders     int       `json:"seeders"`
	Leechers    int       `json:"leechers"`
	AgeHours    float64   `json:"ageHours"`
	PublishDate time.Time `json:"publishDate"`
	Protocol    string    `json:"protocol"`
	GUID        string    `json:"guid"`
	DownloadURL string    `json:"downloadUrl,omitempty"`
	MagnetURL   string    `json:"magnetUrl,omitempty"`
	InfoURL     string    `json:"infoUrl,omitempty"`
	InfoHash    string    `json:"infoHash,omitempty"`

	Info    release.Info `json:"info"`
	Kind    string       `json:"kind"`
	Covers  []int        `json:"covers,omitempty"`    // episode IDs (tv)
	MovieID int          `json:"movieTmdb,omitempty"` // which movie (movie searches)
	Match   bool         `json:"match"`

	Score    float64  `json:"score"`
	Rejected bool     `json:"rejected"`
	Reasons  []string `json:"reasons,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

// Target is what a search is for.
type Target struct {
	Kind   string // tv | movie
	Series *SeriesDetail
	Movies []*MovieDetail
}

// Scope narrows a TV search: which seasons/episodes to query explicitly.
type Scope struct {
	Seasons  []int
	Episodes []int // episode IDs
	Show     bool
}

// buildRequest turns a target + scope into the indexer request. The exact
// scope runs first on every indexer; the rest of the show is prefetched
// behind it so switching scope later is instant.
func buildRequest(t Target, sc Scope, movie *MovieDetail) search.Request {
	if t.Kind == "movie" {
		m := movie
		return search.Request{Kind: "movie", Title: m.Title, Aliases: m.Aliases, Year: m.Year, TmdbID: m.TmdbID, ImdbID: m.ImdbID, Broad: true}
	}
	d := t.Series
	req := search.Request{Kind: "tv", Title: d.Title, Aliases: d.Aliases, Year: d.Year, TvdbID: d.TvdbID, TmdbID: d.TmdbID, ImdbID: d.ImdbID, Anime: d.Anime}
	focus := map[int]bool{}
	for _, s := range sc.Seasons {
		if !focus[s] {
			focus[s] = true
			req.Seasons = append(req.Seasons, s)
		}
	}
	if len(sc.Episodes) > 0 {
		want := map[int]bool{}
		for _, id := range sc.Episodes {
			want[id] = true
		}
		var eps []search.EpQuery
		for _, s := range d.Seasons {
			for _, e := range s.Episodes {
				if !want[e.ID] {
					continue
				}
				eps = append(eps, search.EpQuery{Season: e.Season, Episode: e.Number, Absolute: e.Absolute})
				// Packs containing the episode come from its season query.
				if e.Season > 0 && !focus[e.Season] {
					focus[e.Season] = true
					req.Seasons = append(req.Seasons, e.Season)
				}
			}
		}
		// Per-episode queries only pay off for a handful of episodes; for
		// more, the season queries already return them.
		if len(eps) <= 3 {
			req.Episodes = eps
		}
	}
	if sc.Show || d.Anime {
		req.Broad = true
		req.BroadPriority = search.PShow
		if len(focus) == 0 && len(req.Episodes) == 0 {
			req.BroadPriority = search.PFocus
		}
	}
	if sc.Show {
		// Seasons with missing episodes first: they are the likeliest next click.
		var missingFirst, rest []int
		for _, s := range d.Seasons {
			if s.Number == 0 || focus[s.Number] || !hasAired(s) {
				continue
			}
			if seasonMissing(s) {
				missingFirst = append(missingFirst, s.Number)
			} else {
				rest = append(rest, s.Number)
			}
		}
		req.PrefetchSeasons = append(missingFirst, rest...)
	}
	sort.Ints(req.Seasons)
	return req
}

func seasonMissing(s SeasonDetail) bool {
	for _, e := range s.Episodes {
		if e.Aired && !e.HasFile {
			return true
		}
	}
	return len(s.Episodes) == 0
}

func hasAired(s SeasonDetail) bool {
	if len(s.Episodes) == 0 {
		return true
	}
	for _, e := range s.Episodes {
		if e.Aired {
			return true
		}
	}
	return false
}

// annotator scores raw results for one target.
type annotator struct {
	t       Target
	show    release.Show
	prefs   release.Prefs
	runtime float64 // minutes per episode
	movies  map[int]*MovieDetail
}

func newAnnotator(t Target, prefs release.Prefs) *annotator {
	a := &annotator{t: t, prefs: prefs, movies: map[int]*MovieDetail{}}
	if t.Series != nil {
		a.show = showForMatching(t.Series)
		a.runtime = float64(t.Series.Runtime)
		if a.runtime == 0 {
			a.runtime = 45
			if t.Series.Anime {
				a.runtime = 24
			}
		}
	}
	for _, m := range t.Movies {
		a.movies[m.TmdbID] = m
	}
	return a
}

func (an *annotator) annotate(r arr.SearchResult, movie *MovieDetail) Release {
	rel := Release{
		Title: r.Title, Indexer: r.Indexer, IndexerID: r.IndexerID, Indexers: []string{r.Indexer}, Size: r.Size,
		PublishDate: r.PublishDate, Protocol: r.Protocol, GUID: r.GUID, DownloadURL: r.DownloadURL, MagnetURL: r.MagnetURL,
		InfoURL: r.InfoURL, InfoHash: strings.ToLower(r.InfoHash),
	}
	if r.Seeders != nil {
		rel.Seeders = *r.Seeders
	}
	if r.Leechers != nil {
		rel.Leechers = *r.Leechers
	}
	if !r.PublishDate.IsZero() {
		rel.AgeHours = math.Round(time.Since(r.PublishDate).Hours()*10) / 10
	}
	rel.Key = releaseKey(rel)
	rel.Info = release.Parse(r.Title)
	cand := release.Candidate{Title: r.Title, Size: r.Size, Seeders: rel.Seeders, Protocol: r.Protocol, AgeHours: rel.AgeHours, Info: rel.Info}

	if movie != nil {
		rel.Kind = string(release.KindMovie)
		rel.MovieID = movie.TmdbID
		rel.Match = (r.ImdbID != 0 && "tt"+fmt.Sprintf("%07d", r.ImdbID) == movie.ImdbID) || (r.TmdbID != 0 && r.TmdbID == movie.TmdbID) ||
			release.TitleMatches(rel.Info.Title, append([]string{movie.Title}, movie.Aliases...), movie.Year)
		if rel.Info.Year != 0 && movie.Year != 0 && absInt(rel.Info.Year-movie.Year) > 1 {
			rel.Match = false
		}
		if len(rel.Info.Seasons) > 0 || len(rel.Info.Episodes) > 0 {
			rel.Match = false
		}
		rt := float64(movie.Runtime)
		if rt == 0 {
			rt = 115
		}
		v := release.Score(cand, an.prefs, rt)
		rel.Score, rel.Rejected, rel.Reasons, rel.Notes = v.Score, v.Rejected, v.Reasons, v.Notes
		if !rel.Match {
			rel.Rejected = true
			rel.Reasons = append(rel.Reasons, "different title")
		}
		return rel
	}

	rel.Kind = string(release.Classify(rel.Info))
	rel.Match = (r.TvdbID != 0 && r.TvdbID == an.t.Series.TvdbID) || release.TitleMatches(rel.Info.Title, an.show.Titles, an.show.Year)
	if rel.Match {
		rel.Covers = release.Covers(rel.Info, an.show)
	}
	eps := float64(len(rel.Covers))
	if eps == 0 {
		eps = 1
	}
	v := release.Score(cand, an.prefs, an.runtime*eps)
	rel.Score, rel.Rejected, rel.Reasons, rel.Notes = v.Score, v.Rejected, v.Reasons, v.Notes
	switch {
	case !rel.Match:
		rel.Rejected = true
		rel.Reasons = append(rel.Reasons, "different show")
	case len(rel.Covers) == 0 && !an.t.Series.Synthetic:
		rel.Rejected = true
		rel.Reasons = append(rel.Reasons, "episodes not recognised")
	}
	return rel
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// releaseKey identifies the same release across indexers.
// Title plus size (to the MB) is used rather than the info-hash: private
// trackers re-hash the same files, and the user cares about the content.
func releaseKey(r Release) string {
	return fmt.Sprintf("%s:%d", release.Normalize(r.Title), r.Size/(1<<20))
}

// SearchEvent is streamed to the browser.
type SearchEvent struct {
	Type     string               `json:"type"` // plan | batch | done | error
	Indexers []search.IndexerPlan `json:"indexers,omitempty"`
	Batch    *search.Batch        `json:"batch,omitempty"`
	Releases []Release            `json:"releases,omitempty"`
	Error    string               `json:"error,omitempty"`
	MS       int                  `json:"ms,omitempty"`
	Total    int                  `json:"total,omitempty"`
}

// Session collects and de-duplicates releases for one search.
type Session struct {
	mu   sync.Mutex
	seen map[string]*Release
}

func newSession() *Session { return &Session{seen: map[string]*Release{}} }

// merge folds a release into the session. It returns the release to send
// (new, or the updated existing one) and whether anything changed.
func (s *Session) merge(r Release) (Release, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.seen[r.Key]
	if !ok {
		cp := r
		s.seen[r.Key] = &cp
		return cp, true
	}
	changed := false
	if !containsStr(cur.Indexers, r.Indexer) {
		cur.Indexers = append(cur.Indexers, r.Indexer)
		changed = true
	}
	if r.Seeders > cur.Seeders {
		// Grab from whichever indexer reports the healthiest swarm.
		keep := cur.Indexers
		*cur = r
		cur.Indexers = keep
		changed = true
	}
	return *cur, changed
}

// All returns every release seen so far.
func (s *Session) All() []Release {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Release, 0, len(s.seen))
	for _, r := range s.seen {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rejected != out[j].Rejected {
			return !out[i].Rejected
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Search streams annotated results for a target. emit is never called
// concurrently.
func (a *App) Search(ctx context.Context, t Target, sc Scope, emit func(SearchEvent)) (*Session, error) {
	c := a.C()
	if c.Search == nil {
		return nil, fmt.Errorf("prowlarr %w", ErrNotConfigured)
	}
	an := newAnnotator(t, a.Settings.Get().Prefs)
	sess := newSession()
	start := time.Now()
	var emitMu sync.Mutex
	send := func(ev SearchEvent) {
		emitMu.Lock()
		defer emitMu.Unlock()
		emit(ev)
	}

	run := func(req search.Request, movie *MovieDetail, plan func([]search.IndexerPlan)) error {
		return c.Search.Run(ctx, req, plan, func(b search.Batch) {
			out := make([]Release, 0, len(b.Results))
			for _, r := range b.Results {
				rel, changed := sess.merge(an.annotate(r, movie))
				if changed {
					out = append(out, rel)
				}
			}
			bb := b
			bb.Results = nil
			send(SearchEvent{Type: "batch", Batch: &bb, Releases: out})
		})
	}

	if t.Kind == "movie" {
		// A collection searches each movie at once; merge indexer plans.
		var wg sync.WaitGroup
		var pmu sync.Mutex
		plans := map[int]*search.IndexerPlan{}
		pending := len(t.Movies)
		var firstErr error
		for _, m := range t.Movies {
			wg.Add(1)
			go func(m *MovieDetail) {
				defer wg.Done()
				err := run(buildRequest(t, sc, m), m, func(ps []search.IndexerPlan) {
					pmu.Lock()
					defer pmu.Unlock()
					for _, p := range ps {
						if cur, ok := plans[p.ID]; ok {
							cur.Queries += p.Queries
						} else {
							cp := p
							plans[p.ID] = &cp
						}
					}
					pending--
					if pending == 0 {
						list := make([]search.IndexerPlan, 0, len(plans))
						for _, p := range plans {
							list = append(list, *p)
						}
						sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
						send(SearchEvent{Type: "plan", Indexers: list})
					}
				})
				if err != nil {
					pmu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					pmu.Unlock()
				}
			}(m)
		}
		wg.Wait()
		if firstErr != nil && len(sess.All()) == 0 {
			return sess, firstErr
		}
	} else {
		err := run(buildRequest(t, sc, nil), nil, func(ps []search.IndexerPlan) {
			send(SearchEvent{Type: "plan", Indexers: ps})
		})
		if err != nil {
			return sess, err
		}
	}
	send(SearchEvent{Type: "done", MS: int(time.Since(start).Milliseconds()), Total: len(sess.All())})
	return sess, nil
}
