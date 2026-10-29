package config

import "testing"

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		" http://sonarr:8989/ ": "http://sonarr:8989",
		"sonarr:8989":           "http://sonarr:8989",
		"192.168.1.5:7878/":     "http://192.168.1.5:7878",
		"https://x.example/r":   "https://x.example/r",
		"":                      "",
	} {
		if got := NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTranscodeDefaultsAndEnv(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tc := st.Get().Transcode
	if tc.Enabled || tc.Concurrency != 1 || tc.Preset != 8 || tc.MinSavings != 15 {
		t.Errorf("defaults wrong: %+v", tc)
	}
	t.Setenv("TRANSCODE", "true")
	t.Setenv("TRANSCODE_CONCURRENCY", "99")
	t.Setenv("TRANSCODE_NICE", "0")
	t.Setenv("TRANSCODE_FILM_GRAIN", "8")
	st, _ = Open(dir)
	tc = st.Get().Transcode
	if !tc.Enabled || tc.Concurrency != 8 || tc.Nice != 0 || tc.FilmGrain != 8 {
		t.Errorf("env not applied or not clamped: %+v", tc)
	}
}

func TestVerifyEnv(t *testing.T) {
	t.Setenv("VERIFY_MEDIA", "auto")
	t.Setenv("VERIFY_AUTO_REGRAB", "true")
	t.Setenv("VERIFY_PATH_MAP", "/downloads=/data")
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := s.Get().Verify
	if v.Enabled != nil || !v.AutoRegrab || v.PathMap != "/downloads=/data" {
		t.Fatalf("auto: %+v", v)
	}
	t.Setenv("VERIFY_MEDIA", "false")
	s, _ = Open(t.TempDir())
	if v := s.Get().Verify; v.Enabled == nil || *v.Enabled {
		t.Fatalf("off: %+v", v)
	}
}

func TestImportSettings(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if im := s.Get().Import; im.Mode != "arr" || im.Transfer != "hardlink" || im.Category != "roarr" || im.Native() {
		t.Fatalf("defaults: %+v", im)
	}
	t.Setenv("IMPORT_MODE", "Native")
	t.Setenv("TV_ROOT", "/data/tv")
	t.Setenv("MOVIES_ROOT", "/data/movies")
	t.Setenv("IMPORT_TRANSFER", "teleport")
	t.Setenv("QBIT_PATH_MAP", "/downloads=/data/downloads")
	s, err = Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	im := s.Get().Import
	if !im.Native() || im.TVRoot != "/data/tv" || im.MovieRoot != "/data/movies" || im.Transfer != "hardlink" || im.PathMap != "/downloads=/data/downloads" {
		t.Fatalf("from env: %+v", im)
	}
	if !s.Locked()["IMPORT_MODE"] {
		t.Fatal("IMPORT_MODE should be locked")
	}
}
