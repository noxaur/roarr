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
