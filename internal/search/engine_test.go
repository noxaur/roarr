package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
)

// fakeProwlarr serves two indexers and records the order of search terms
// per indexer. Each search takes `delay`; on an indexer with a gate it also
// waits for one token.
type fakeProwlarr struct {
	mu    sync.Mutex
	order map[string][]string
	calls int
	delay time.Duration
	fail  bool
	gate  map[string]chan struct{}

	ixCalls, statusCalls int
	ixGate               chan struct{} // the indexer list waits for it when set
	status               string        // body of /indexerstatus, "[]" when empty
	statusFail           bool
}

func (f *fakeProwlarr) listCalls() (indexers, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ixCalls, f.statusCalls
}

func (f *fakeProwlarr) ran(ix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.order[ix])
}

func (f *fakeProwlarr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/indexer":
		f.mu.Lock()
		f.ixCalls++
		gate := f.ixGate
		f.mu.Unlock()
		if gate != nil {
			<-gate
		}
		caps := `{"tvSearchParams":["q","season","ep","tvdbId"],"movieSearchParams":["q","imdbId"],"categories":[{"id":5000},{"id":2000}]}`
		w.Write([]byte(`[{"id":1,"name":"one","enable":true,"capabilities":` + caps + `},{"id":2,"name":"two","enable":true,"capabilities":` + caps + `}]`))
	case "/api/v1/indexerstatus":
		f.mu.Lock()
		f.statusCalls++
		body, fail := f.status, f.statusFail
		f.mu.Unlock()
		if fail {
			http.Error(w, "boom", 500)
			return
		}
		if body == "" {
			body = `[]`
		}
		w.Write([]byte(body))
	case "/api/v1/search":
		ix := r.URL.Query().Get("indexerIds")
		term := r.URL.Query().Get("query")
		f.mu.Lock()
		f.order[ix] = append(f.order[ix], term)
		f.calls++
		fail := f.fail
		f.mu.Unlock()
		time.Sleep(f.delay)
		if g := f.gate[ix]; g != nil {
			<-g
		}
		if fail {
			http.Error(w, "boom", 500)
			return
		}
		json.NewEncoder(w).Encode([]arr.SearchResult{{Title: "Show.S01E01.1080p.WEB.H264-GRP", IndexerID: 1, Indexer: "x"}})
	default:
		http.NotFound(w, r)
	}
}

func newTestEngine(t testing.TB, f *fakeProwlarr) *Engine {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	e := NewEngine(arr.NewProwlarr(srv.URL, "k"))
	t.Cleanup(e.Close)
	for _, g := range f.gate {
		t.Cleanup(func() { close(g) })
	}
	return e
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// queuedWaiters reports how many requests wait for a query queued on
// indexer 1, or -1 when it is not queued.
func queuedWaiters(e *Engine, term string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if q := e.queues[1]; q != nil {
		for _, en := range q.pending {
			if en.j.q.Term == term {
				return en.waiters
			}
		}
	}
	return -1
}

const (
	s01 = "{TvdbId:1}{Season:01}"
	s02 = "{TvdbId:1}{Season:02}"
	s03 = "{TvdbId:1}{Season:03}"
)

func seasonReq(n int) Request {
	return Request{Kind: "tv", Title: "Show", TvdbID: 1, Seasons: []int{n}}
}

// busyEngine returns an engine whose indexer 1 is held on the season 3 query
// until the gate gets a token.
func busyEngine(t *testing.T) (*Engine, *fakeProwlarr) {
	f := &fakeProwlarr{order: map[string][]string{}, gate: map[string]chan struct{}{"1": make(chan struct{}, 8)}}
	e := newTestEngine(t, f)
	go e.Run(context.Background(), seasonReq(3), nil, func(Batch) {})
	waitFor(t, "indexer 1 to be busy", func() bool { return len(f.ran("1")) == 1 })
	return e, f
}

func TestAbandonedFocusQueryIsDropped(t *testing.T) {
	e, f := busyEngine(t)
	bg := context.Background()

	ctx, cancel := context.WithCancel(bg)
	go e.Run(ctx, seasonReq(1), nil, func(Batch) {})
	waitFor(t, "season 1 to queue", func() bool { return queuedWaiters(e, s01) == 1 })
	cancel()
	waitFor(t, "season 1 to lose its waiter", func() bool { return queuedWaiters(e, s01) == 0 })

	for i := 0; i < 3; i++ {
		f.gate["1"] <- struct{}{}
	}
	e.Run(bg, seasonReq(2), nil, func(Batch) {})
	if got, want := f.ran("1"), []string{s03, s02}; !slices.Equal(got, want) {
		t.Fatalf("indexer 1 ran %v, want %v", got, want)
	}

	// Asking for the dropped query again runs it fresh.
	var again Batch
	e.Run(bg, seasonReq(1), nil, func(b Batch) {
		if b.IndexerID == 1 {
			again = b
		}
	})
	if got, want := f.ran("1"), []string{s03, s02, s01}; !slices.Equal(got, want) {
		t.Fatalf("indexer 1 ran %v, want %v", got, want)
	}
	if again.Error != "" || again.Cached || len(again.Results) != 1 {
		t.Fatalf("re-requested season 1 = %+v, want a fresh answer", again)
	}
}

func TestQueryWithRemainingWaiterRuns(t *testing.T) {
	e, f := busyEngine(t)
	bg := context.Background()

	ctx, cancel := context.WithCancel(bg)
	go e.Run(ctx, seasonReq(1), nil, func(Batch) {})
	waitFor(t, "season 1 to queue", func() bool { return queuedWaiters(e, s01) == 1 })
	done := make(chan struct{})
	go func() {
		e.Run(bg, seasonReq(1), nil, func(Batch) {})
		close(done)
	}()
	waitFor(t, "second waiter", func() bool { return queuedWaiters(e, s01) == 2 })
	cancel()
	waitFor(t, "first waiter to leave", func() bool { return queuedWaiters(e, s01) == 1 })

	f.gate["1"] <- struct{}{}
	f.gate["1"] <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("season 1 never ran for its remaining waiter")
	}
	if got, want := f.ran("1"), []string{s03, s01}; !slices.Equal(got, want) {
		t.Fatalf("indexer 1 ran %v, want %v", got, want)
	}
}

func TestQueueRunsFocusThenShowThenPrefetch(t *testing.T) {
	e, f := busyEngine(t)
	bg := context.Background()

	go e.Run(bg, Request{Kind: "tv", Title: "Show", TvdbID: 1, PrefetchSeasons: []int{1, 2}}, nil, func(Batch) {})
	waitFor(t, "prefetch to queue", func() bool { return queuedWaiters(e, s02) == 1 })
	go e.Run(bg, Request{Kind: "tv", Title: "Show", TvdbID: 1, Broad: true, BroadPriority: PShow}, nil, func(Batch) {})
	waitFor(t, "show query to queue", func() bool { return queuedWaiters(e, "{TvdbId:1}") == 1 })
	go e.Run(bg, seasonReq(5), nil, func(Batch) {})
	waitFor(t, "focus query to queue", func() bool { return queuedWaiters(e, "{TvdbId:1}{Season:05}") == 1 })

	for i := 0; i < 5; i++ {
		f.gate["1"] <- struct{}{}
	}
	want := []string{s03, "{TvdbId:1}{Season:05}", "{TvdbId:1}", s01, s02}
	waitFor(t, "the queue to drain", func() bool { return len(f.ran("1")) == len(want) })
	got := f.ran("1")
	slices.Sort(got[3:]) // the two prefetch seasons queue in either order
	if !slices.Equal(got, want) {
		t.Fatalf("indexer 1 ran %v, want %v", got, want)
	}
}

func TestStuckIndexerDoesNotBlockOthers(t *testing.T) {
	f := &fakeProwlarr{order: map[string][]string{}, gate: map[string]chan struct{}{"2": make(chan struct{})}}
	e := newTestEngine(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	got := make(chan Batch, 2)
	go e.Run(ctx, seasonReq(1), nil, func(b Batch) { got <- b })
	select {
	case b := <-got:
		if b.IndexerID != 1 || len(b.Results) != 1 {
			t.Fatalf("first batch = %+v, want indexer 1's results", b)
		}
		if d := time.Since(start); d > 500*time.Millisecond {
			t.Fatalf("first results took %v with indexer 2 stuck", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("indexer 1 waited for stuck indexer 2")
	}
}

func TestColdIndexerListIsFetchedOnce(t *testing.T) {
	gate := make(chan struct{})
	f := &fakeProwlarr{order: map[string][]string{}, ixGate: gate}
	e := newTestEngine(t, f)
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)

	var wg sync.WaitGroup
	got := make([]int, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ix, _, _ := e.Indexers(context.Background())
			got[i] = len(ix)
		}()
	}
	// The status lookup does not wait for the indexer list.
	waitFor(t, "both lookups to start", func() bool {
		ix, st := f.listCalls()
		return ix == 1 && st == 1
	})
	time.Sleep(20 * time.Millisecond) // let every caller arrive
	release()
	wg.Wait()
	if ix, st := f.listCalls(); ix != 1 || st != 1 {
		t.Fatalf("%d indexer and %d status calls for 8 cold callers, want 1 each", ix, st)
	}
	for _, n := range got {
		if n != 2 {
			t.Fatalf("callers got %v indexers, want 2 each", got)
		}
	}
}

// expireIndexers makes the cached indexer list older than its TTL.
func expireIndexers(e *Engine) {
	e.mu.Lock()
	e.ixAt = time.Now().Add(-2 * ixTTL)
	e.mu.Unlock()
}

func indexersFresh(e *Engine) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return time.Since(e.ixAt) < ixTTL && e.ixRefresh == nil
}

func TestExpiredIndexerListIsServedWhileRefreshing(t *testing.T) {
	f := &fakeProwlarr{order: map[string][]string{}}
	e := newTestEngine(t, f)
	bg := context.Background()
	if _, _, err := e.Indexers(bg); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	f.mu.Lock()
	f.ixGate = gate
	f.mu.Unlock()
	expireIndexers(e)

	for i := 0; i < 2; i++ {
		done := make(chan int)
		go func() {
			ix, _, _ := e.Indexers(bg)
			done <- len(ix)
		}()
		select {
		case n := <-done:
			if n != 2 {
				t.Fatalf("stale list has %d indexers, want 2", n)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("an expired list waited for the slow refresh")
		}
	}
	waitFor(t, "the refresh to start", func() bool { ix, _ := f.listCalls(); return ix == 2 })
	release()
	waitFor(t, "the refresh to land", func() bool { return indexersFresh(e) })
	if ix, st := f.listCalls(); ix != 2 || st != 2 {
		t.Fatalf("%d indexer and %d status calls, want one refresh (2 each)", ix, st)
	}
}

func TestFailedStatusLookupKeepsDisabledSet(t *testing.T) {
	till := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	f := &fakeProwlarr{order: map[string][]string{}, status: `[{"indexerId":2,"disabledTill":"` + till + `"}]`}
	e := newTestEngine(t, f)
	bg := context.Background()
	if _, dis, _ := e.Indexers(bg); len(dis) != 1 || dis[2].IsZero() {
		t.Fatalf("disabled = %v, want indexer 2", dis)
	}

	f.mu.Lock()
	f.statusFail = true
	f.mu.Unlock()
	e.mu.Lock()
	e.disabled = map[int]time.Time{1: time.Now().Add(-time.Minute), 2: e.disabled[2]}
	e.mu.Unlock()
	expireIndexers(e)
	e.Indexers(bg)
	waitFor(t, "the refresh to land", func() bool { return indexersFresh(e) })

	// Indexer 2 stays disabled; indexer 1's ban ran out, so it is not kept.
	if _, dis, _ := e.Indexers(bg); len(dis) != 1 || dis[2].IsZero() {
		t.Fatalf("disabled after a failed status lookup = %v, want indexer 2", dis)
	}
}

// BenchmarkRunCached is the hot path of a repeat search: a warm indexer list
// and every query answered from the cache.
func BenchmarkRunCached(b *testing.B) {
	e := newTestEngine(b, &fakeProwlarr{order: map[string][]string{}})
	req := Request{Kind: "tv", Title: "Show", TvdbID: 1, Broad: true, Seasons: []int{1}, PrefetchSeasons: []int{2, 3, 4, 5, 6}}
	run := func() { e.Run(context.Background(), req, nil, func(Batch) {}) }
	run()
	b.ReportAllocs()
	for b.Loop() {
		run()
	}
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
