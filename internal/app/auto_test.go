package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
)

func TestAutoMoviesCompleteNeedsEveryMovie(t *testing.T) {
	hit := func(tmdb int, key string) Release { return Release{Key: key, MovieID: tmdb, Score: 50} }
	// The first indexer to answer has only one movie of a three-movie collection.
	rels := []Release{hit(603, "matrix")}
	if complete(pickMovies(3, rels)) {
		t.Fatal("one of three movies has a pick: the search must go on")
	}
	rels = append(rels, hit(604, "reloaded"), Release{Key: "fake", MovieID: 605, Score: 90, Rejected: true})
	if complete(pickMovies(3, rels)) {
		t.Fatal("a rejected release is not a pick")
	}
	rels = append(rels, hit(605, "revolutions"))
	p := pickMovies(3, rels)
	if !complete(p) || p.best[605] != "revolutions" {
		t.Fatalf("every movie has a pick: %+v", p)
	}
	if !complete(pickMovies(1, []Release{hit(603, "matrix")})) {
		t.Fatal("a single movie is complete on its first acceptable release")
	}
}

// fakeQueueArr is a Sonarr and Radarr with one aired episode and one movie,
// both missing and both already in the queue.
type fakeQueueArr struct {
	queueDown atomic.Bool
}

func (f *fakeQueueArr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/api/v3/series/7":
		send(map[string]any{"id": 7, "title": "Show", "seasons": []any{map[string]any{"seasonNumber": 1}}})
	case "/api/v3/episode":
		send([]any{map[string]any{"id": 70, "seriesId": 7, "seasonNumber": 1, "episodeNumber": 1, "airDateUtc": time.Now().Add(-time.Hour)}})
	case "/api/v3/episodefile":
		send([]any{})
	case "/api/v3/movie/5":
		send(map[string]any{"id": 5, "tmdbId": 603, "title": "Movie"})
	case "/api/v3/queue":
		if f.queueDown.Load() {
			http.Error(w, "database is locked", 500)
			return
		}
		send(map[string]any{"totalRecords": 2, "records": []any{
			map[string]any{"id": 1, "seriesId": 7, "episodeId": 70},
			map[string]any{"id": 2, "movieId": 5},
		}})
	default:
		http.Error(w, "unexpected "+r.URL.Path, 404)
	}
}

// With no Prowlarr configured, an Auto that goes on to search fails with
// ErrNotConfigured; one that stops at the queue fails with errQueueUnknown.
func TestAutoRefusesWhenQueueUnknown(t *testing.T) {
	f := &fakeQueueArr{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a := testApp(t, &Clients{Sonarr: arr.NewSonarr(srv.URL, "k"), Radarr: arr.NewRadarr(srv.URL, "k")})
	a.Library = newLibrary(a)
	a.Library.items = []LibraryItem{{Kind: "movie", ID: 5, TmdbID: 603}}
	reqs := map[string]AutoRequest{
		"tv":    {Kind: "tv", SeriesID: 7},
		"movie": {Kind: "movie", MovieTmdb: []int{603}},
	}
	for name, req := range reqs {
		f.queueDown.Store(true)
		if _, err := a.Auto(context.Background(), req); !errors.Is(err, errQueueUnknown) {
			t.Fatalf("%s, queue down: Auto went on to search for what is already queued: %v", name, err)
		}
		// The answer without a queue is not cached: Auto sees it come back.
		f.queueDown.Store(false)
		res, err := a.Auto(context.Background(), req)
		if err != nil || res.Message != "Nothing is missing." {
			t.Fatalf("%s, queue loaded: %+v, %v", name, res, err)
		}
	}
	// Named episodes are no exception: episode 70 may be downloading.
	f.queueDown.Store(true)
	if _, err := a.Auto(context.Background(), AutoRequest{Kind: "tv", SeriesID: 7, EpisodeIDs: []int{70}}); !errors.Is(err, errQueueUnknown) {
		t.Fatalf("named episodes, queue down: %v", err)
	}
	// The pages still load.
	if d, err := a.Series(context.Background(), 7, true); err != nil || len(d.Seasons) != 1 {
		t.Fatalf("series page with the queue down: %+v, %v", d, err)
	}
	a.forget(GrabRequest{MovieID: 5})
	if d, err := a.Movie(context.Background(), 5); err != nil || d.Queued {
		t.Fatalf("movie page with the queue down: %+v, %v", d, err)
	}
}
