package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/noxaur/roarr/internal/arr"
)

func TestDeleteEpisodesDeletesEachFileOnce(t *testing.T) {
	var deleted []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/episode":
			// 11 and 12 share a double-episode file; 13 has nothing on disk.
			w.Write([]byte(`[{"id":11,"hasFile":true,"episodeFileId":7},{"id":12,"hasFile":true,"episodeFileId":7},
				{"id":13,"hasFile":false},{"id":14,"hasFile":true,"episodeFileId":9}]`))
		case "DELETE /api/v3/episodefile/bulk":
			var body struct {
				IDs []int `json:"episodeFileIds"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			deleted = body.IDs
			w.Write([]byte(`{}`))
		default:
			http.Error(w, r.URL.Path, 404)
		}
	}))
	defer srv.Close()
	a := testApp(t, &Clients{Sonarr: arr.NewSonarr(srv.URL, "k")})
	a.Library = newLibrary(a)

	res, err := a.SeriesEpisodes(context.Background(), 3, EpisodeAction{Action: "delete", EpisodeIDs: []int{11, 12, 13}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deleted, []int{7}) || res.Count != 1 {
		t.Fatalf("deleted %v (count %d), want [7]", deleted, res.Count)
	}

	deleted = nil
	res, _ = a.SeriesEpisodes(context.Background(), 3, EpisodeAction{Action: "delete", EpisodeIDs: []int{13}})
	if deleted != nil || !res.OK {
		t.Fatalf("nothing on disk: deleted %v, res %+v", deleted, res)
	}
}

func TestProfilesHideBypassProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 2, "name": "Ultra-HD"}, {"id": 99, "name": arr.BypassProfileName}, {"id": 1, "name": "any"}})
	}))
	defer srv.Close()
	a := testApp(t, &Clients{Radarr: arr.NewRadarr(srv.URL, "k")})
	ps, err := a.Profiles(context.Background(), "movie")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Profile{{1, "any"}, {2, "Ultra-HD"}}; !reflect.DeepEqual(ps, want) {
		t.Fatalf("profiles = %v, want %v", ps, want)
	}
}
