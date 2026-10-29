package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/noxaur/roarr/internal/app"
	"github.com/noxaur/roarr/internal/config"
)

// The UI saves by sending back what GET returned, with its edits: PUT
// replaces the whole object. Every field must survive that round trip, or a
// save from one Admin tab resets settings on another.
func TestSettingsRoundTrip(t *testing.T) {
	// Environment values override the file; keep them out of the comparison.
	for _, k := range []string{"SONARR_URL", "SONARR_API_KEY", "QBIT_URL", "QBIT_USERNAME", "QBIT_PASSWORD", "AUTH_MODE", "AUTH_PASSWORD",
		"SEERR_AUTO_FETCH", "TRANSCODE", "TRANSCODE_CONCURRENCY", "TRANSCODE_NICE", "TRANSCODE_CRF", "TRANSCODE_PRESET", "TRANSCODE_FILM_GRAIN",
		"TRANSCODE_MIN_SAVINGS", "TRANSCODE_PATH_MAP", "VERIFY_MEDIA", "VERIFY_AUTO_REGRAB", "VERIFY_PATH_MAP", "IMPORT_MODE", "TV_ROOT",
		"MOVIES_ROOT", "IMPORT_TRANSFER", "QBIT_PATH_MAP", "QBIT_CATEGORY", "QBIT_SAVE_PATH"} {
		t.Setenv(k, "")
	}
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	off := false
	want, err := store.Update(func(s *config.Settings) {
		s.Sonarr = config.Service{URL: "http://sonarr:8989", APIKey: "sonarr-key"}
		s.QBit = config.Service{URL: "http://qbit:8080", Username: "admin", Password: "qbit-pass"}
		s.AuthMode, s.AuthPassword = "password", "secret"
		s.Behaviour.SeerrAutoFetch = true
		s.Transcode = config.Transcode{Enabled: true, Concurrency: 3, Nice: 5, CRF: 28, Preset: 8, FilmGrain: 12, MinSavings: 20, PathMap: "/tv=/media/tv",
			Schedule: []config.Window{{Days: []int{1, 2, 3, 4, 5}, From: "22:00", To: "07:00"}, {From: "00:00", To: "24:00"}}}
		s.Verify = config.Verify{Enabled: &off, AutoRegrab: true, PathMap: "/downloads=/data/downloads"}
		s.Import = config.Import{Mode: "native", TVRoot: "/media/tv", MovieRoot: "/media/movies", Transfer: "copy", PathMap: "/dl=/data/dl", Category: "films", SavePath: "/dl/roarr"}
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{app: &app.App{Settings: store}}

	// GET, then PUT the settings object back untouched, as the UI does.
	get := httptest.NewRecorder()
	s.getSettings(get, httptest.NewRequest("GET", "/api/settings", nil))
	var got struct {
		Settings json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	put := httptest.NewRecorder()
	s.putSettings(put, httptest.NewRequest("PUT", "/api/settings", bytes.NewReader(got.Settings)))
	if put.Code != 200 {
		t.Fatalf("PUT: %d %s", put.Code, put.Body)
	}
	if after := store.Get(); !reflect.DeepEqual(after, want) {
		t.Fatalf("settings changed by a round trip:\nwant %+v\ngot  %+v", want, after)
	}

	// Unset verify.enabled ("auto" in the UI) must also survive.
	if _, err := store.Update(func(s *config.Settings) { s.Verify.Enabled = nil }); err != nil {
		t.Fatal(err)
	}
	get = httptest.NewRecorder()
	s.getSettings(get, httptest.NewRequest("GET", "/api/settings", nil))
	_ = json.Unmarshal(get.Body.Bytes(), &got)
	put = httptest.NewRecorder()
	s.putSettings(put, httptest.NewRequest("PUT", "/api/settings", bytes.NewReader(got.Settings)))
	if v := store.Get().Verify; v.Enabled != nil || !v.AutoRegrab {
		t.Fatalf("verify after auto round trip: %+v", v)
	}
}
