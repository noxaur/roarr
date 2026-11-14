package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/qbit"
)

func TestWatchPlanMatchesLikeTheImporter(t *testing.T) {
	episodes := []arr.Episode{
		{ID: 101, SeasonNumber: 1, EpisodeNumber: 1, HasFile: true},
		{ID: 205, SeasonNumber: 2, EpisodeNumber: 5},
		{ID: 206, SeasonNumber: 2, EpisodeNumber: 6},
		{ID: 301, SeasonNumber: 3, EpisodeNumber: 1},
	}
	sonarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/v3") {
		case "/series/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "title": "Show"})
		case "/episode":
			_ = json.NewEncoder(w).Encode(episodes)
		case "/episodefile":
			_, _ = w.Write([]byte("[]"))
		case "/queue":
			_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 0, "records": []any{}})
		default:
			http.Error(w, "unexpected "+r.URL.Path, 404)
		}
	}))
	defer sonarr.Close()
	var mu sync.Mutex
	prio := map[int]int{} // file index -> priority roarr set
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/torrents/filePrio" {
			http.Error(w, "unexpected "+r.URL.Path, 404)
			return
		}
		p, _ := strconv.Atoi(r.FormValue("priority"))
		mu.Lock()
		defer mu.Unlock()
		for _, id := range strings.Split(r.FormValue("id"), "|") {
			n, _ := strconv.Atoi(id)
			prio[n] = p
		}
	}))
	defer qb.Close()
	a := &App{details: map[string]cachedDetail{}}
	a.clients = &Clients{Sonarr: arr.NewSonarr(sonarr.URL, "k"), QBit: qbit.New(qb.URL, "", "")}
	w := newWatchFirst(a, t.TempDir())

	j := &WatchJob{Hash: "abc", Title: "Show.S01-S03.1080p.BluRay.x264-GRP", SeriesID: 7}
	w.plan(context.Background(), a.clients, j, []qbit.File{
		{Index: 0, Name: "Show.S01-S03/Season 1/Show.S01E01.mkv"}, // already in the library
		{Index: 1, Name: "Show.S01-S03/Season 2/05.mkv"},
		{Index: 2, Name: "Show.S01-S03/Season 2/Episode 06.mkv.!qB"},
		{Index: 3, Name: "Show.S01-S03/Season 3/Show.S03E01.m2ts"},
		{Index: 4, Name: "Show.S01-S03/Sample/Show.S02E05.mkv"},
		{Index: 5, Name: "Show.S01-S03/Extras/Making Of.mkv"},
		{Index: 6, Name: "Show.S01-S03/Show.nfo"},
	})

	type mapped struct {
		Index   int
		Name    string
		IDs     []int
		Skipped bool
	}
	var got []mapped
	for _, f := range j.Files {
		got = append(got, mapped{f.Index, f.Name, f.EpisodeIDs, f.Skipped})
	}
	want := []mapped{
		{0, "Show.S01-S03/Season 1/Show.S01E01.mkv", []int{101}, true},
		{1, "Show.S01-S03/Season 2/05.mkv", []int{205}, false},
		{2, "Show.S01-S03/Season 2/Episode 06.mkv", []int{206}, false},
		{3, "Show.S01-S03/Season 3/Show.S03E01.m2ts", []int{301}, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files:\n got %+v\nwant %+v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	var skipped []int
	for idx, p := range prio {
		if p != qbit.PrioSkip {
			t.Errorf("file %d got priority %d", idx, p)
		}
		skipped = append(skipped, idx)
	}
	sort.Ints(skipped)
	if !reflect.DeepEqual(skipped, []int{0, 4, 5}) {
		t.Errorf("skip priority on %v, want [0 4 5]", skipped)
	}
}

func TestWatchConcurrentSavesKeepEveryJob(t *testing.T) {
	dir := t.TempDir()
	a := &App{details: map[string]cachedDetail{}}
	w := newWatchFirst(a, dir)
	// Older (smaller) snapshots take longer to write, so without
	// serialisation a stale snapshot would land last.
	orig := writeJobs
	defer func() { writeJobs = orig }()
	writeJobs = func(name string, b []byte, perm os.FileMode) error {
		time.Sleep(time.Duration(50-strings.Count(string(b), `"hash"`)) * 200 * time.Microsecond)
		return os.WriteFile(name, b, perm)
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.Track(WatchJob{Hash: fmt.Sprintf("h%02d", i), Kind: "tv"})
		}(i)
	}
	wg.Wait()
	if got := len(newWatchFirst(a, dir).Jobs()); got != 40 {
		t.Fatalf("watch.json holds %d jobs, want 40", got)
	}
}
