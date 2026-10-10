package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/seerr"
)

// RequestItem is a Seerr request enriched with title metadata and library
// links so the inbox can go straight to "fetch".
type RequestItem struct {
	ID          int       `json:"id"`
	Status      string    `json:"status"` // pending | approved | declined | failed | completed
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Year        int       `json:"year,omitempty"`
	Poster      string    `json:"poster,omitempty"`
	TmdbID      int       `json:"tmdbId"`
	TvdbID      int       `json:"tvdbId,omitempty"`
	Seasons     []int     `json:"seasons,omitempty"`
	RequestedBy string    `json:"requestedBy"`
	CreatedAt   time.Time `json:"createdAt"`
	Available   string    `json:"available"` // unknown | pending | processing | partial | available
	InLibrary   bool      `json:"inLibrary"`
	LibraryID   int       `json:"libraryId,omitempty"`
}

var requestStatus = map[int]string{1: "pending", 2: "approved", 3: "declined", 4: "failed", 5: "completed"}
var mediaStatus = map[int]string{1: "unknown", 2: "pending", 3: "processing", 4: "partial", 5: "available"}

type titleMeta struct {
	title  string
	year   int
	poster string
	at     time.Time
}

var (
	metaMu    sync.Mutex
	metaCache = map[string]titleMeta{}
)

// Requests lists Seerr requests (newest first).
func (a *App) Requests(ctx context.Context, filter string) ([]RequestItem, error) {
	c := a.C()
	if c.Seerr == nil {
		return nil, fmt.Errorf("seerr %w", ErrNotConfigured)
	}
	if filter == "" {
		filter = "all"
	}
	reqs, err := c.Seerr.Requests(ctx, filter, 60)
	if err != nil {
		return nil, err
	}
	out := make([]RequestItem, len(reqs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, r := range reqs {
		it := RequestItem{ID: r.ID, Status: requestStatus[r.Status], Kind: r.Type, TmdbID: r.Media.TmdbID, TvdbID: r.Media.TvdbID, RequestedBy: r.RequestedBy.DisplayName, CreatedAt: r.CreatedAt, Available: mediaStatus[r.Media.Status]}
		for _, s := range r.Seasons {
			it.Seasons = append(it.Seasons, s.SeasonNumber)
		}
		out[i] = it
		wg.Add(1)
		go func(i int, r seerr.Request) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			m := a.meta(ctx, c.Seerr, r.Type, r.Media.TmdbID)
			out[i].Title, out[i].Year, out[i].Poster = m.title, m.year, m.poster
		}(i, r)
	}
	wg.Wait()
	for i := range out {
		d := DiscoverItem{Kind: out[i].Kind, TmdbID: out[i].TmdbID, TvdbID: out[i].TvdbID}
		a.markLibrary(&d)
		out[i].InLibrary, out[i].LibraryID = d.InLibrary, d.LibraryID
		if out[i].TvdbID == 0 {
			out[i].TvdbID = d.TvdbID
		}
	}
	return out, nil
}

func (a *App) meta(ctx context.Context, s *seerr.Client, kind string, tmdbID int) titleMeta {
	key := fmt.Sprintf("%s:%d", kind, tmdbID)
	metaMu.Lock()
	m, ok := metaCache[key]
	metaMu.Unlock()
	if ok && time.Since(m.at) < 24*time.Hour {
		return m
	}
	var res struct {
		Title        string `json:"title"`
		Name         string `json:"name"`
		ReleaseDate  string `json:"releaseDate"`
		FirstAirDate string `json:"firstAirDate"`
		PosterPath   string `json:"posterPath"`
	}
	path := fmt.Sprintf("/api/v1/%s/%d", kind, tmdbID)
	if err := s.Get(ctx, path, nil, &res); err != nil {
		slog.Debug("seerr meta failed", "path", path, "err", err)
		return titleMeta{title: fmt.Sprintf("TMDB %d", tmdbID)}
	}
	r := seerr.Result{Title: res.Title, Name: res.Name, ReleaseDate: res.ReleaseDate, FirstAirDate: res.FirstAirDate}
	m = titleMeta{title: r.DisplayTitle(), year: r.Year(), poster: seerr.Poster(res.PosterPath, "w342"), at: time.Now()}
	metaMu.Lock()
	metaCache[key] = m
	metaMu.Unlock()
	return m
}

// RequestAction approves or declines a Seerr request.
func (a *App) RequestAction(ctx context.Context, id int, action string) error {
	c := a.C()
	if c.Seerr == nil {
		return fmt.Errorf("seerr %w", ErrNotConfigured)
	}
	switch action {
	case "approve":
		return c.Seerr.Approve(ctx, id)
	case "decline":
		return c.Seerr.Decline(ctx, id)
	}
	return fmt.Errorf("unknown action %q", action)
}

// seerrLoop grabs newly approved Seerr requests with roarr's fast search
// when SEERR_AUTO_FETCH is on. Each request is handled once (remembered in
// the data dir), and only if its media is not available yet.
func (a *App) seerrLoop(ctx context.Context) {
	path := filepath.Join(a.DataDir, "seerr-handled.json")
	handled := map[int]time.Time{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &handled)
	}
	t := time.NewTicker(90 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !a.Settings.Get().Behaviour.SeerrAutoFetch {
			continue
		}
		c := a.C()
		if c.Seerr == nil {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		reqs, err := c.Seerr.Requests(rctx, "approved", 30)
		cancel()
		if err != nil {
			slog.Warn("seerr auto-fetch: listing requests failed", "err", err)
			continue
		}
		changed := false
		for _, r := range reqs {
			if _, done := handled[r.ID]; done || r.Media.Status >= 4 || time.Since(r.CreatedAt) > 7*24*time.Hour {
				continue
			}
			handled[r.ID] = time.Now()
			changed = true
			ar := AutoRequest{Kind: r.Type, TvdbID: r.Media.TvdbID, TmdbID: r.Media.TmdbID}
			if r.Type == "movie" {
				ar.MovieTmdb = []int{r.Media.TmdbID}
			} else {
				for _, s := range r.Seasons {
					ar.Seasons = append(ar.Seasons, s.SeasonNumber)
				}
			}
			actx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			res, err := a.Auto(actx, ar)
			cancel()
			if err != nil {
				slog.Warn("seerr auto-fetch failed", "request", r.ID, "err", err)
				continue
			}
			slog.Info("seerr auto-fetch", "request", r.ID, "result", res.Message)
		}
		if changed {
			if b, err := json.Marshal(handled); err == nil {
				_ = os.WriteFile(path, b, 0o600)
			}
		}
	}
}
