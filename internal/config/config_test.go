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
