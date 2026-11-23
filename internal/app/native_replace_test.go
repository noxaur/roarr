package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/search"
)

// swarmQBit keeps torrents by hash: adds create them in state, deletes
// remove them.
type swarmQBit struct {
	mu       sync.Mutex
	torrents map[string]qbit.Torrent
	files    map[string][]qbit.File
	state    string // state of added torrents
	deleted  []string
}

func (f *swarmQBit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		_ = r.ParseMultipartForm(1 << 20)
	} else {
		_ = r.ParseForm()
	}
	switch r.URL.Path {
	case "/api/v2/auth/login":
		_, _ = w.Write([]byte("Ok."))
	case "/api/v2/torrents/info":
		out := []qbit.Torrent{}
		for _, h := range strings.Split(r.Form.Get("hashes"), "|") {
			if t, ok := f.torrents[h]; ok {
				out = append(out, t)
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	case "/api/v2/torrents/files":
		_ = json.NewEncoder(w).Encode(f.files[r.Form.Get("hash")])
	case "/api/v2/torrents/add":
		h := magnetHash(r.FormValue("urls"))
		f.torrents[h] = qbit.Torrent{Hash: h, Name: h, State: f.state, SavePath: "/downloads/"}
		_, _ = w.Write([]byte("Ok."))
	case "/api/v2/torrents/delete":
		for _, h := range strings.Split(r.Form.Get("hashes"), "|") {
			delete(f.torrents, h)
			f.deleted = append(f.deleted, h+":"+r.Form.Get("deleteFiles"))
		}
	}
}

func (f *swarmQBit) has(hash string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.torrents[hash]
	return ok
}

// releasesProwlarr answers every search with the same releases.
type releasesProwlarr struct{ results []arr.SearchResult }

func (p releasesProwlarr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/indexer":
		caps := `{"tvSearchParams":["q","season","ep","tvdbId"],"movieSearchParams":["q","imdbId"],"categories":[{"id":5000},{"id":2000}]}`
		_, _ = w.Write([]byte(`[{"id":1,"name":"one","enable":true,"capabilities":` + caps + `}]`))
	case "/api/v1/indexerstatus":
		_, _ = w.Write([]byte(`[]`))
	case "/api/v1/search":
		_ = json.NewEncoder(w).Encode(p.results)
	default:
		http.NotFound(w, r)
	}
}

func ptr[T any](v T) *T { return &v }

const (
	deadHash = "1111111111111111111111111111111111111111"
	goodHash = "2222222222222222222222222222222222222222"
)

// replaceApp is a native-library app whose show has one missing episode,
// a qBittorrent and a Prowlarr that offers a dead and a good release.
func replaceApp(t *testing.T) (*App, *swarmQBit, importer.Title) {
	t.Helper()
	dir := t.TempDir()
	fq := &swarmQBit{torrents: map[string]qbit.Torrent{}, files: map[string][]qbit.File{}, state: "downloading"}
	qs := httptest.NewServer(fq)
	t.Cleanup(qs.Close)
	pr := httptest.NewServer(releasesProwlarr{results: []arr.SearchResult{
		{Title: "Show.S01E01.1080p.WEB-DL-DEAD", IndexerID: 1, Indexer: "one", Protocol: "torrent", Seeders: ptr(50), Size: 2 << 30, MagnetURL: "magnet:?xt=urn:btih:" + deadHash},
		{Title: "Show.S01E01.720p.WEB-DL-GOOD", IndexerID: 1, Indexer: "one", Protocol: "torrent", Seeders: ptr(10), Size: 1 << 30, MagnetURL: "magnet:?xt=urn:btih:" + goodHash},
	}})
	t.Cleanup(pr.Close)
	store, err := config.Open(filepath.Join(dir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(s *config.Settings) {
		s.Import = config.Import{Mode: "native", TVRoot: filepath.Join(dir, "tv")}
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{Settings: store, DataDir: filepath.Join(dir, "config"), details: map[string]cachedDetail{}, Hooks: &importer.Hooks{}}
	prowlarr := arr.NewProwlarr(pr.URL, "k")
	a.clients = &Clients{QBit: qbit.New(qs.URL, "admin", "x"), Prowlarr: prowlarr, Search: search.NewEngine(prowlarr)}
	a.Native = newNative(a, filepath.Join(dir, "config"))
	a.Library = newLibrary(a)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go a.Native.replaceLoop(ctx)
	show := importer.Title{Kind: "tv", Title: "Show", Year: 2020, TmdbID: 5, MetaAt: time.Now().UTC().Format(time.RFC3339),
		Episodes: []importer.Episode{{Season: 1, Number: 1, AirDate: "2020-01-01"}}}
	if err := a.Native.Store().PutTitle(show); err != nil {
		t.Fatal(err)
	}
	return a, fq, show
}

// A torrent that stops receiving data is blocklisted, removed with its
// data, and the next best release is grabbed for the same episode.
func TestStalledDownloadIsReplaced(t *testing.T) {
	a, fq, show := replaceApp(t)
	ctx := context.Background()
	n := a.Native

	res, err := a.Auto(ctx, AutoRequest{Kind: "tv", SeriesID: NativeID(show), EpisodeIDs: []int{10001}})
	if err != nil || len(res.Grabs) != 1 || res.Grabs[0].Release != "Show.S01E01.1080p.WEB-DL-DEAD" {
		t.Fatalf("first pick: %+v %v", res, err)
	}
	fq.mu.Lock()
	fq.torrents[deadHash] = qbit.Torrent{Hash: deadHash, State: "stalledDL", Progress: 0.1, SavePath: "/downloads/"}
	fq.files[deadHash] = []qbit.File{{Index: 0, Name: "Show.S01E01.1080p.WEB-DL-DEAD.mkv", Size: 2 << 30, Progress: 0.1}}
	fq.mu.Unlock()
	n.tick(ctx) // plans; data arrived just now
	n.set(n.jobs[deadHash], func() { n.jobs[deadHash].ProgressAt = time.Now().Add(-2 * stallAfter) })
	n.started = time.Now().Add(-2 * stallAfter)
	n.tick(ctx)
	for i := 0; i < 200 && !fq.has(goodHash); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !fq.has(goodHash) || fq.has(deadHash) {
		t.Fatalf("not replaced: qbit has %v, deleted %v", fq.torrents, fq.deleted)
	}
	if fq.deleted[0] != deadHash+":true" {
		t.Fatalf("dead torrent not removed with its data: %v", fq.deleted)
	}
	if _, ok := n.blocklist.Match("Show.S01E01.1080p.WEB-DL-DEAD", ""); !ok {
		t.Fatal("dead release not blocklisted")
	}
	var old NativeJob
	for i := 0; i < 100; i++ {
		n.mu.Lock()
		old = *n.jobs[deadHash]
		n.mu.Unlock()
		if old.State == "replaced" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if old.State != "replaced" || !strings.Contains(old.Replacement, "WEB-DL-GOOD") {
		t.Fatalf("old job %+v", old)
	}
	for _, it := range n.activity() {
		if it.DownloadID == deadHash {
			t.Fatal("a replaced download still shows in the activity feed")
		}
	}
	// The blocklist outlives a restart and keeps the release out of picks.
	n2 := newNative(a, a.DataDir)
	if _, ok := n2.blocklist.Match("show s01e01 1080p web dl dead", deadHash); !ok {
		t.Fatal("blocklist not saved")
	}
}

// A download whose verification fails stops at once, without waiting for
// the rest of the pack, and is replaced.
func TestBadReleaseFailsTheWholeDownload(t *testing.T) {
	a, fq, show := replaceApp(t)
	ctx := context.Background()
	n := a.Native
	a.Hooks.BeforeImport(func(context.Context, importer.Candidate) error {
		return importer.BadRelease("failed verification: the video is a slideshow")
	})
	if _, err := a.Auto(ctx, AutoRequest{Kind: "tv", SeriesID: NativeID(show), EpisodeIDs: []int{10001}}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeLib(t, filepath.Join(dir, "Show.S01E01.1080p.WEB-DL-DEAD.mkv"))
	_, _ = a.Settings.Update(func(s *config.Settings) { s.Import.PathMap = "/downloads=" + dir })
	fq.mu.Lock()
	fq.torrents[deadHash] = qbit.Torrent{Hash: deadHash, State: "uploading", Progress: 1, SavePath: "/downloads/"}
	fq.files[deadHash] = []qbit.File{{Index: 0, Name: "Show.S01E01.1080p.WEB-DL-DEAD.mkv", Size: 4, Progress: 1}}
	fq.mu.Unlock()
	n.tick(ctx)
	for i := 0; i < 200 && !fq.has(goodHash); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !fq.has(goodHash) {
		t.Fatal("not replaced after a failed verification")
	}
	e, ok := n.blocklist.Match("Show.S01E01.1080p.WEB-DL-DEAD", "")
	if !ok || !strings.Contains(e.Reason, "slideshow") {
		t.Fatalf("blocklist entry %+v", e)
	}
}

// An episode that aired yesterday is grabbed without anyone asking, and
// not searched again before it is due.
func TestNewEpisodeIsGrabbedAfterItAirs(t *testing.T) {
	a, fq, show := replaceApp(t)
	ctx := context.Background()
	n := a.Native
	missingPause = 0
	defer func() { missingPause = 5 * time.Second }()
	yesterday := time.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	_, _ = n.Store().UpdateTitle(show.Key(), func(x *importer.Title) {
		x.Added = time.Now().AddDate(0, 0, -30).Format(time.DateOnly) // tracked before it aired: monitored
		x.Episodes[0].AirDate = yesterday
	})
	last := map[string]time.Time{}
	n.searchAired(ctx, last, time.Now())
	if !fq.has(deadHash) {
		t.Fatal("the new episode was not grabbed")
	}
	// The download goes away; within the hour roarr does not search again.
	fq.mu.Lock()
	delete(fq.torrents, deadHash)
	fq.mu.Unlock()
	n.Remove(deadHash)
	n.searchAired(ctx, last, time.Now().Add(30*time.Minute))
	if fq.has(deadHash) {
		t.Fatal("searched again before it was due")
	}
	n.searchAired(ctx, last, time.Now().Add(61*time.Minute))
	if !fq.has(deadHash) {
		t.Fatal("not searched again once due")
	}
	// Two weeks after it aired, the periodic missing search takes over.
	if airedSearchEvery(15) != 0 || airedSearchEvery(0) != time.Hour {
		t.Fatal("schedule")
	}
}

func TestBlocklistMatchesByHashOrName(t *testing.T) {
	b := openBlocklist(t.TempDir())
	b.Add(Blocked{Release: "Show.S01E01.1080p.WEB-DL-GRP", Hash: "ABC", Reason: "dead"})
	if _, ok := b.Match("show s01e01 1080p web dl grp", ""); !ok {
		t.Fatal("same release by name not matched")
	}
	if _, ok := b.Match("Other.Name", "abc"); !ok {
		t.Fatal("same hash not matched")
	}
	if rel := b.annotate(Release{Title: "Show.S01E01.720p.WEB-DL-GRP"}); rel.Rejected {
		t.Fatal("another release rejected")
	}
	if err := b.Remove("abc"); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Match("Show.S01E01.1080p.WEB-DL-GRP", ""); ok {
		t.Fatal("still blocked after removal")
	}
}

// A file below the quality cutoff is upgraded, and only to a release at
// the cutoff; files at the cutoff or of unknown quality are left alone.
func TestUpgradeSearchGrabsOnlyCutoffReleases(t *testing.T) {
	a, fq, show := replaceApp(t)
	ctx := context.Background()
	n := a.Native
	_, _ = n.Store().UpdateTitle(show.Key(), func(x *importer.Title) { x.Monitor = []int{10001} })
	show, _ = n.Store().Title(show.Key())
	path := filepath.Join(n.Importer().TitleFolder(show), "Season 01", "Show - S01E01.mkv")
	writeLib(t, path)
	record := func(rel string) {
		_ = n.Store().Put(importer.FileRecord{TitleKey: show.Key(), Season: 1, Episodes: []int{1}, Path: path, Release: rel})
	}

	for _, rel := range []string{"Show.S01E01.1080p.WEB-DL-GRP", ""} {
		record(rel)
		if _, count := a.upgradeRequest(show); count != 0 {
			t.Fatalf("%q: a file at the cutoff, or of unknown quality, is searched for upgrades", rel)
		}
	}
	record("Show.S01E01.720p.HDTV-OLD")
	req, count := a.upgradeRequest(show)
	if count != 1 || !req.Upgrade || req.EpisodeIDs[0] != 10001 {
		t.Fatalf("%+v %d", req, count)
	}
	res, err := a.Auto(ctx, req)
	if err != nil || len(res.Grabs) != 1 || res.Grabs[0].Release != "Show.S01E01.1080p.WEB-DL-DEAD" || !fq.has(deadHash) {
		t.Fatalf("upgrade grab: %+v %v", res, err)
	}
	// The 720p WEB-DL is a better file than the HDTV one, but not an upgrade
	// to the cutoff.
	rels := a.cutoff(req, []Release{{Title: "x", Info: release.Parse("Show.S01E01.720p.WEB-DL-GOOD")}})
	if !rels[0].Rejected {
		t.Fatal("a release below the cutoff was not rejected")
	}
}

// A failed download whose title is being searched stays replaceable: it is
// neither blocklisted nor removed until its own search can run.
func TestReplaceWaitsForARunningSearch(t *testing.T) {
	a, fq, show := replaceApp(t)
	n := a.Native
	fq.mu.Lock()
	fq.torrents[deadHash] = qbit.Torrent{Hash: deadHash}
	fq.mu.Unlock()
	n.Track(NativeJob{Hash: deadHash, Release: "Show.S01E01.1080p.WEB-DL-DEAD", Kind: "tv", TitleKey: show.Key(), TitleName: show.Title, Wanted: [][2]int{{1, 1}}})
	n.failRelease(n.jobs[deadHash], "stalled")
	n.claimSearch(show.Key())
	if _, err := n.Replace(context.Background(), deadHash, "", false); err == nil {
		t.Fatal("replaced while another search held the title")
	}
	n.mu.Lock()
	j := *n.jobs[deadHash]
	n.mu.Unlock()
	if j.Replacement != "" || !fq.has(deadHash) {
		t.Fatalf("the job is no longer replaceable: %+v", j)
	}
	if _, ok := n.blocklist.Match(j.Release, ""); ok {
		t.Fatal("blocklisted before it could be replaced")
	}
	// Once the search is done, a later tick replaces it.
	n.releaseSearch(show.Key())
	n.tick(context.Background())
	for i := 0; i < 200 && fq.has(deadHash); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if fq.has(deadHash) {
		t.Fatal("a job that already failed was never offered again")
	}
}

// Removing a title with its torrents also removes torrents of jobs that
// were forgotten after they finished.
func TestRemoveTitleRemovesForgottenTorrents(t *testing.T) {
	a, fq, show := replaceApp(t)
	fq.mu.Lock()
	fq.torrents[goodHash] = qbit.Torrent{Hash: goodHash}
	fq.mu.Unlock()
	_ = a.Native.Store().Put(importer.FileRecord{TitleKey: show.Key(), Season: 1, Episodes: []int{1}, Path: "/nowhere/Show - S01E01.mkv", Hash: goodHash})
	if err := a.nativeRemoveTitle(context.Background(), "tv", NativeID(show), false, true); err != nil {
		t.Fatal(err)
	}
	if fq.has(goodHash) {
		t.Fatal("the finished torrent still seeds")
	}
}
