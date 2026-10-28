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
	if tc.Enabled || tc.Concurrency != 1 || tc.Preset != 6 || tc.MinSavings != 15 {
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
