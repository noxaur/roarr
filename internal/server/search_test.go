package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/app"
	"github.com/noxaur/roarr/internal/config"
)

// The search stream must be open before roarr has heard from Radarr, and a
// failed lookup must arrive as the stream's error event.
func TestSearchOpensStreamBeforeUpstreamAnswers(t *testing.T) {
	for _, k := range []string{"SONARR_URL", "RADARR_URL", "RADARR_API_KEY", "PROWLARR_URL", "SEERR_URL", "JELLYFIN_URL", "QBIT_URL", "QBIT_PASSWORD", "AUTH_MODE", "AUTH_PASSWORD", "IMPORT_MODE", "TRANSCODE"} {
		t.Setenv(k, "")
	}
	release := make(chan struct{})
	radarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/movie/lookup" {
			<-release
		}
		_, _ = io.WriteString(w, "[]")
	}))
	defer radarr.Close()
	defer close(release)

	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(s *config.Settings) { s.Radarr = config.Service{URL: radarr.URL, APIKey: "k"} }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Server{app: app.New(ctx, store, t.TempDir())}
	roarr := httptest.NewServer(http.HandlerFunc(s.search))
	defer roarr.Close()

	type answer struct {
		res *http.Response
		err error
	}
	got := make(chan answer, 1)
	go func() {
		res, err := http.Get(roarr.URL + "/api/search?kind=movie&tmdb=603")
		got <- answer{res, err}
	}()
	var res *http.Response
	select {
	case a := <-got:
		if a.err != nil {
			t.Fatal(a.err)
		}
		res = a.res
	case <-time.After(3 * time.Second):
		t.Fatal("no response headers while Radarr is still answering")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream = %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}

	release <- struct{}{}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if want := `data: {"type":"error","error":"radarr found nothing for tmdb:603"}` + "\n\n"; !strings.HasSuffix(string(body), want) {
		t.Fatalf("stream body = %q, want it to end with %q", body, want)
	}
}
