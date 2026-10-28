package app

import (
	"archive/zip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/qbit"
)

// deleteQBit records torrent deletions.
type deleteQBit struct {
	mu      sync.Mutex
	deleted []string
}

func (f *deleteQBit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch r.URL.Path {
	case "/api/v2/auth/login":
		_, _ = w.Write([]byte("Ok."))
	case "/api/v2/torrents/delete":
		f.mu.Lock()
		f.deleted = append(f.deleted, r.Form.Get("hashes")+":"+r.Form.Get("deleteFiles"))
		f.mu.Unlock()
	}
}

func (f *deleteQBit) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

func nativeLibraryApp(t *testing.T) (*App, *deleteQBit, string) {
	t.Helper()
	dir := t.TempDir()
	fq := &deleteQBit{}
	srv := httptest.NewServer(fq)
	t.Cleanup(srv.Close)
	store, err := config.Open(filepath.Join(dir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(s *config.Settings) {
		s.Import = config.Import{Mode: "native", TVRoot: filepath.Join(dir, "tv"), MovieRoot: filepath.Join(dir, "movies")}
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{Settings: store, DataDir: filepath.Join(dir, "config"), details: map[string]cachedDetail{}, Hooks: &importer.Hooks{}}
	a.clients = &Clients{QBit: qbit.New(srv.URL, "admin", "x")}
	a.Native = newNative(a, filepath.Join(dir, "config"))
	a.Library = newLibrary(a)
	return a, fq, dir
}

func writeLib(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func episode(d *SeriesDetail, season, number int) EpisodeDetail {
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			if e.Season == season && e.Number == number {
				return e
			}
		}
	}
	return EpisodeDetail{}
}

func TestNativeLibraryShowFlow(t *testing.T) {
	a, fq, dir := nativeLibraryApp(t)
	ctx := context.Background()
	n := a.Native
	show := importer.Title{Kind: "tv", Title: "The Show", Year: 2021, TmdbID: 7, TvdbID: 70, Poster: "/p.jpg", MetaAt: time.Now().UTC().Format(time.RFC3339),
		Episodes: []importer.Episode{
			{Season: 1, Number: 1, AirDate: "2020-01-01"},
			{Season: 1, Number: 2, AirDate: "2020-01-08"},
			{Season: 1, Number: 3, AirDate: "2999-01-01"}, // not aired yet
			{Season: 2, Number: 1, AirDate: "2020-06-01"}, // aired after roarr started to track the show
		}}
	if err := n.Store().PutTitle(show); err != nil {
		t.Fatal(err)
	}
	_, _ = n.Store().UpdateTitle(show.Key(), func(x *importer.Title) { x.Added, x.Monitor = "2020-01-05", []int{10001} })
	season := filepath.Join(dir, "tv", "The Show (2021)", "Season 01")
	e1, e1sub, e2 := filepath.Join(season, "The Show - S01E01.mkv"), filepath.Join(season, "The Show - S01E01.en.srt"), filepath.Join(season, "The Show - S01E02.mkv")
	for _, p := range []string{e1, e1sub, e2} {
		writeLib(t, p)
	}
	_ = n.Store().Put(importer.FileRecord{TitleKey: show.Key(), Season: 1, Episodes: []int{1}, Path: e1, Subtitles: []string{e1sub}, Hash: "h1", Release: "The.Show.S01E01.1080p.WEB-DL-GRP"})
	_ = n.Store().Put(importer.FileRecord{TitleKey: show.Key(), Season: 1, Episodes: []int{2}, Path: e2, Hash: "h2"})

	// The library list holds the show under its TMDB id.
	a.Library.Load(ctx)
	it, ok := a.Library.Find("tv:7")
	if !ok || it.ID != 7 || it.Have != 2 || it.Total != 3 || !it.Monitored || it.Poster != "/p.jpg" {
		t.Fatalf("library item %+v (found %v)", it, ok)
	}
	if got, ok := a.Library.FindTvdb(70); !ok || got.ID != 7 {
		t.Fatal("the show is not found by its TVDB id")
	}

	d, err := a.Series(ctx, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if !d.InLibrary || d.Synthetic || d.Path != filepath.Join(dir, "tv", "The Show (2021)") {
		t.Fatalf("detail %+v", d)
	}
	if e := episode(d, 1, 1); e.ID != 10001 || !e.HasFile || !e.Monitored || e.File == nil || e.File.Quality != "WEB-DL-1080p" {
		t.Fatalf("S01E01 %+v %+v", e, e.File)
	}
	if e := episode(d, 1, 3); e.Aired || e.Monitored && e.Aired {
		t.Fatalf("S01E03 %+v", e)
	}
	if e := episode(d, 2, 1); !e.Monitored || !e.Aired || e.HasFile {
		t.Fatalf("S02E01 %+v", e)
	}
	req, count, err := a.missingRequest(ctx, show, false)
	if err != nil || count != 1 || !slices.Equal(req.EpisodeIDs, []int{20001}) || req.SeriesID != 7 {
		t.Fatalf("missing %+v %d %v", req, count, err)
	}

	// Delete S01E01 with its torrent; S01E02 came from another torrent.
	res, err := a.SeriesEpisodes(ctx, 7, EpisodeAction{Action: "delete", EpisodeIDs: []int{10001}, RemoveTorrent: true})
	if err != nil || res.Count != 1 {
		t.Fatalf("delete %+v %v", res, err)
	}
	if fileExists(e1) || fileExists(e1sub) || !fileExists(e2) {
		t.Fatal("wrong files deleted")
	}
	if got := fq.got(); !slices.Equal(got, []string{"h1:true"}) {
		t.Fatalf("torrents removed %v", got)
	}
	d, _ = a.Series(ctx, 7, false)
	if e := episode(d, 1, 1); e.HasFile {
		t.Fatal("deleted episode still on disk")
	}

	// Monitoring: S02E01 off, then the whole show off.
	if _, err := a.SeriesEpisodes(ctx, 7, EpisodeAction{Action: "unmonitor", EpisodeIDs: []int{20001}}); err != nil {
		t.Fatal(err)
	}
	if _, count, _ := a.missingRequest(ctx, show, false); count != 1 { // S01E01 is missing now
		t.Fatalf("missing after unmonitor: %d", count)
	}
	off := false
	if err := a.PatchSeries(ctx, 7, TitlePatch{Monitored: &off}); err != nil {
		t.Fatal(err)
	}
	if st, _ := n.Store().Title(show.Key()); !st.Unmonitored {
		t.Fatal("show still monitored")
	}
	if _, count, _ := a.missingRequest(ctx, show, false); count != 0 {
		t.Fatalf("an unmonitored show misses %d episodes", count)
	}

	// A record that points out of the library is never deleted.
	outside := filepath.Join(dir, "downloads", "The.Show.S01E02.mkv")
	writeLib(t, outside)
	_ = n.Store().Forget(e2)
	_ = os.Remove(e2)
	_ = n.Store().Put(importer.FileRecord{TitleKey: show.Key(), Season: 1, Episodes: []int{2}, Path: filepath.Join(dir, "tv", "..", "downloads", "The.Show.S01E02.mkv")})
	if _, err := a.SeriesEpisodes(ctx, 7, EpisodeAction{Action: "delete", EpisodeIDs: []int{10002}}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("escape not refused: %v", err)
	}
	if !fileExists(outside) {
		t.Fatal("a file outside the library was deleted")
	}

	// Removing the show stops its unfinished downloads.
	n.Track(NativeJob{Hash: "active", Kind: "tv", TitleKey: show.Key(), TitleName: show.Title})
	n.Track(NativeJob{Hash: "other", Kind: "tv", TitleKey: "tv:tmdb:99"})
	if err := a.RemoveTitle(ctx, "tv", 7, true, false); err != nil {
		t.Fatal(err)
	}
	if got := fq.got(); !slices.Contains(got, "active:true") || slices.Contains(got, "other:true") {
		t.Fatalf("torrents removed %v", got)
	}
	if len(n.Jobs()) != 1 {
		t.Fatalf("jobs %+v", n.Jobs())
	}
	if _, err := a.Series(ctx, 7, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed show: %v", err)
	}
	if _, ok := a.Library.Find("tv:7"); ok {
		t.Fatal("removed show still listed")
	}
	if !fileExists(outside) {
		t.Fatal("removing the show deleted a file outside the library")
	}
}

func TestNativeLibraryMovieFlow(t *testing.T) {
	a, fq, dir := nativeLibraryApp(t)
	ctx := context.Background()
	n := a.Native
	m := importer.Title{Kind: "movie", Title: "Film", Year: 2001, TmdbID: 9}
	_ = n.Store().PutTitle(m)
	p := filepath.Join(dir, "movies", "Film (2001)", "Film (2001).mkv")
	writeLib(t, p)
	_ = n.Store().Put(importer.FileRecord{TitleKey: m.Key(), Path: p, Hash: "m1", Size: 5})

	a.Library.Load(ctx)
	if it, ok := a.Library.Find("movie:9"); !ok || it.Have != 1 || it.Total != 1 {
		t.Fatalf("library item %+v", it)
	}
	d, err := a.Movie(ctx, 9)
	if err != nil || !d.HasFile || !d.InLibrary || d.File == nil || d.File.Path != p {
		t.Fatalf("movie %+v %v", d, err)
	}
	if _, count, _ := a.missingRequest(ctx, m, false); count != 0 {
		t.Fatal("a movie on disk counts as missing")
	}
	res, err := a.DeleteMovieFile(ctx, 9, true)
	if err != nil || res.Count != 1 || fileExists(p) {
		t.Fatalf("delete %+v %v", res, err)
	}
	if got := fq.got(); !slices.Equal(got, []string{"m1:true"}) {
		t.Fatalf("torrents removed %v", got)
	}
	if _, err := os.Stat(filepath.Dir(p)); err == nil {
		t.Fatal("empty movie folder stays")
	}
	req, count, _ := a.missingRequest(ctx, m, false)
	if count != 1 || !slices.Equal(req.MovieTmdb, []int{9}) {
		t.Fatalf("missing movie %+v", req)
	}
	off := false
	_ = a.PatchMovie(ctx, 9, TitlePatch{Monitored: &off})
	if _, count, _ := a.missingRequest(ctx, m, false); count != 0 {
		t.Fatal("an unmonitored movie is searched by the periodic search")
	}
	if _, count, _ := a.missingRequest(ctx, m, true); count != 1 {
		t.Fatal("a search you ask for must include an unmonitored movie")
	}
}

func TestNativeLibraryOnlyWithoutArr(t *testing.T) {
	a, _, _ := nativeLibraryApp(t)
	if !a.nativeLib("tv") || !a.nativeLib("movie") {
		t.Fatal("native mode without Sonarr/Radarr must use the native library")
	}
	_, _ = a.Settings.Update(func(s *config.Settings) { s.Import.Mode = "arr" })
	if a.nativeLib("tv") || a.nativeLib("movie") {
		t.Fatal("arr mode must not use the native library")
	}
}

func TestEpisodeMonitored(t *testing.T) {
	tl := importer.Title{Added: "2024-05-01", Monitor: []int{10001}, Ignore: []int{10010}}
	cases := []struct {
		s, e int
		air  string
		want bool
	}{
		{1, 1, "2020-01-01", true},  // grabbed
		{1, 2, "2020-01-08", false}, // aired before roarr tracked the show
		{1, 9, "2024-06-01", true},  // new episode
		{1, 10, "2024-06-08", false},
		{0, 1, "2024-06-08", false}, // specials only on purpose
		{1, 11, "", false},
	}
	for _, c := range cases {
		if got := episodeMonitored(tl, c.s, c.e, c.air); got != c.want {
			t.Errorf("S%02dE%02d (%s): %v, want %v", c.s, c.e, c.air, got, c.want)
		}
	}
	tl.Unmonitored = true
	if episodeMonitored(tl, 1, 1, "2020-01-01") {
		t.Error("an unmonitored title monitors nothing")
	}
}

func TestNativeUnpacksZipRelease(t *testing.T) {
	dir := t.TempDir()
	dl := filepath.Join(dir, "downloads")
	rel := "Film.2001.1080p.WEB-DL-GRP/Film.2001.1080p.WEB-DL-GRP.zip"
	arc := filepath.Join(dl, rel)
	_ = os.MkdirAll(filepath.Dir(arc), 0o755)
	f, _ := os.Create(arc)
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{"Film.2001.1080p.WEB-DL-GRP.mkv": "film", "Film.2001.1080p.WEB-DL-GRP.en.srt": "sub", "../evil.mkv": "x"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	_ = f.Close()
	fq := &fakeQBit{prio: map[string]string{},
		torrent: qbit.Torrent{Hash: "zzz", Name: "Film", State: "uploading", Progress: 1, SavePath: "/downloads/"},
		files:   []qbit.File{{Index: 0, Name: rel, Size: 100, Progress: 1, Priority: 1}, {Index: 1, Name: "Film.2001.1080p.WEB-DL-GRP/Film.nfo", Size: 1, Progress: 1, Priority: 1}}}
	srv := httptest.NewServer(fq)
	defer srv.Close()
	store, _ := config.Open(filepath.Join(dir, "config"))
	_, _ = store.Update(func(s *config.Settings) {
		s.Import = config.Import{Mode: "native", MovieRoot: filepath.Join(dir, "movies"), PathMap: "/downloads=" + dl}
	})
	a := &App{Settings: store, DataDir: filepath.Join(dir, "config"), details: map[string]cachedDetail{}, Hooks: &importer.Hooks{}}
	a.clients = &Clients{QBit: qbit.New(srv.URL, "admin", "x")}
	a.Native = newNative(a, filepath.Join(dir, "config"))
	title := importer.Title{Kind: "movie", Title: "Film", Year: 2001, TmdbID: 9}
	_ = a.Native.Store().PutTitle(title)
	a.Native.Track(NativeJob{Hash: "zzz", Release: "Film.2001.1080p.WEB-DL-GRP", Kind: "movie", TitleKey: title.Key(), TitleName: "Film"})

	a.Native.tick(context.Background())
	movie := filepath.Join(dir, "movies", "Film (2001)")
	if b, err := os.ReadFile(filepath.Join(movie, "Film (2001).mkv")); err != nil || string(b) != "film" {
		t.Fatalf("movie from the archive not imported: %v (jobs %+v)", err, a.Native.Jobs())
	}
	if _, err := os.Stat(filepath.Join(movie, "Film (2001).en.srt")); err != nil {
		t.Fatalf("subtitle from the archive not imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dl, "Film.2001.1080p.WEB-DL-GRP", "evil.mkv")); err == nil {
		t.Fatal("an archive entry left the unpack folder")
	}
	j := a.Native.Jobs()[0]
	if j.State != "done" || !j.Unpacked {
		t.Fatalf("job %+v", j)
	}
	unpacked := filepath.Join(dl, "Film.2001.1080p.WEB-DL-GRP", importer.UnpackDir("zzz"))
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(unpacked); err != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(unpacked); err == nil {
		t.Fatal("the unpack folder stays after the import")
	}
	fq.mu.Lock()
	defer fq.mu.Unlock()
	if fq.prio["1"] != "0" {
		t.Errorf("the nfo was not skipped: %v", fq.prio)
	}
}

func TestNativeRarReleaseFailsWithReason(t *testing.T) {
	dir := t.TempDir()
	fq := &fakeQBit{prio: map[string]string{},
		torrent: qbit.Torrent{Hash: "rrr", Name: "Film", State: "downloading", Progress: 0.1, SavePath: "/downloads/"},
		files:   []qbit.File{{Index: 0, Name: "Film/film.rar", Size: 100}, {Index: 1, Name: "Film/film.r00", Size: 100}}}
	srv := httptest.NewServer(fq)
	defer srv.Close()
	store, _ := config.Open(filepath.Join(dir, "config"))
	_, _ = store.Update(func(s *config.Settings) {
		s.Import = config.Import{Mode: "native", MovieRoot: filepath.Join(dir, "movies")}
	})
	a := &App{Settings: store, DataDir: filepath.Join(dir, "config"), details: map[string]cachedDetail{}, Hooks: &importer.Hooks{}}
	a.clients = &Clients{QBit: qbit.New(srv.URL, "admin", "x")}
	a.Native = newNative(a, filepath.Join(dir, "config"))
	title := importer.Title{Kind: "movie", Title: "Film", Year: 2001, TmdbID: 9}
	_ = a.Native.Store().PutTitle(title)
	a.Native.Track(NativeJob{Hash: "rrr", Release: "Film.2001.1080p", Kind: "movie", TitleKey: title.Key()})
	a.Native.tick(context.Background())
	j := a.Native.Jobs()[0]
	if j.State != "failed" || !strings.Contains(j.Message, "RAR") {
		t.Fatalf("job %+v", j)
	}
}

func TestSearchMissing(t *testing.T) {
	a, _, _ := nativeLibraryApp(t)
	ctx := context.Background()
	n := a.Native
	show := importer.Title{Kind: "tv", Title: "Show", TmdbID: 5, MetaAt: time.Now().UTC().Format(time.RFC3339),
		Episodes: []importer.Episode{{Season: 1, Number: 1, AirDate: "2020-01-01"}, {Season: 1, Number: 2, AirDate: "2020-01-08"}}}
	_ = n.Store().PutTitle(show)
	_, _ = n.Store().UpdateTitle(show.Key(), func(x *importer.Title) { x.Monitor = []int{10002} })
	res, err := a.TitleCommand(ctx, "tv", 5, "search")
	if err != nil || res.Count != 1 || !strings.Contains(res.Message, "1 missing episode") {
		t.Fatalf("%+v %v", res, err)
	}
	// An episode that is downloading is not missing.
	for i := 0; i < 100 && func() bool { n.searchMu.Lock(); defer n.searchMu.Unlock(); return n.searching[show.Key()] }(); i++ {
		time.Sleep(10 * time.Millisecond) // the search fails at once: no Prowlarr
	}
	n.Track(NativeJob{Hash: "q", Kind: "tv", TitleKey: show.Key(), Wanted: [][2]int{{1, 2}}})
	a.detailMu.Lock()
	a.details = map[string]cachedDetail{}
	a.detailMu.Unlock()
	if res, err := a.TitleCommand(ctx, "tv", 5, "search"); err != nil || res.Count != 0 {
		t.Fatalf("queued episode searched: %+v %v", res, err)
	}
	// The periodic pass skips titles that are not monitored and returns.
	missingPause = 0
	defer func() { missingPause = 5 * time.Second }()
	_, _ = n.Store().UpdateTitle(show.Key(), func(x *importer.Title) { x.Unmonitored = true })
	n.searchAllMissing(ctx)
}
