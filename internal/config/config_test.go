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
