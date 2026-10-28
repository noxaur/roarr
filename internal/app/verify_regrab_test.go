package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/verify"
)

const badHash = "abcdef0123456789abcdef0123456789abcdef01"

// fakeStack is a Sonarr and a qBittorrent that record what roarr asks.
type fakeStack struct {
	mu sync.Mutex

	queue       []map[string]any
	queueFail   bool // GET /queue answers 500
	removeFail  bool // DELETE /queue/{id} answers 500
	torrents    []string
	keepTorrent bool // qBittorrent ignores deletes
	episodes    []arr.Episode
	files       map[int]string // episode file id -> path
	history     map[int][]arr.HistoryRecord
	deleted     []int
	calls       []string
}

func (f *fakeStack) sonarr(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	p := strings.TrimPrefix(r.URL.Path, "/api/v3")
	switch {
	case p == "/queue" && r.Method == http.MethodGet:
		if f.queueFail {
			http.Error(w, "boom", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": len(f.queue), "records": f.queue})
	case strings.HasPrefix(p, "/queue/") && r.Method == http.MethodDelete:
		if f.removeFail {
			http.Error(w, "boom", 500)
			return
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/queue/"))
		for i, q := range f.queue {
			if q["id"] == id {
				f.queue = append(f.queue[:i], f.queue[i+1:]...)
				break
			}
		}
		if r.URL.Query().Get("removeFromClient") == "true" && !f.keepTorrent {
			f.torrents = nil
		}
	case p == "/episode":
		_ = json.NewEncoder(w).Encode(f.episodes)
	case p == "/episodefile" && r.Method == http.MethodGet:
		var out []map[string]any
		for id, path := range f.files {
			out = append(out, map[string]any{"id": id, "path": path})
		}
		_ = json.NewEncoder(w).Encode(out)
	case p == "/episodefile/bulk" && r.Method == http.MethodDelete:
		var body struct {
			IDs []int `json:"episodeFileIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.deleted = append(f.deleted, body.IDs...)
	case p == "/history/series":
		ep, _ := strconv.Atoi(r.URL.Query().Get("episodeId"))
		_ = json.NewEncoder(w).Encode(f.history[ep])
	default:
		http.Error(w, "unexpected "+r.Method+" "+p, 404)
	}
}

func (f *fakeStack) qbit(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "qbit "+r.URL.Path)
	switch r.URL.Path {
	case "/api/v2/torrents/delete":
		if !f.keepTorrent {
			f.torrents = nil
		}
	case "/api/v2/torrents/info":
		out := []map[string]any{}
		for _, h := range f.torrents {
			out = append(out, map[string]any{"hash": h})
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func (f *fakeStack) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func newRegrabApp(t *testing.T, f *fakeStack) (*Verification, *Clients) {
	t.Helper()
	old := removalChecks
	removalChecks = []time.Duration{0, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { removalChecks = old })
	ss := httptest.NewServer(http.HandlerFunc(f.sonarr))
	qs := httptest.NewServer(http.HandlerFunc(f.qbit))
	t.Cleanup(ss.Close)
	t.Cleanup(qs.Close)
	dir := t.TempDir()
	a := &App{details: map[string]cachedDetail{}}
	a.clients = &Clients{Sonarr: arr.NewSonarr(ss.URL, "k"), QBit: qbit.New(qs.URL, "", "")}
	a.Watch = newWatchFirst(a, dir)
	v := &Verification{app: a, path: filepath.Join(dir, "verify.json"), sem: make(chan struct{}, 1), recs: map[string]*VerifyRecord{}}
	a.Verify = v
	v.recs[badHash] = &VerifyRecord{Hash: badHash, Source: "sonarr", Kind: "tv", SeriesID: 7, EpisodeIDs: []int{101, 102, 103, 104}, Release: "Show.S01-FAKE", State: "done",
		Verdict: &verify.Verdict{Level: verify.Fail, Summary: "fake"}}
	return v, a.clients
}

func imported(ep int, dl, path string, at time.Time) arr.HistoryRecord {
	return arr.HistoryRecord{EpisodeID: ep, EventType: "downloadFolderImported", DownloadID: dl, Date: at, Data: map[string]string{"importedPath": path}}
}

func TestRegrabDeletesOnlyFilesImportedByTheDownload(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	f := &fakeStack{
		queue:    []map[string]any{{"id": 1, "downloadId": strings.ToUpper(badHash), "seriesId": 7}, {"id": 2, "downloadId": strings.ToUpper(badHash), "seriesId": 7}},
		torrents: []string{badHash},
		episodes: []arr.Episode{{ID: 101, EpisodeFileID: 11}, {ID: 102, EpisodeFileID: 12}, {ID: 103, EpisodeFileID: 13}, {ID: 104, EpisodeFileID: 14}, {ID: 105, EpisodeFileID: 15}},
		files:    map[int]string{11: "/tv/Show/S01E01.mkv", 12: "/tv/Show/S01E02.mkv", 13: "/tv/Show/S01E03.mkv", 14: "/tv/Show/S01E04.mkv", 15: "/tv/Show/S01E05.mkv"},
		history: map[int][]arr.HistoryRecord{
			// The bad download's file is still in place: delete.
			101: {{EventType: "grabbed", DownloadID: badHash, Date: t0}, imported(101, strings.ToUpper(badHash), "/tv/Show/S01E01.mkv", t0.Add(time.Hour))},
			// A newer, good download replaced it since: keep.
			102: {imported(102, badHash, "/tv/Show/S01E02.mkv", t0.Add(time.Hour)), imported(102, "other", "/tv/Show/S01E02.mkv", t0.Add(48*time.Hour))},
			// Already in the library before, never imported by this download: keep.
			103: {{EventType: "seriesFolderImported", Date: t0.Add(-time.Hour), Data: map[string]string{"importedPath": "/tv/Show/S01E03.mkv"}}},
			// Imported by it, but the current file is another one: keep.
			104: {imported(104, badHash, "/tv/Show/old-name.mkv", t0.Add(time.Hour))},
		},
	}
	v, c := newRegrabApp(t, f)
	r, _ := v.Record(badHash)
	if err := v.removeDownload(context.Background(), c, r); err != nil {
		t.Fatal(err)
	}
	if len(f.queue) != 0 || len(f.torrents) != 0 {
		t.Fatalf("queue %v torrents %v left", f.queue, f.torrents)
	}
	deleted, kept, err := v.deleteImported(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || kept != 3 || !reflect.DeepEqual(f.deleted, []int{11}) {
		t.Fatalf("deleted=%d kept=%d ids=%v", deleted, kept, f.deleted)
	}
}

func TestRegrabNoHistoryDeletesNothing(t *testing.T) {
	f := &fakeStack{
		episodes: []arr.Episode{{ID: 101, EpisodeFileID: 11}},
		files:    map[int]string{11: "/tv/Show/S01E01.mkv"},
	}
	v, c := newRegrabApp(t, f)
	r, _ := v.Record(badHash)
	deleted, kept, err := v.deleteImported(context.Background(), c, r)
	if err != nil || deleted != 0 || kept != 1 || len(f.deleted) != 0 {
		t.Fatalf("deleted=%d kept=%d ids=%v err=%v", deleted, kept, f.deleted, err)
	}
}

// When the download cannot be confirmed gone, re-grab stops: no library
// file is touched and no replacement is searched.
func TestRegrabStopsWhenRemovalFails(t *testing.T) {
	cases := map[string]func(*fakeStack){
		"queue unreadable": func(f *fakeStack) { f.queueFail = true },
		"queue removal":    func(f *fakeStack) { f.removeFail = true },
		"torrent stays":    func(f *fakeStack) { f.keepTorrent = true },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeStack{
				queue:    []map[string]any{{"id": 1, "downloadId": badHash, "seriesId": 7}},
				torrents: []string{badHash},
				episodes: []arr.Episode{{ID: 101, EpisodeFileID: 11}},
				files:    map[int]string{11: "/tv/Show/S01E01.mkv"},
				history:  map[int][]arr.HistoryRecord{101: {imported(101, badHash, "/tv/Show/S01E01.mkv", time.Now())}},
			}
			setup(f)
			v, _ := newRegrabApp(t, f)
			rec, err := v.Regrab(context.Background(), badHash, false)
			if err == nil {
				t.Fatal("want an error")
			}
			if rec.Regrab == nil || rec.Regrab.Error == "" || !strings.Contains(err.Error(), "retry") {
				t.Fatalf("error not recorded or not actionable: %v / %+v", err, rec.Regrab)
			}
			if f.called("GET /api/v3/episode") || f.called("GET /api/v3/history") || len(f.deleted) != 0 {
				t.Fatalf("library was touched: %v", f.calls)
			}
			if name == "queue unreadable" || name == "queue removal" {
				if f.called("qbit /api/v2/torrents/delete") {
					t.Fatal("torrent deleted although the queue step failed")
				}
			}
		})
	}
}

func TestImportedBy(t *testing.T) {
	t0 := time.Now()
	cases := []struct {
		name string
		hist []arr.HistoryRecord
		ok   bool
	}{
		{"none", nil, false},
		{"ours", []arr.HistoryRecord{imported(1, badHash, "/a.mkv", t0)}, true},
		{"ours then other", []arr.HistoryRecord{imported(1, badHash, "/a.mkv", t0), imported(1, "x", "/a.mkv", t0.Add(time.Second))}, false},
		{"other then ours", []arr.HistoryRecord{imported(1, "x", "/a.mkv", t0), imported(1, badHash, "/a.mkv", t0.Add(time.Second))}, true},
		{"ours then rescan", []arr.HistoryRecord{imported(1, badHash, "/a.mkv", t0), {EventType: "seriesFolderImported", Date: t0.Add(time.Second)}}, false},
		{"grab only", []arr.HistoryRecord{{EventType: "grabbed", DownloadID: badHash, Date: t0}}, false},
		{"no path", []arr.HistoryRecord{imported(1, badHash, "", t0)}, false},
	}
	for _, c := range cases {
		if _, ok := importedBy(c.hist, badHash); ok != c.ok {
			t.Errorf("%s: ok=%v", c.name, ok)
		}
	}
}

func TestVerifySaveConcurrent(t *testing.T) {
	v := &Verification{path: filepath.Join(t.TempDir(), "verify.json"), recs: map[string]*VerifyRecord{}}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v.mu.Lock()
			h := strconv.Itoa(i)
			v.recs[h] = &VerifyRecord{Hash: h}
			v.mu.Unlock()
			v.save()
		}(i)
	}
	wg.Wait()
	// The last save on disk holds every record.
	v.save()
	loaded := newVerificationFromFile(t, v.path)
	if len(loaded) != 20 {
		t.Fatalf("saved %d records, want 20", len(loaded))
	}
}

func newVerificationFromFile(t *testing.T, p string) []*VerifyRecord {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var recs []*VerifyRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		t.Fatalf("verify.json corrupt: %v", err)
	}
	return recs
}
