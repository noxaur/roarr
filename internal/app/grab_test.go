package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
)

// fakeRadarr declines every push unless the movie sits on the bypass profile.
type fakeRadarr struct {
	mu       sync.Mutex
	profile  int
	created  map[string]any
	switches []int
	// alwaysReject declines every push (blocklist, file on disk, ...).
	alwaysReject bool
	grabbed      map[string]any
}

func (f *fakeRadarr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v3/movie/5":
		send(map[string]any{"id": 5, "qualityProfileId": f.profile})
	case "PUT /api/v3/movie/editor":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if p, ok := body["qualityProfileId"].(float64); ok {
			f.profile = int(p)
			f.switches = append(f.switches, int(p))
		}
		send(map[string]any{})
	case "GET /api/v3/indexer":
		send([]any{})
	case "GET /api/v3/qualityprofile":
		ps := []any{map[string]any{"id": 1, "name": "HD-1080p"}}
		if f.created != nil {
			ps = append(ps, map[string]any{"id": 99, "name": arr.BypassProfileName})
		}
		send(ps)
	case "GET /api/v3/qualityprofile/schema":
		w.Write([]byte(`{"id":0,"name":"","upgradeAllowed":false,"cutoff":0,"minFormatScore":0,"cutoffFormatScore":0,
			"language":{"id":1,"name":"English"},
			"items":[{"quality":{"id":1,"name":"SDTV"},"items":[],"allowed":false},
			         {"id":1001,"name":"WEB 1080p","items":[{"quality":{"id":3,"name":"WEBDL-1080p"},"items":[],"allowed":false}],"allowed":false},
			         {"quality":{"id":31,"name":"Remux-2160p"},"items":[],"allowed":false}],
			"formatItems":[{"format":7,"name":"x265","score":-10000}]}`))
	case "POST /api/v3/qualityprofile":
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		send(map[string]any{"id": 99})
	case "POST /api/v3/release/push":
		if f.profile == 99 && !f.alwaysReject {
			send([]any{map[string]any{"approved": true}})
		} else {
			send([]any{map[string]any{"approved": false, "rejected": true, "rejections": []string{"x265 is not wanted in profile"}}})
		}
	case "GET /api/v3/release":
		if r.URL.Query().Get("movieId") != "5" {
			http.Error(w, "bad query", 400)
			return
		}
		send([]any{
			map[string]any{"guid": "other", "indexerId": 3, "title": "Movie 2024 1080p", "rejected": true},
			map[string]any{"guid": "abc", "indexerId": 4, "title": "Movie 2024 2160p x265", "size": 1000, "rejected": true, "rejections": []string{"Release is blocklisted"}},
		})
	case "POST /api/v3/release":
		_ = json.NewDecoder(r.Body).Decode(&f.grabbed)
		send(f.grabbed)
	case "GET /api/v3/history":
		send(map[string]any{"records": []any{map[string]any{"sourceTitle": "Movie.2024.2160p.x265", "eventType": "grabbed", "date": time.Now().UTC()}}})
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 404)
	}
}

func testApp(t *testing.T, c *Clients) *App {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &App{Settings: store, clients: c, details: map[string]cachedDetail{}}
}

func TestGrabMovieBypassesQualityProfile(t *testing.T) {
	f := &fakeRadarr{profile: 1}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a := testApp(t, &Clients{Radarr: arr.NewRadarr(srv.URL, "k")})
	g := GrabRequest{Kind: "movie", MovieID: 5, TmdbID: 42, Release: Release{Title: "Movie.2024.2160p.x265", DownloadURL: "http://prowlarr/1/download"}}

	res, err := a.grabMovie(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !res.CanBypass {
		t.Fatalf("plain grab: ok=%v canBypass=%v, want declined and bypassable", res.OK, res.CanBypass)
	}

	g.Bypass = true
	res, err = a.grabMovie(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !strings.Contains(res.Message, "bypassed") {
		t.Fatalf("bypass grab = %+v", res)
	}
	if f.profile != 1 {
		t.Fatalf("profile after grab = %d, want restored to 1 (switches %v)", f.profile, f.switches)
	}
	if f.created == nil || f.created["name"] != arr.BypassProfileName || f.created["cutoff"] != float64(31) {
		t.Fatalf("created profile = %v", f.created)
	}
	if lang := f.created["language"].(map[string]any); lang["id"] != float64(-1) {
		t.Fatalf("language = %v, want Any", lang)
	}
	group := f.created["items"].([]any)[1].(map[string]any)
	if group["allowed"] != true || group["items"].([]any)[0].(map[string]any)["allowed"] != true {
		t.Fatalf("group not fully allowed: %v", group)
	}
	if fi := f.created["formatItems"].([]any)[0].(map[string]any); fi["score"] != float64(0) {
		t.Fatalf("format score = %v, want 0", fi["score"])
	}
}

func TestGrabMovieBypassSetting(t *testing.T) {
	f := &fakeRadarr{profile: 1}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a := testApp(t, &Clients{Radarr: arr.NewRadarr(srv.URL, "k")})
	if _, err := a.Settings.Update(func(s *config.Settings) { s.Behaviour.BypassArrQuality = true }); err != nil {
		t.Fatal(err)
	}
	res, err := a.grabMovie(context.Background(), GrabRequest{Kind: "movie", MovieID: 5, Release: Release{Title: "Movie.2024.2160p.x265"}})
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if f.profile != 1 {
		t.Fatalf("profile = %d, want 1", f.profile)
	}
}

// Rejections a profile cannot fix are skipped the way Radarr's own "grab
// anyway" does: interactive search, then grab from the release cache.
func TestGrabMovieSkipsAllChecks(t *testing.T) {
	f := &fakeRadarr{profile: 1, alwaysReject: true}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a := testApp(t, &Clients{Radarr: arr.NewRadarr(srv.URL, "k")})
	res, err := a.grabMovie(context.Background(), GrabRequest{Kind: "movie", MovieID: 5, Bypass: true, Release: Release{Title: "Movie.2024.2160p.x265", Size: 1001}})
	if err != nil || !res.OK || !strings.Contains(res.Message, "all Radarr checks skipped") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if f.grabbed["guid"] != "abc" || f.grabbed["indexerId"] != float64(4) {
		t.Fatalf("grabbed %v, want guid abc on indexer 4", f.grabbed)
	}
	if f.profile != 1 {
		t.Fatalf("profile = %d, want 1", f.profile)
	}
}

func TestSwapOrigin(t *testing.T) {
	link := "http://localhost:9696/1/download?apikey=k&link=abc"
	cases := []struct{ from, to, want string }{
		{"http://localhost:9696", "http://prowlarr:9696/1/", "http://prowlarr:9696/1/download?apikey=k&link=abc"},
		{"http://localhost:9696", "prowlarr:9696", "http://prowlarr:9696/1/download?apikey=k&link=abc"},
		{"http://other:9696", "http://prowlarr:9696", link}, // not a Prowlarr link
		{"http://localhost:9696", "", link},
	}
	for _, c := range cases {
		if got := swapOrigin(link, c.from, c.to); got != c.want {
			t.Errorf("swapOrigin(%q, %q) = %q, want %q", c.from, c.to, got, c.want)
		}
	}
	if got := swapOrigin("magnet:?xt=urn:btih:abc", "http://localhost:9696", "http://prowlarr:9696"); got != "magnet:?xt=urn:btih:abc" {
		t.Errorf("magnet changed: %q", got)
	}
}

func TestSonarrSearchFor(t *testing.T) {
	d := testShow() // episode ids 1..30, ten per season
	q, _ := sonarrSearchFor(d, []int{25}, nil)
	if q.Get("episodeId") != "25" {
		t.Fatalf("single episode: %v", q)
	}
	q, extra := sonarrSearchFor(d, []int{21, 22, 23}, nil)
	if q.Get("seasonNumber") != "3" || q.Get("seriesId") != "2" || extra["seriesId"] != 2 {
		t.Fatalf("season pack: %v %v", q, extra)
	}
	q, _ = sonarrSearchFor(d, []int{15, 25}, nil)
	if q.Get("seasonNumber") != "2" {
		t.Fatalf("multi-season: %v", q)
	}
	if q, _ := sonarrSearchFor(d, nil, nil); q != nil {
		t.Fatalf("nothing to search: %v", q)
	}
}
