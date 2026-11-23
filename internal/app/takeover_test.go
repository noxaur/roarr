package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/importer"
)

// fakeArr answers Sonarr's and Radarr's library calls from fixed JSON.
func fakeArr(t *testing.T, routes map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestTakeoverAdoptsSonarrAndRadarr(t *testing.T) {
	a, _, dir := nativeLibraryApp(t)
	ctx := context.Background()
	tv, movies := filepath.Join(dir, "tv"), filepath.Join(dir, "movies")
	showDir := filepath.Join(tv, "The Show (2019) [tvdbid-11]")
	ep1 := filepath.Join(showDir, "Season 1", "The Show - S01E01 - Pilot WEBDL-1080p.mkv")
	writeLib(t, ep1)
	film := filepath.Join(movies, "Film (2001)", "Film (2001) Bluray-1080p.mkv")
	writeLib(t, film)

	series := []map[string]any{{"id": 3, "title": "The Show", "year": 2019, "tvdbId": 11, "tmdbId": 22, "monitored": true,
		"path": "/tv/The Show (2019) [tvdbid-11]", "seriesType": "standard"}}
	episodes := []map[string]any{
		{"id": 1, "seasonNumber": 1, "episodeNumber": 1, "title": "Pilot", "airDateUtc": "2019-01-01T02:00:00Z", "monitored": true, "hasFile": true, "episodeFileId": 9},
		{"id": 2, "seasonNumber": 1, "episodeNumber": 2, "title": "Two", "airDateUtc": "2019-01-08T02:00:00Z", "monitored": false},
	}
	epFiles := []map[string]any{{"id": 9, "relativePath": "Season 1/The Show - S01E01 - Pilot WEBDL-1080p.mkv",
		"path": "/tv/The Show (2019) [tvdbid-11]/Season 1/The Show - S01E01 - Pilot WEBDL-1080p.mkv", "size": 3,
		"sceneName": "The.Show.S01E01.1080p.WEB-DL.DDP5.1.H.264-GRP", "quality": map[string]any{"quality": map[string]any{"name": "WEBDL-1080p", "resolution": 1080}}}}
	js := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	son := fakeArr(t, map[string]string{
		"/api/v3/series": js(series), "/api/v3/episode": js(episodes), "/api/v3/episodefile": js(epFiles),
		"/api/v3/rootfolder": `[{"id":1,"path":"/tv"}]`,
	})
	rad := fakeArr(t, map[string]string{
		"/api/v3/rootfolder": `[{"id":1,"path":"/movies"}]`,
		"/api/v3/movie": js([]map[string]any{{"id": 5, "title": "Film", "year": 2001, "tmdbId": 77, "monitored": false, "path": "/movies/Film (2001)",
			"movieFile": map[string]any{"id": 1, "relativePath": "Film (2001) Bluray-1080p.mkv", "size": 3,
				"quality": map[string]any{"quality": map[string]any{"name": "Bluray-1080p", "resolution": 1080}}}}}),
	})
	a.clients.Sonarr, a.clients.Radarr = arr.NewSonarr(son, "k"), arr.NewRadarr(rad, "k")

	if err := a.runTakeover(ctx); err != nil {
		t.Fatal(err)
	}
	if st := a.Settings.Get().Import; !st.OwnsLibrary() {
		t.Fatalf("roarr does not run the library after the takeover: %+v", st)
	}
	n := a.Native
	show, ok := n.Store().Title(importer.Title{Kind: "tv", TmdbID: 22}.Key())
	if !ok || show.Folder != "The Show (2019) [tvdbid-11]" || show.TvdbID != 11 || len(show.Episodes) != 2 || show.Episodes[0].AirDate == "" {
		t.Fatalf("show %+v", show)
	}
	if show.Unmonitored || len(show.Monitor) != 1 || show.Monitor[0] != 10001 || len(show.Ignore) != 1 {
		t.Fatalf("monitoring not taken over: %+v", show)
	}
	rec, ok := n.Store().Record(ep1)
	if !ok || rec.Season != 1 || len(rec.Episodes) != 1 || rec.Rank != importer.QualityRank("The.Show.S01E01.1080p.WEB-DL") {
		t.Fatalf("episode file record %+v %v", rec, ok)
	}
	// New episodes go into the folder and season folder Sonarr made.
	dest, err := n.Importer().Destination(show, importer.Placement{Season: 1, Episodes: []int{2}, Video: importer.SourceFile{Rel: "x.mkv"}})
	if err != nil || dest != filepath.Join(showDir, "Season 1", "The Show - S01E02.mkv") {
		t.Fatalf("destination %s %v", dest, err)
	}
	movie, ok := n.Store().Title(importer.Title{Kind: "movie", TmdbID: 77}.Key())
	if !ok || !movie.Unmonitored || movie.Folder != "" {
		t.Fatalf("movie %+v", movie)
	}
	if f, ok := n.movieFile(movie); !ok || f.Path != film || f.Record == nil {
		t.Fatalf("movie file %+v", f)
	}
	// Running it again changes nothing.
	if err := a.runTakeover(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(n.Store().Files("")); got != 2 {
		t.Fatalf("%d file records after a second takeover", got)
	}
}

func TestTakeoverSkipsFoldersItCannotSee(t *testing.T) {
	a, _, _ := nativeLibraryApp(t)
	son := fakeArr(t, map[string]string{
		"/api/v3/series":      `[{"id":3,"title":"Elsewhere","year":2019,"tvdbId":11,"tmdbId":22,"path":"/other/Elsewhere (2019)"}]`,
		"/api/v3/episode":     `[{"id":1,"seasonNumber":1,"episodeNumber":1,"episodeFileId":9}]`,
		"/api/v3/episodefile": `[{"id":9,"relativePath":"Season 1/x.mkv"}]`,
		"/api/v3/rootfolder":  `[{"id":1,"path":"/other"}]`,
	})
	a.clients.Sonarr = arr.NewSonarr(son, "k")
	if err := a.runTakeover(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := a.Native.takeoverState()
	if st.Shows != 0 || len(st.Skipped) != 1 || !strings.Contains(st.Skipped[0], "cannot see") {
		t.Fatalf("%+v", st)
	}
}

func TestStandaloneDropsSonarrAndRadarr(t *testing.T) {
	a, _, _ := nativeLibraryApp(t)
	s, _ := a.Settings.Update(func(s *config.Settings) { s.Sonarr = config.Service{URL: "http://sonarr:8989", APIKey: "k"} })
	a.rebuild(s)
	if a.C().Sonarr == nil {
		t.Fatal("native mode without standalone stopped using Sonarr")
	}
	s, _ = a.Settings.Update(func(s *config.Settings) { s.Import.Standalone = true })
	a.rebuild(s)
	if a.C().Sonarr != nil || !a.nativeLib("tv") {
		t.Fatal("standalone still uses Sonarr")
	}
	if son, _ := a.arrSources(); son == nil {
		t.Fatal("a takeover can no longer read the configured Sonarr")
	}
}
