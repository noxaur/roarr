// Command bench measures how quickly different search strategies return
// release results from the lab stack (see lab/docker-compose.yml).
//
// Strategies:
//
//	sonarr-episode   GET Sonarr /api/v3/release?episodeId=  (original pulldeck path)
//	sonarr-season    GET Sonarr /api/v3/release?seriesId=&seasonNumber=
//	prowlarr-all     one aggregated Prowlarr /api/v1/search across every indexer
//	fanout-json      one Prowlarr /api/v1/search per indexer, in parallel, caps-aware
//	fanout-torznab   one Prowlarr /{id}/api Torznab call per indexer, in parallel
//
// For every run it records time-to-first-result, time until 90% of the final
// result count was known, time-to-complete, and how many upstream indexer
// requests the strategy cost (from mockindexer /stats).
package main

import (
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

func main() {
	strategies := flag.String("s", "sonarr-episode,sonarr-season,prowlarr-all,fanout-json,fanout-torznab", "strategies to run")
	runs := flag.Int("n", 6, "episodes per strategy")
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
		if e.SeasonNumber > 0 && e.EpisodeNumber%3 == 1 && len(targets) < *runs {
			targets = append(targets, target{SeriesID: 2, Title: "The Expanse", TVDB: 280619, Season: e.SeasonNumber, Episode: e.EpisodeNumber, EpisodeID: e.ID})
		}
	}

	var all []result
	for _, s := range strings.Split(*strategies, ",") {
		for _, t := range targets {
			var fn func(*arrival) int
			switch s {
			case "sonarr-episode":
				fn = sonarrEpisode(t)
			case "sonarr-season":
				fn = sonarrSeason(t)
			case "prowlarr-all":
				fn = prowlarrAll(t, ixs)
			case "fanout-json":
				fn = fanoutJSON(t, ixs, *perIndexer)
			case "fanout-torznab":
				fn = fanoutTorznab(t, ixs, *perIndexer)
			default:
				panic("unknown strategy " + s)
			}
			r := run(s, t, fn)
			fmt.Printf("%-15s %-22s first=%6.2fs p90=%6.2fs all=%6.2fs results=%4d reqs=%3d failed=%d\n", r.Strategy, r.Target, r.First.Seconds(), r.P90.Seconds(), r.All.Seconds(), r.Count, r.Requests, r.Failed)
			all = append(all, r)
		}
	}

	fmt.Println("\nmedians")
	fmt.Printf("%-15s %8s %8s %8s %8s %6s\n", "strategy", "first", "p90", "all", "results", "reqs")
	for _, s := range strings.Split(*strategies, ",") {
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
