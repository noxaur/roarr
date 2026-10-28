package app

import "testing"

func TestMapPath(t *testing.T) {
	spec := "/tv=/media/tv, /movies/=/media/films"
	for in, want := range map[string]string{
		"/tv/Show/S01E01.mkv":    "/media/tv/Show/S01E01.mkv",
		"/movies/A (2020)/a.mkv": "/media/films/A (2020)/a.mkv",
		"/tvshows/x.mkv":         "/tvshows/x.mkv",
		"/other/x.mkv":           "/other/x.mkv",
	} {
		if got := mapPath(spec, in); got != want {
			t.Errorf("mapPath(%q) = %q, want %q", in, got, want)
		}
	}
	if mapPath("", "/a/b") != "/a/b" {
		t.Error("empty map must not change paths")
	}
}
