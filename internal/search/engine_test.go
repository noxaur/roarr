package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
)

// fakeProwlarr serves two indexers and records the order of search terms
// per indexer. Each search takes `delay`.
type fakeProwlarr struct {
	mu    sync.Mutex
	order map[string][]string
	calls int
	delay time.Duration
	fail  bool
}

func (f *fakeProwlarr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/indexer":
		caps := `{"tvSearchParams":["q","season","ep","tvdbId"],"movieSearchParams":["q","imdbId"],"categories":[{"id":5000},{"id":2000}]}`
		w.Write([]byte(`[{"id":1,"name":"one","enable":true,"capabilities":` + caps + `},{"id":2,"name":"two","enable":true,"capabilities":` + caps + `}]`))
	case "/api/v1/indexerstatus":
		w.Write([]byte(`[]`))
	case "/api/v1/search":
		ix := r.URL.Query().Get("indexerIds")
		term := r.URL.Query().Get("query")
		f.mu.Lock()
		f.order[ix] = append(f.order[ix], term)
		f.calls++
		fail := f.fail
		f.mu.Unlock()
		time.Sleep(f.delay)
		if fail {
			http.Error(w, "boom", 500)
			return
		}
		json.NewEncoder(w).Encode([]arr.SearchResult{{Title: "Show.S01E01.1080p.WEB.H264-GRP", IndexerID: 1, Indexer: "x"}})
	default:
		http.NotFound(w, r)
	}
}

func newTestEngine(t *testing.T, f *fakeProwlarr) *Engine {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	e := NewEngine(arr.NewProwlarr(srv.URL, "k"))
	t.Cleanup(e.Close)
	return e
}

func TestFocusJumpsAheadOfPrefetch(t *testing.T) {
	f := &fakeProwlarr{order: map[string][]string{}, delay: 40 * time.Millisecond}
	e := newTestEngine(t, f)
	ctx := context.Background()

	// Show page opens: broad query + six prefetch seasons.
	go e.Run(ctx, Request{Kind: "tv", Title: "Show", TvdbID: 1, Broad: true, PrefetchSeasons: []int{1, 2, 3, 4, 5, 6}}, nil, func(Batch) {})
	time.Sleep(60 * time.Millisecond)
	// User clicks season 6 while seasons 1..5 are still queued.
	done := make(chan struct{})
	go func() {
		e.Run(ctx, Request{Kind: "tv", Title: "Show", TvdbID: 1, Seasons: []int{6}}, nil, func(Batch) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("focus query never finished")
	}
	f.mu.Lock()
	order := f.order["1"]
	f.mu.Unlock()
	// Season 6 must have run before the remaining prefetch seasons, and
	// only once even though both requests wanted it.
	pos := -1
	count := 0
	for i, term := range order {
		if strings.Contains(term, "{Season:06}") {
			pos = i
			count++
		}
	}
	if count != 1 {
		t.Fatalf("season 6 queried %d times: %v", count, order)
	}
	if pos > 2 {
		t.Fatalf("season 6 ran at position %d, want near the front: %v", pos, order)
	}
}

func TestIdenticalQueriesShareOneCall(t *testing.T) {
	f := &fakeProwlarr{order: map[string][]string{}, delay: 30 * time.Millisecond}
	e := newTestEngine(t, f)
	req := Request{Kind: "tv", Title: "Show", TvdbID: 1, Seasons: []int{1}}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.Run(context.Background(), req, nil, func(Batch) {})
		}()
	}
	wg.Wait()
	// A later identical search is served from cache.
	var cached bool
	e.Run(context.Background(), req, nil, func(b Batch) { cached = b.Cached })
	if f.calls != 2 { // one per indexer
		t.Fatalf("want 2 upstream calls, got %d", f.calls)
	}
	if !cached {
		t.Fatal("repeat search was not cached")
	}
}

func TestFailuresAreRememberedBriefly(t *testing.T) {
	f := &fakeProwlarr{order: map[string][]string{}, fail: true}
	e := newTestEngine(t, f)
	req := Request{Kind: "movie", Title: "Film", ImdbID: "tt1"}
	var errs int
	e.Run(context.Background(), req, nil, func(b Batch) {
		if b.Error != "" {
			errs++
		}
	})
	e.Run(context.Background(), req, nil, func(Batch) {})
	if errs != 2 || f.calls != 2 {
		t.Fatalf("want 2 errors from 2 calls, got errs=%d calls=%d", errs, f.calls)
	}
}
