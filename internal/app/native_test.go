package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/seerr"
)

// fakeQBit serves one torrent whose file progress the test controls.
type fakeQBit struct {
	mu       sync.Mutex
	torrent  qbit.Torrent
	files    []qbit.File
	prio     map[string]string // file ids -> priority
	sequenal bool
}

func (f *fakeQBit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = r.ParseForm()
	switch r.URL.Path {
	case "/api/v2/auth/login":
		_, _ = w.Write([]byte("Ok."))
	case "/api/v2/torrents/info":
		_ = json.NewEncoder(w).Encode([]qbit.Torrent{f.torrent})
	case "/api/v2/torrents/files":
		_ = json.NewEncoder(w).Encode(f.files)
	case "/api/v2/torrents/filePrio":
		f.prio[r.Form.Get("id")] = r.Form.Get("priority")
	case "/api/v2/torrents/toggleSequentialDownload":
		f.sequenal = !f.sequenal
	}
}

func TestNativeImportsSeasonPackEpisodeByEpisode(t *testing.T) {
	dir := t.TempDir()
	dl := filepath.Join(dir, "downloads") // what roarr sees; qBittorrent calls it /downloads
	pack := "The.Show.S01.1080p.WEB-DL-GRP"
	for name, body := range map[string]string{
		"The.Show.S01E01.1080p.WEB-DL-GRP.mkv":    "ep1",
		"The.Show.S01E01.1080p.WEB-DL-GRP.en.srt": "sub1",
		"The.Show.S01E02.1080p.WEB-DL-GRP.mkv":    "ep2",
		"The.Show.S01E02.1080p.WEB-DL-GRP.en.srt": "sub2",
		"Sample/sample.mkv":                       "s",
	} {
		p := filepath.Join(dl, pack, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fq := &fakeQBit{prio: map[string]string{},
		torrent: qbit.Torrent{Hash: "abc", Name: pack, State: "downloading", Progress: 0.5, SavePath: "/downloads/"},
		files: []qbit.File{
			{Index: 0, Name: pack + "/The.Show.S01E01.1080p.WEB-DL-GRP.mkv", Size: 3, Progress: 1, Priority: 1},
			{Index: 1, Name: pack + "/The.Show.S01E01.1080p.WEB-DL-GRP.en.srt", Size: 4, Progress: 1, Priority: 1},
			{Index: 2, Name: pack + "/The.Show.S01E02.1080p.WEB-DL-GRP.mkv", Size: 3, Progress: 0.2, Priority: 1},
			{Index: 3, Name: pack + "/Sample/sample.mkv", Size: 1, Progress: 0, Priority: 1},
			{Index: 4, Name: pack + "/The.Show.S01E02.1080p.WEB-DL-GRP.en.srt", Size: 4, Progress: 0, Priority: 1},
		}}
	srv := httptest.NewServer(fq)
	defer srv.Close()

	store, err := config.Open(filepath.Join(dir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(s *config.Settings) {
		s.Import = config.Import{Mode: "native", TVRoot: filepath.Join(dir, "tv"), PathMap: "/downloads=" + dl}
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{Settings: store, DataDir: filepath.Join(dir, "config"), details: map[string]cachedDetail{}, Hooks: &importer.Hooks{}}
	a.clients = &Clients{QBit: qbit.New(srv.URL, "admin", "x")}
	a.Native = newNative(a, filepath.Join(dir, "config"))
	if a.Backend().Name() != "native" {
		t.Fatal("IMPORT_MODE=native did not select the native backend")
	}
	var events []importer.Event
	a.Hooks.AfterImport(func(_ context.Context, e importer.Event) { events = append(events, e) })

	title := importer.Title{Kind: "tv", Title: "The Show", Year: 2021, TmdbID: 7, Episodes: []importer.Episode{{Season: 1, Number: 1}, {Season: 1, Number: 2}}}
	if err := a.Native.Store().PutTitle(title); err != nil {
		t.Fatal(err)
	}
	a.Native.Track(NativeJob{Hash: "ABC", Release: pack, Kind: "tv", TitleKey: title.Key(), TitleName: title.Title, Watch: true, Wanted: [][2]int{{1, 1}, {1, 2}}})

	ctx := context.Background()
	a.Native.tick(ctx)
	season := filepath.Join(dir, "tv", "The Show (2021)", "Season 01")
	if b, err := os.ReadFile(filepath.Join(season, "The Show - S01E01.mkv")); err != nil || string(b) != "ep1" {
		t.Fatalf("episode 1 not imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(season, "The Show - S01E01.en.srt")); err != nil {
		t.Fatalf("subtitle not imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(season, "The Show - S01E02.mkv")); err == nil {
		t.Fatal("episode 2 imported before it finished")
	}
	fq.mu.Lock()
	if fq.prio["3"] != "0" {
		t.Errorf("sample not skipped: priorities %v", fq.prio)
	}
	if !fq.sequenal {
		t.Error("watch-first pack not set to sequential")
	}
	// Episode 2's video finishes before its subtitle.
	fq.files[2].Progress = 1
	fq.files[4].Progress = 0.5
	fq.mu.Unlock()

	a.Native.tick(ctx)
	if _, err := os.Stat(filepath.Join(season, "The Show - S01E02.mkv")); err != nil {
		t.Fatalf("episode 2 not imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(season, "The Show - S01E02.en.srt")); err == nil {
		t.Fatal("unfinished subtitle imported")
	}
	if st := a.Native.Jobs()[0].State; st == "done" {
		t.Fatal("job done while a subtitle is still downloading")
	}

	fq.mu.Lock()
	fq.files[4].Progress = 1
	fq.torrent.Progress, fq.torrent.State = 1, "uploading"
	fq.mu.Unlock()
	a.Native.tick(ctx)
	if b, err := os.ReadFile(filepath.Join(season, "The Show - S01E02.en.srt")); err != nil || string(b) != "sub2" {
		t.Fatalf("late subtitle not imported: %v", err)
	}
	jobs := a.Native.Jobs()
	if len(jobs) != 1 || jobs[0].State != "done" {
		t.Fatalf("job %+v", jobs)
	}
	if len(events) != 2 || !strings.HasSuffix(events[1].Path, "S01E02.mkv") || events[1].Hash != "abc" {
		t.Fatalf("hook events %+v", events)
	}
	// Restart: jobs and the library store come back from /config.
	n2 := newNative(a, filepath.Join(dir, "config"))
	if len(n2.Jobs()) != 1 || !n2.Store().HasEpisode(title.Key(), 1, 2) {
		t.Fatal("state not persisted")
	}
	// Synthetic show pages count natively imported episodes as on disk.
	d := &SeriesDetail{LibraryItem: LibraryItem{TmdbID: 7}, Seasons: []SeasonDetail{{Number: 1, Episodes: []EpisodeDetail{{Season: 1, Number: 1}, {Season: 1, Number: 3}}}}}
	a.Native.markOnDisk(d)
	if !d.Seasons[0].Episodes[0].HasFile || d.Seasons[0].Episodes[1].HasFile {
		t.Fatalf("markOnDisk %+v", d.Seasons[0].Episodes)
	}
}

func TestNativeConcurrentSavesKeepEveryJob(t *testing.T) {
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Settings: store, DataDir: dir, details: map[string]cachedDetail{}, Hooks: &importer.Hooks{}}
	n := newNative(a, dir)
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
			n.Track(NativeJob{Hash: fmt.Sprintf("h%02d", i), Kind: "movie"})
		}(i)
	}
	wg.Wait()
	if got := len(newNative(a, dir).Jobs()); got != 40 {
		t.Fatalf("native.json holds %d jobs, want 40", got)
	}
}

func TestSeerrEpisodesAllOrNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/season/1") {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"episodes":[{"episodeNumber":1,"seasonNumber":2},{"episodeNumber":2,"seasonNumber":2}]}`))
	}))
	defer srv.Close()
	eps, err := seerrEpisodes(context.Background(), seerr.New(srv.URL, "k"), 7, []int{1, 2})
	if err == nil || eps != nil {
		t.Fatalf("a failed season must fail the lookup: %v %v", eps, err)
	}
	eps, err = seerrEpisodes(context.Background(), seerr.New(srv.URL, "k"), 7, []int{2})
	if err != nil || len(eps) != 2 {
		t.Fatalf("complete lookup: %v %v", eps, err)
	}
}
