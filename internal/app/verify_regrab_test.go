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
	files       map[int]string      // episode file id -> path
	history     []arr.HistoryRecord // the whole series' (or movie's) history
	movieFile   map[string]any
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
	case p == "/history/series", p == "/history/movie":
		// Like the real Sonarr, /history/series ignores episodeId.
		_ = json.NewEncoder(w).Encode(f.history)
	case strings.HasPrefix(p, "/movie/"):
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "tmdbId": 99, "movieFile": f.movieFile})
	case strings.HasPrefix(p, "/moviefile/") && r.Method == http.MethodDelete:
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/moviefile/"))
		f.deleted = append(f.deleted, id)
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
	a.clients = &Clients{Sonarr: arr.NewSonarr(ss.URL, "k"), Radarr: arr.NewRadarr(ss.URL, "k"), QBit: qbit.New(qs.URL, "", "")}
	a.Watch = newWatchFirst(a, dir)
	v := &Verification{app: a, path: filepath.Join(dir, "verify.json"), sem: make(chan struct{}, 1), recs: map[string]*VerifyRecord{}}
	a.Verify = v
	v.recs[badHash] = &VerifyRecord{Hash: badHash, Source: "sonarr", Kind: "tv", SeriesID: 7, EpisodeIDs: []int{101, 102, 103, 104}, Release: "Show.S01-FAKE", State: "done",
		Verdict: &verify.Verdict{Level: verify.Fail, Summary: "fake"}}
	return v, a.clients
}

func imported(ep, fileID int, dl, path string, at time.Time) arr.HistoryRecord {
	return arr.HistoryRecord{EpisodeID: ep, EventType: "downloadFolderImported", DownloadID: dl, Date: at, Data: map[string]string{"importedPath": path, "fileId": strconv.Itoa(fileID)}}
}

func TestRegrabDeletesOnlyFilesImportedByTheDownload(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	bad := strings.ToUpper(badHash)
	f := &fakeStack{
		queue:    []map[string]any{{"id": 1, "downloadId": bad, "seriesId": 7}, {"id": 2, "downloadId": bad, "seriesId": 7}},
		torrents: []string{badHash},
		episodes: []arr.Episode{{ID: 101, EpisodeFileID: 11}, {ID: 102, EpisodeFileID: 12}, {ID: 103, EpisodeFileID: 13}, {ID: 104, EpisodeFileID: 14}, {ID: 105, EpisodeFileID: 15}, {ID: 106, EpisodeFileID: 26}, {ID: 107, EpisodeFileID: 17}},
		files: map[int]string{11: "/tv/Show/S01E01.mkv", 12: "/tv/Show/S01E02.mkv", 13: "/tv/Show/S01E03.mkv", 14: "/tv/Show/S01E04.mkv",
			15: "/tv/Show/S01E05.mkv", 26: "/tv/Show/S01E06.mkv", 17: "/tv/Show/S01E07.mkv"},
		// One series history, as Sonarr returns it for every episode.
		history: []arr.HistoryRecord{
			{EpisodeID: 101, EventType: "grabbed", DownloadID: bad, Date: t0},
			// 101, 105: the pack's files are still in place: delete both.
			imported(101, 11, bad, "/tv/Show/S01E01.mkv", t0.Add(time.Hour)),
			imported(105, 15, bad, "/tv/Show/S01E05.mkv", t0.Add(time.Hour)),
			// 102: a newer, good download replaced it since: keep.
			imported(102, 12, badHash, "/tv/Show/S01E02.mkv", t0.Add(time.Hour)),
			imported(102, 12, "other", "/tv/Show/S01E02.mkv", t0.Add(48*time.Hour)),
			// 103: in the library before, never imported by this download: keep.
			{EpisodeID: 103, EventType: "seriesFolderImported", Date: t0.Add(-time.Hour), Data: map[string]string{"importedPath": "/tv/Show/S01E03.mkv"}},
			// 104: imported by it, but the current file is another one: keep.
			imported(104, 14, badHash, "/tv/Show/old-name.mkv", t0.Add(time.Hour)),
			// 106: replaced by a valid file at the same path and rescanned,
			// which leaves no new history: the file id differs, keep.
			imported(106, 16, badHash, "/tv/Show/S01E06.mkv", t0.Add(time.Hour)),
			// 107: not part of this download; its later import must not hide
			// the pack's imports of other episodes.
			imported(107, 17, "other", "/tv/Show/S01E07.mkv", t0.Add(72*time.Hour)),
		},
	}
	v, c := newRegrabApp(t, f)
	v.update(badHash, func(rec *VerifyRecord) { rec.EpisodeIDs = []int{101, 102, 103, 104, 105, 106} })
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
	if deleted != 2 || kept != 4 || !reflect.DeepEqual(f.deleted, []int{11, 15}) {
		t.Fatalf("deleted=%d kept=%d ids=%v", deleted, kept, f.deleted)
	}
}

func TestRegrabMovieNeedsSameFileID(t *testing.T) {
	for _, c := range []struct {
		name   string
		fileID int
		want   []int
	}{
		{"bad file still there", 31, []int{31}},
		{"replaced at the same path", 32, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeStack{
				movieFile: map[string]any{"id": c.fileID, "path": "/movies/M (2020)/M.mkv"},
				history:   []arr.HistoryRecord{{MovieID: 9, EventType: "downloadFolderImported", DownloadID: badHash, Date: time.Now(), Data: map[string]string{"importedPath": "/movies/M (2020)/M.mkv", "FileId": "31"}}},
			}
			v, cl := newRegrabApp(t, f)
			v.update(badHash, func(rec *VerifyRecord) { rec.Source, rec.Kind, rec.SeriesID, rec.MovieID = "radarr", "movie", 0, 9 })
			r, _ := v.Record(badHash)
			deleted, kept, err := v.deleteImported(context.Background(), cl, r)
			if err != nil || !reflect.DeepEqual(f.deleted, c.want) || deleted+kept != 1 {
				t.Fatalf("deleted=%d kept=%d ids=%v err=%v", deleted, kept, f.deleted, err)
			}
		})
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
				history:  []arr.HistoryRecord{imported(101, 11, badHash, "/tv/Show/S01E01.mkv", time.Now())},
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
		{"ours", []arr.HistoryRecord{imported(1, 5, badHash, "/a.mkv", t0)}, true},
		{"ours then other", []arr.HistoryRecord{imported(1, 5, badHash, "/a.mkv", t0), imported(1, 6, "x", "/a.mkv", t0.Add(time.Second))}, false},
		{"other then ours", []arr.HistoryRecord{imported(1, 6, "x", "/a.mkv", t0), imported(1, 5, badHash, "/a.mkv", t0.Add(time.Second))}, true},
		{"ours then rescan", []arr.HistoryRecord{imported(1, 5, badHash, "/a.mkv", t0), {EventType: "seriesFolderImported", Date: t0.Add(time.Second)}}, false},
		{"grab only", []arr.HistoryRecord{{EventType: "grabbed", DownloadID: badHash, Date: t0}}, false},
		{"no path", []arr.HistoryRecord{imported(1, 5, badHash, "", t0)}, false},
		{"no file id", []arr.HistoryRecord{{EventType: "downloadFolderImported", DownloadID: badHash, Date: t0, Data: map[string]string{"importedPath": "/a.mkv"}}}, false},
	}
	for _, c := range cases {
		if _, _, ok := importedBy(c.hist, badHash); ok != c.ok {
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
