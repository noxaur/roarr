// Package search fans a request out to every Prowlarr indexer in parallel
// and hands results back the moment each indexer answers.
//
// Prowlarr rate-limits each indexer (one request at a time, ~2s apart), so
// firing every query at once only builds an invisible queue inside Prowlarr.
// Instead roarr keeps its own queue per indexer and orders it: the exact
// scope the user is looking at first, then the show-wide query, then
// prefetching of other seasons. Results are cached per (indexer, query) and
// identical queries share one upstream call, so a page that opens warms the
// cache for whatever the user clicks next.
package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/arr"
)

// Priority orders queries waiting for the same indexer (lower runs first).
type Priority int

const (
	PFocus    Priority = iota // what the user is looking at right now
	PShow                     // show-wide context (complete packs, broad hits)
	PPrefetch                 // seasons the user may click next
)

// Request describes what to look for.
type Request struct {
	Kind    string   // "tv" or "movie"
	Title   string   // canonical title
	Aliases []string // alternate / scene titles (anime romaji, etc.)
	Year    int
	TvdbID  int
	TmdbID  int
	ImdbID  string
	Anime   bool

	Broad           bool     // title/id-level query (complete packs, movies)
	BroadPriority   Priority // priority of the broad query
	Seasons         []int    // focus seasons
	PrefetchSeasons []int    // other seasons, fetched in the background
	Episodes        []EpQuery
}

// EpQuery is an episode-level query.
type EpQuery struct {
	Season, Episode, Absolute int
}

// Batch is one indexer answering one query.
type Batch struct {
	IndexerID int                `json:"indexerId"`
	Indexer   string             `json:"indexer"`
	Label     string             `json:"label"`
	MS        int                `json:"ms"`
	Cached    bool               `json:"cached"`
	Error     string             `json:"error,omitempty"`
	Results   []arr.SearchResult `json:"-"`
}

// IndexerPlan tells the client which indexers will be queried.
type IndexerPlan struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Queries  int    `json:"queries"`
	Disabled string `json:"disabled,omitempty"`
}

// Engine runs searches.
type Engine struct {
	Prowlarr *arr.Prowlarr
	Timeout  time.Duration // hard per-query budget
	TTL      time.Duration // result cache lifetime

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	cache    map[string]*entry
	queues   map[int]*queue
	seq      uint64
	ixAt     time.Time
	ix       []arr.Indexer
	disabled map[int]time.Time
}

type entry struct {
	j       job
	done    chan struct{}
	at      time.Time
	results []arr.SearchResult
	err     error
	ms      int
	// queue state (guarded by Engine.mu)
	queued  bool
	prio    Priority
	seq     uint64
	waiters int
}

type queue struct {
	pending []*entry
	wake    chan struct{}
}

var errAbandoned = errors.New("abandoned")

// errTTL is how long a failed query is remembered before it is retried.
const errTTL = 90 * time.Second

// NewEngine returns an engine with defaults.
func NewEngine(p *arr.Prowlarr) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{Prowlarr: p, Timeout: 30 * time.Second, TTL: 20 * time.Minute, ctx: ctx, cancel: cancel,
		cache: map[string]*entry{}, queues: map[int]*queue{}}
}

// Close stops the per-indexer workers.
func (e *Engine) Close() { e.cancel() }

// Indexers returns enabled indexers (cached for a few minutes).
func (e *Engine) Indexers(ctx context.Context) ([]arr.Indexer, map[int]time.Time, error) {
	e.mu.Lock()
	if time.Since(e.ixAt) < 3*time.Minute && e.ix != nil {
		ix, dis := e.ix, e.disabled
		e.mu.Unlock()
		return ix, dis, nil
	}
	e.mu.Unlock()
	all, err := e.Prowlarr.AllIndexers(ctx)
	if err != nil {
		return nil, nil, err
	}
	dis, _ := e.Prowlarr.DisabledIndexers(ctx)
	var on []arr.Indexer
	for _, i := range all {
		if i.Enable {
			on = append(on, i)
		}
	}
	sort.Slice(on, func(a, b int) bool {
		return on[a].Priority < on[b].Priority || on[a].Priority == on[b].Priority && on[a].ID < on[b].ID
	})
	e.mu.Lock()
	e.ix, e.disabled, e.ixAt = on, dis, time.Now()
	e.mu.Unlock()
	return on, dis, nil
}

type job struct {
	ix    arr.Indexer
	q     arr.Query
	label string
	prio  Priority
}

// plan builds the per-indexer queries for a request.
func (e *Engine) plan(req Request, indexers []arr.Indexer) []job {
	var jobs []job
	for _, ix := range indexers {
		if req.Kind == "movie" {
			if !ix.HasCategory(2000, 3000) {
				continue
			}
			q := arr.Query{IndexerID: ix.ID, Type: "movie", Categories: []int{2000}, Limit: limit(ix)}
			switch {
			case req.ImdbID != "" && ix.MovieParam("imdbId"):
				q.Term = fmt.Sprintf("{ImdbId:%s}", req.ImdbID)
			case req.TmdbID != 0 && ix.MovieParam("tmdbId"):
				q.Term = fmt.Sprintf("{TmdbId:%d}", req.TmdbID)
			default:
				q.Type = "search"
				q.Term = cleanQuery(req.Title)
				if req.Year > 0 {
					q.Term += " " + fmt.Sprint(req.Year)
				}
			}
			jobs = append(jobs, job{ix, q, "movie", PFocus})
			continue
		}

		if !ix.HasCategory(5000, 6000) {
			continue
		}
		cats := []int{5000}
		if req.Anime {
			cats = []int{5000, 5070}
		}
		byID := ""
		switch {
		case req.TvdbID != 0 && ix.TVParam("tvdbId"):
			byID = fmt.Sprintf("{TvdbId:%d}", req.TvdbID)
		case req.ImdbID != "" && ix.TVParam("imdbId"):
			byID = fmt.Sprintf("{ImdbId:%s}", req.ImdbID)
		}
		titles := []string{req.Title}
		if req.Anime {
			// Fansub trackers name things by romaji title; query the first
			// distinct alias as well.
			for _, a := range req.Aliases {
				if !strings.EqualFold(cleanQuery(a), cleanQuery(req.Title)) {
					titles = append(titles, a)
					break
				}
			}
		}
		add := func(typ, term, label string, p Priority) {
			jobs = append(jobs, job{ix, arr.Query{IndexerID: ix.ID, Type: typ, Term: term, Categories: cats, Limit: limit(ix)}, label, p})
		}
		tvType := "tvsearch"
		if !ix.SupportsTV() {
			tvType = "search"
		}
		base := byID
		if base == "" {
			base = cleanQuery(req.Title) + " "
		}
		for _, ep := range req.Episodes {
			if req.Anime && ep.Absolute > 0 && byID == "" {
				for _, t := range titles {
					add("search", fmt.Sprintf("%s %02d", cleanQuery(t), ep.Absolute), fmt.Sprintf("#%d", ep.Absolute), PFocus)
				}
				continue
			}
			add(tvType, fmt.Sprintf("%s{Season:%02d}{Episode:%02d}", base, ep.Season, ep.Episode), fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode), PFocus)
		}
		seasonJobs := func(seasons []int, p Priority) {
			for _, s := range seasons {
				if req.Anime && byID == "" {
					continue // anime trackers rarely tag seasons; the broad query covers it
				}
				add(tvType, fmt.Sprintf("%s{Season:%02d}", base, s), fmt.Sprintf("S%02d", s), p)
			}
		}
		seasonJobs(req.Seasons, PFocus)
		if req.Broad {
			if byID != "" {
				add(tvType, byID, "show", req.BroadPriority)
			}
			if byID == "" || req.Anime {
				for _, t := range titles {
					add("search", cleanQuery(t), "show", req.BroadPriority)
				}
			}
		}
		seasonJobs(req.PrefetchSeasons, PPrefetch)
	}
	return jobs
}

func limit(ix arr.Indexer) int {
	if ix.Capabilities.LimitsMax > 0 && ix.Capabilities.LimitsMax < 100 {
		return ix.Capabilities.LimitsMax
	}
	return 100
}

// cleanQuery strips characters trackers choke on ("Frieren: Beyond" -> "Frieren Beyond").
func cleanQuery(s string) string {
	r := strings.NewReplacer(":", " ", "'", "", "’", "", ",", " ", "!", "", "?", "", "&", "and", "(", " ", ")", " ", ".", " ")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

// Run executes the request and calls emit for every batch as soon as it is
// known. It returns when every query finished or ctx ended. emit is never
// called concurrently.
func (e *Engine) Run(ctx context.Context, req Request, onPlan func([]IndexerPlan), emit func(Batch)) error {
	indexers, disabled, err := e.Indexers(ctx)
	if err != nil {
		return err
	}
	jobs := e.plan(req, indexers)
	counts := map[int]int{}
	for _, j := range jobs {
		if _, off := disabled[j.ix.ID]; !off {
			counts[j.ix.ID]++
		}
	}
	var plans []IndexerPlan
	for _, ix := range indexers {
		p := IndexerPlan{ID: ix.ID, Name: ix.Name, Queries: counts[ix.ID]}
		if until, ok := disabled[ix.ID]; ok {
			p.Disabled = "disabled by Prowlarr until " + until.Local().Format("15:04")
		}
		if p.Queries > 0 || p.Disabled != "" {
			plans = append(plans, p)
		}
	}
	if onPlan != nil {
		onPlan(plans)
	}

	var emitMu sync.Mutex
	var wg sync.WaitGroup
	for _, j := range jobs {
		if _, off := disabled[j.ix.ID]; off {
			continue
		}
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			b := e.fetch(ctx, j)
			if ctx.Err() != nil {
				return
			}
			emitMu.Lock()
			emit(b)
			emitMu.Unlock()
		}(j)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return nil
}

// fetch returns a cached answer, joins an identical in-flight or queued
// query (raising its priority if needed), or queues a new one.
func (e *Engine) fetch(ctx context.Context, j job) Batch {
	key := j.q.Key()
	b := Batch{IndexerID: j.ix.ID, Indexer: j.ix.Name, Label: j.label}

	e.mu.Lock()
	en, ok := e.cache[key]
	if ok {
		select {
		case <-en.done:
			// Failures are remembered briefly so one dead indexer does not
			// add its full timeout to every search.
			if time.Since(en.at) > e.TTL || (en.err != nil && time.Since(en.at) > errTTL) || errors.Is(en.err, errAbandoned) {
				ok = false
			}
		default:
		}
	}
	if ok {
		b.Cached = !en.queued && isClosed(en.done)
		if en.queued && j.prio < en.prio {
			en.prio = j.prio
		}
	} else {
		e.seq++
		en = &entry{j: j, done: make(chan struct{}), queued: true, prio: j.prio, seq: e.seq}
		e.cache[key] = en
		e.enqueue(en)
	}
	en.waiters++
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		en.waiters--
		e.mu.Unlock()
	}()
	select {
	case <-en.done:
	case <-ctx.Done():
		return b
	}
	b.Results, b.MS = en.results, en.ms
	if b.Cached {
		b.MS = 0
	}
	if en.err != nil {
		b.Error = en.err.Error()
	}
	return b
}

func isClosed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// enqueue adds an entry to its indexer's queue; e.mu must be held.
func (e *Engine) enqueue(en *entry) {
	id := en.j.ix.ID
	q, ok := e.queues[id]
	if !ok {
		q = &queue{wake: make(chan struct{}, 1)}
		e.queues[id] = q
		go e.worker(q)
	}
	q.pending = append(q.pending, en)
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// next pops the most urgent entry; e.mu must be held.
func (q *queue) next() *entry {
	best := -1
	for i, en := range q.pending {
		if best < 0 || en.prio < q.pending[best].prio || en.prio == q.pending[best].prio && en.seq < q.pending[best].seq {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	en := q.pending[best]
	q.pending = append(q.pending[:best], q.pending[best+1:]...)
	return en
}

// worker runs one indexer's queries one at a time, most urgent first.
func (e *Engine) worker(q *queue) {
	for {
		e.mu.Lock()
		en := q.next()
		if en != nil {
			en.queued = false
			// Nobody is waiting for background prefetch any more (the user
			// left the page): drop it instead of spending indexer quota.
			if en.waiters == 0 && en.prio == PPrefetch {
				delete(e.cache, en.j.q.Key())
				en.err = errAbandoned
				close(en.done)
				e.mu.Unlock()
				continue
			}
		}
		e.mu.Unlock()
		if en == nil {
			select {
			case <-q.wake:
				continue
			case <-e.ctx.Done():
				return
			}
		}
		e.run(en)
	}
}

func (e *Engine) run(en *entry) {
	ctx, cancel := context.WithTimeout(e.ctx, e.Timeout)
	defer cancel()
	start := time.Now()
	res, err := e.Prowlarr.Search(ctx, en.j.q)
	e.mu.Lock()
	en.ms = int(time.Since(start).Milliseconds())
	en.results, en.err, en.at = res, err, time.Now()
	e.mu.Unlock()
	if err != nil {
		slog.Debug("indexer query failed", "indexer", en.j.ix.Name, "query", en.j.q.Term, "err", err)
	}
	close(en.done)
}

// Sweep drops expired cache entries. Call periodically.
func (e *Engine) Sweep() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, en := range e.cache {
		if isClosed(en.done) && time.Since(en.at) > e.TTL {
			delete(e.cache, k)
		}
	}
}

// Flush clears finished cache entries.
func (e *Engine) Flush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, en := range e.cache {
		if isClosed(en.done) {
			delete(e.cache, k)
		}
	}
}
