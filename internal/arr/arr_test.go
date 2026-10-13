package arr

import "testing"

func TestMapRemotePath(t *testing.T) {
	ms := []RemotePathMapping{
		{Host: "qbittorrent", RemotePath: "/downloads/", LocalPath: "/data/torrents/"},
		{Host: "qbittorrent", RemotePath: "/downloads/tv", LocalPath: "/tv-dl"},
		{Host: "other", RemotePath: "/downloads/", LocalPath: "/wrong/"},
	}
	cases := []struct{ host, in, want string }{
		{"qbittorrent", "/downloads/movies/X", "/data/torrents/movies/X"},
		{"qbittorrent", "/downloads/tv/Show S01", "/tv-dl/Show S01"},
		{"qbittorrent", "/downloadsx/a", "/downloadsx/a"},
		{"other", "/downloads/a", "/wrong/a"},
		{"unknown", "/downloads/tv/a", "/tv-dl/a"},
	}
	for _, c := range cases {
		if got := MapRemotePath(ms, c.host, c.in); got != c.want {
			t.Errorf("MapRemotePath(%q, %q) = %q, want %q", c.host, c.in, got, c.want)
		}
	}
}

func TestMatchCached(t *testing.T) {
	rs := []CachedRelease{
		{GUID: "g1", Title: "Show.S01E01.1080p.WEB-DL", Size: 1000},
		{GUID: "g2", Title: "Show.S01E01.720p", InfoHash: "ABCDEF", Size: 500},
		{GUID: "g3", Title: "Show S01E01 2160p", Size: 9000},
	}
	cases := []struct {
		guid, hash, title string
		size              int64
		want              string
	}{
		{"g3", "", "", 0, "g3"},
		{"x", "abcdef", "", 0, "g2"},
		{"x", "", "show s01e01 1080p web dl", 1010, "g1"},
		{"x", "", "Show.S01E01.2160p", 5000, ""}, // size too far off
	}
	for _, c := range cases {
		got, ok := MatchCached(rs, c.guid, c.hash, c.title, c.size)
		if (c.want == "") == ok || ok && got.GUID != c.want {
			t.Errorf("MatchCached(%q,%q,%q) = %q,%v want %q", c.guid, c.hash, c.title, got.GUID, ok, c.want)
		}
	}
}
