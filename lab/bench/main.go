// Command bench measures how quickly different search strategies return
// release results from the lab stack (see lab/docker-compose.yml).
//
// Strategies:
//
//	sonarr-episode   GET Sonarr /api/v3/release?episodeId=  (what roarr used at first)
//	sonarr-season    GET Sonarr /api/v3/release?seriesId=&seasonNumber=
//	prowlarr-all     one aggregated Prowlarr /api/v1/search across every indexer
//	fanout-json      one Prowlarr /api/v1/search per indexer, in parallel, caps-aware
//	fanout-torznab   one Prowlarr /{id}/api Torznab call per indexer, in parallel
//	roarr            roarr's own /api/search event stream, reported as two rows:
//	                 roarr-cold, then roarr-warm for the same search repeated at once
//
// For every run it records time-to-first-result, time until 90% of the final
// result count was known, time-to-complete, and how many upstream indexer
// requests the strategy cost (from mockindexer /stats).
//
// Each run searches the first episode of a different season. roarr caches
// the episode query and its season query, so a second episode of one season
// would not be a cold run.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	sonarrURL   = "http://localhost:8989"
	sonarrKey   = "0123456789abcdef0123456789abcd01"
	prowlarrURL = "http://localhost:9696"
	prowlarrKey = "0123456789abcdef0123456789abcd03"
	roarrURL    = "http://localhost:3847"
	mockURL     = "http://localhost:9117"
	client      = &http.Client{Timeout: 150 * time.Second}
)

type target struct {
	SeriesID  int
	Title     string
	TVDB      int
	Season    int
	Episode   int
	EpisodeID int
}

type indexer struct {
	ID       int
	Name     string
	IDSearch bool
}

type result struct {
	Strategy string
	Target   string
	First    time.Duration
	P90      time.Duration
	All      time.Duration
	Count    int
	Requests int
	Failed   int
}

func get(u string, key string, out any) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("X-Api-Key", key)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s: %d %s", u, res.StatusCode, b[:min(len(b), 200)])
	}
	if out == nil {
		_, err = io.Copy(io.Discard, res.Body)
		return err
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// arrival tracks when each batch of results showed up.
type arrival struct {
	mu    sync.Mutex
	start time.Time
	marks []mark
}

type mark struct {
	at time.Duration
	n  int
}

func (a *arrival) add(n int) {
	a.mu.Lock()
	a.marks = append(a.marks, mark{time.Since(a.start), n})
	a.mu.Unlock()
}

func (a *arrival) summarize(r *result) {
	total := 0
	for _, m := range a.marks {
		total += m.n
	}
	r.Count = total
	sort.Slice(a.marks, func(i, j int) bool { return a.marks[i].at < a.marks[j].at })
	seen := 0
	for _, m := range a.marks {
		if m.n > 0 && r.First == 0 {
			r.First = m.at
		}
		seen += m.n
		if r.P90 == 0 && float64(seen) >= 0.9*float64(total) && total > 0 {
			r.P90 = m.at
		}
	}
}

func resetStats() { _ = get(mockURL+"/stats/reset", "", nil) }

func countStats() int {
	var s []json.RawMessage
	_ = get(mockURL+"/stats", "", &s)
	return len(s)
}

func run(name string, t target, fn func(*arrival) int) result {
	resetStats()
	a := &arrival{start: time.Now()}
	failed := fn(a)
	r := result{Strategy: name, Target: fmt.Sprintf("%s S%02dE%02d", t.Title, t.Season, t.Episode), All: time.Since(a.start), Failed: failed}
	a.summarize(&r)
	time.Sleep(300 * time.Millisecond) // let late requests land in stats
	r.Requests = countStats()
	return r
}

func sonarrEpisode(t target) func(*arrival) int {
	return func(a *arrival) int {
		var rel []json.RawMessage
		if err := get(fmt.Sprintf("%s/api/v3/release?episodeId=%d", sonarrURL, t.EpisodeID), sonarrKey, &rel); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		a.add(len(rel))
		return 0
	}
}

func sonarrSeason(t target) func(*arrival) int {
	return func(a *arrival) int {
		var rel []json.RawMessage
		if err := get(fmt.Sprintf("%s/api/v3/release?seriesId=%d&seasonNumber=%d", sonarrURL, t.SeriesID, t.Season), sonarrKey, &rel); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		a.add(len(rel))
		return 0
	}
}

// query builds the per-indexer Prowlarr search query for an episode.
func query(t target, ix indexer) url.Values {
	v := url.Values{}
	v.Set("type", "tvsearch")
	v.Set("limit", "100")
	v.Add("categories", "5000")
	if ix.IDSearch {
		v.Set("query", fmt.Sprintf("{TvdbId:%d}{Season:%02d}{Episode:%02d}", t.TVDB, t.Season, t.Episode))
	} else {
		v.Set("query", fmt.Sprintf("%s {Season:%02d}{Episode:%02d}", t.Title, t.Season, t.Episode))
	}
	v.Add("indexerIds", fmt.Sprint(ix.ID))
	return v
}

func prowlarrAll(t target, ixs []indexer) func(*arrival) int {
	return func(a *arrival) int {
		// What a naive client does: one text query across all indexers.
		v := url.Values{}
		v.Set("type", "tvsearch")
		v.Set("limit", "100")
		v.Add("categories", "5000")
		v.Set("query", fmt.Sprintf("%s {Season:%02d}{Episode:%02d}", t.Title, t.Season, t.Episode))
		for _, ix := range ixs {
			v.Add("indexerIds", fmt.Sprint(ix.ID))
		}
		var rel []json.RawMessage
		if err := get(prowlarrURL+"/api/v1/search?"+v.Encode(), prowlarrKey, &rel); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		a.add(len(rel))
		return 0
	}
}

func fanoutJSON(t target, ixs []indexer, perIndexer time.Duration) func(*arrival) int {
	return func(a *arrival) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		failed := 0
		for _, ix := range ixs {
			wg.Add(1)
			go func(ix indexer) {
				defer wg.Done()
				c := &http.Client{Timeout: perIndexer}
				req, _ := http.NewRequest("GET", prowlarrURL+"/api/v1/search?"+query(t, ix).Encode(), nil)
				req.Header.Set("X-Api-Key", prowlarrKey)
				res, err := c.Do(req)
				if err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				defer res.Body.Close()
				var rel []json.RawMessage
				if res.StatusCode >= 300 || json.NewDecoder(res.Body).Decode(&rel) != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				a.add(len(rel))
			}(ix)
		}
		wg.Wait()
		return failed
	}
}

type torznabFeed struct {
	Items []struct {
		Title string `xml:"title"`
	} `xml:"channel>item"`
}

func fanoutTorznab(t target, ixs []indexer, perIndexer time.Duration) func(*arrival) int {
	return func(a *arrival) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		failed := 0
		for _, ix := range ixs {
			wg.Add(1)
			go func(ix indexer) {
				defer wg.Done()
				v := url.Values{}
				v.Set("t", "tvsearch")
				v.Set("cat", "5000")
				v.Set("limit", "100")
				v.Set("apikey", prowlarrKey)
				v.Set("season", fmt.Sprint(t.Season))
				v.Set("ep", fmt.Sprint(t.Episode))
				if ix.IDSearch {
					v.Set("tvdbid", fmt.Sprint(t.TVDB))
				} else {
					v.Set("q", t.Title)
				}
				c := &http.Client{Timeout: perIndexer}
				res, err := c.Get(fmt.Sprintf("%s/%d/api?%s", prowlarrURL, ix.ID, v.Encode()))
				if err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				defer res.Body.Close()
				var feed torznabFeed
				if res.StatusCode >= 300 || xml.NewDecoder(res.Body).Decode(&feed) != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				a.add(len(feed.Items))
			}(ix)
		}
		wg.Wait()
		return failed
	}
}

// streamSearch reads a roarr /api/search event stream until its done event
// and marks each batch with the number of releases not seen before (a batch
// repeats a release when another indexer has it too). It returns how many
// batches came from roarr's cache and how many indexer queries failed.
func streamSearch(u string, a *arrival) (cached, failed int, err error) {
	res, err := client.Get(u)
	if err != nil {
		return 0, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return 0, 0, fmt.Errorf("%s: %d %s", u, res.StatusCode, b[:min(len(b), 200)])
	}
	seen := map[string]bool{}
	br := bufio.NewReader(res.Body)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return cached, failed, fmt.Errorf("%s: stream ended before done: %w", u, err)
		}
		data, ok := bytes.CutPrefix(line, []byte("data: "))
		if !ok {
			continue // blank separator or ": ping"
		}
		var ev struct {
			Type  string `json:"type"`
			Batch *struct {
				Cached bool   `json:"cached"`
				Error  string `json:"error"`
			} `json:"batch"`
			Releases []struct {
				Key string `json:"key"`
			} `json:"releases"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return cached, failed, fmt.Errorf("%s: %w", u, err)
		}
		switch ev.Type {
		case "batch":
			if ev.Batch != nil && ev.Batch.Cached {
				cached++
			}
			if ev.Batch != nil && ev.Batch.Error != "" {
				failed++
			}
			n := 0
			for _, r := range ev.Releases {
				if !seen[r.Key] {
					seen[r.Key] = true
					n++
				}
			}
			a.add(n)
		case "error":
			return cached, failed, fmt.Errorf("%s: %s", u, ev.Error)
		case "done":
			return cached, failed, nil
		}
	}
}

func roarrSearch(t target, cold bool) func(*arrival) int {
	return func(a *arrival) int {
		cached, failed, err := streamSearch(fmt.Sprintf("%s/api/search?kind=tv&id=%d&episodes=%d", roarrURL, t.SeriesID, t.EpisodeID), a)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return failed + 1
		}
		if cold && cached > 0 {
			fmt.Fprintf(os.Stderr, "roarr-cold: %d batches came from roarr's cache; restart the lab roarr for a cold run\n", cached)
		}
		return failed
	}
}

func main() {
	strategies := flag.String("s", "sonarr-episode,sonarr-season,prowlarr-all,fanout-json,fanout-torznab,roarr", "strategies to run")
	runs := flag.Int("n", 6, "episodes per strategy, at most one per season")
	perIndexer := flag.Duration("timeout", 12*time.Second, "per-indexer budget for fan-out strategies")
	jsonOut := flag.String("json", "", "write raw results here")
	flag.Parse()

	var ixRaw []struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Enable       bool   `json:"enable"`
		Capabilities struct {
			TVSearchParams []string `json:"tvSearchParams"`
		} `json:"capabilities"`
	}
	if err := get(prowlarrURL+"/api/v1/indexer", prowlarrKey, &ixRaw); err != nil {
		panic(err)
	}
	var ixs []indexer
	for _, i := range ixRaw {
		if !i.Enable {
			continue
		}
		ix := indexer{ID: i.ID, Name: i.Name}
		for _, p := range i.Capabilities.TVSearchParams {
			if p == "tvdbId" {
				ix.IDSearch = true
			}
		}
		ixs = append(ixs, ix)
	}

	var eps []struct {
		ID            int `json:"id"`
		SeasonNumber  int `json:"seasonNumber"`
		EpisodeNumber int `json:"episodeNumber"`
	}
	if err := get(sonarrURL+"/api/v3/episode?seriesId=2", sonarrKey, &eps); err != nil {
		panic(err)
	}
	var targets []target
	for _, e := range eps {
		if e.SeasonNumber > 0 && e.EpisodeNumber == 1 && len(targets) < *runs {
			targets = append(targets, target{SeriesID: 2, Title: "The Expanse", TVDB: 280619, Season: e.SeasonNumber, Episode: e.EpisodeNumber, EpisodeID: e.ID})
		}
	}

	var all []result
	var names []string
	for _, s := range strings.Split(*strategies, ",") {
		if s == "roarr" {
			names = append(names, "roarr-cold", "roarr-warm")
		} else {
			names = append(names, s)
		}
		for _, t := range targets {
			fns := map[string]func(*arrival) int{}
			switch s {
			case "sonarr-episode":
				fns[s] = sonarrEpisode(t)
			case "sonarr-season":
				fns[s] = sonarrSeason(t)
			case "prowlarr-all":
				fns[s] = prowlarrAll(t, ixs)
			case "fanout-json":
				fns[s] = fanoutJSON(t, ixs, *perIndexer)
			case "fanout-torznab":
				fns[s] = fanoutTorznab(t, ixs, *perIndexer)
			case "roarr":
				fns["roarr-cold"] = roarrSearch(t, true)
				fns["roarr-warm"] = roarrSearch(t, false)
			default:
				panic("unknown strategy " + s)
			}
			for _, name := range names[len(names)-len(fns):] {
				r := run(name, t, fns[name])
				fmt.Printf("%-15s %-22s first=%6.2fs p90=%6.2fs all=%6.2fs results=%4d reqs=%3d failed=%d\n", r.Strategy, r.Target, r.First.Seconds(), r.P90.Seconds(), r.All.Seconds(), r.Count, r.Requests, r.Failed)
				all = append(all, r)
			}
		}
	}

	fmt.Println("\nmedians")
	fmt.Printf("%-15s %8s %8s %8s %8s %6s\n", "strategy", "first", "p90", "all", "results", "reqs")
	for _, s := range names {
		var f, p, a, c, q []float64
		for _, r := range all {
			if r.Strategy == s {
				f = append(f, r.First.Seconds())
				p = append(p, r.P90.Seconds())
				a = append(a, r.All.Seconds())
				c = append(c, float64(r.Count))
				q = append(q, float64(r.Requests))
			}
		}
		fmt.Printf("%-15s %7.2fs %7.2fs %7.2fs %8.0f %6.0f\n", s, median(f), median(p), median(a), median(c), median(q))
	}
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(all, "", "  ")
		_ = os.WriteFile(*jsonOut, b, 0o644)
	}
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[len(s)/2]
}
