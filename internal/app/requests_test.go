package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/seerr"
)

func TestSeerrPassRetriesFailedAutoFetch(t *testing.T) {
	var lookups atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/request":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{
				map[string]any{"id": 11, "status": 2, "type": "movie", "createdAt": time.Now(), "media": map[string]any{"tmdbId": 603, "status": 3}},
			}})
		case "/api/v3/movie/lookup":
			// Radarr is down for the first pass, then knows the movie is on disk.
			if lookups.Add(1) == 1 {
				http.Error(w, "starting up", 503)
				return
			}
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"tmdbId": 603, "title": "The Matrix", "hasFile": true}})
		default:
			http.Error(w, "unexpected "+r.URL.Path, 404)
		}
	}))
	defer srv.Close()
	a := testApp(t, &Clients{Radarr: arr.NewRadarr(srv.URL, "k")})
	a.Library = newLibrary(a)
	s := seerr.New(srv.URL, "k")
	handled := map[int]time.Time{}

	if a.seerrPass(context.Background(), s, handled) || len(handled) != 0 {
		t.Fatalf("a failed auto-fetch must stay unhandled: %v", handled)
	}
	if !a.seerrPass(context.Background(), s, handled) || handled[11].IsZero() {
		t.Fatalf("the second pass must fetch the request again and mark it handled: %v", handled)
	}
	if a.seerrPass(context.Background(), s, handled) || lookups.Load() != 2 {
		t.Fatalf("a handled request must not be fetched a third time: %d lookups", lookups.Load())
	}
}
