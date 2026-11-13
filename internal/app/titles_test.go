package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/noxaur/roarr/internal/arr"
)

// countingRadarr has one library movie (id 5, tmdb 603) that belongs to a
// collection, knows tmdb 604 by lookup only, and counts requests by path.
type countingRadarr struct {
	mu    sync.Mutex
	calls map[string]int
}

func (f *countingRadarr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls[r.URL.Path]++
	f.mu.Unlock()
	send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/api/v3/movie/5":
		send(map[string]any{"id": 5, "tmdbId": 603, "title": "The Matrix", "year": 1999, "runtime": 136, "collection": map[string]any{"tmdbId": 2344, "title": "The Matrix Collection"}})
	case "/api/v3/movie/lookup":
		send([]any{map[string]any{"tmdbId": 604, "title": "The Matrix Reloaded", "year": 2003, "runtime": 138, "collection": map[string]any{"tmdbId": 2344, "title": "The Matrix Collection"}}})
	case "/api/v3/queue":
		send(map[string]any{"totalRecords": 1, "records": []any{map[string]any{"id": 1, "movieId": 5}}})
	case "/api/v3/collection":
		send([]any{map[string]any{"tmdbId": 2344, "title": "The Matrix Collection", "movies": []any{map[string]any{"tmdbId": 603, "title": "The Matrix"}}}})
	default:
		http.Error(w, "unexpected "+r.URL.Path, 404)
	}
}

func (f *countingRadarr) got() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for k, v := range f.calls {
		out[k] = v
	}
	return out
}

func TestMovieTargetSkipsQueueAndCollection(t *testing.T) {
	f := &countingRadarr{calls: map[string]int{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a := testApp(t, &Clients{Radarr: arr.NewRadarr(srv.URL, "k")})
	a.Library = newLibrary(a)
	a.Library.items = []LibraryItem{{Kind: "movie", ID: 5, TmdbID: 603}}
	ctx := context.Background()

	// A collection page searches each movie, twice here.
	for range 2 {
		for tmdb, title := range map[int]string{603: "The Matrix", 604: "The Matrix Reloaded"} {
			m, err := a.MovieTarget(ctx, tmdb)
			if err != nil || m.Title != title || m.TmdbID != tmdb || m.Runtime == 0 {
				t.Fatalf("target %d = %+v, %v", tmdb, m, err)
			}
		}
	}
	if want := map[string]int{"/api/v3/movie/5": 1, "/api/v3/movie/lookup": 1}; !reflect.DeepEqual(f.got(), want) {
		t.Fatalf("search targets asked Radarr for %v, want %v", f.got(), want)
	}
}

func TestMovieDetailIsCachedUntilAGrab(t *testing.T) {
	f := &countingRadarr{calls: map[string]int{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a := testApp(t, &Clients{Radarr: arr.NewRadarr(srv.URL, "k")})
	a.Library = newLibrary(a)
	a.Library.items = []LibraryItem{{Kind: "movie", ID: 5, TmdbID: 603}}
	ctx := context.Background()

	for range 2 {
		d, err := a.Movie(ctx, 5)
		if err != nil || !d.Queued || d.Collection == nil {
			t.Fatalf("movie = %+v, %v", d, err)
		}
	}
	once := map[string]int{"/api/v3/movie/5": 1, "/api/v3/queue": 1, "/api/v3/collection": 1}
	if !reflect.DeepEqual(f.got(), once) {
		t.Fatalf("two page loads asked Radarr for %v, want %v", f.got(), once)
	}
	// A grab changes the queue: the next page load must ask again.
	a.forget(GrabRequest{Kind: "movie", MovieID: 5})
	if _, err := a.Movie(ctx, 5); err != nil {
		t.Fatal(err)
	}
	if got := f.got()["/api/v3/queue"]; got != 2 {
		t.Fatalf("after a grab the queue was loaded %d times, want 2", got)
	}
}
