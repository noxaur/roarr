package app

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/verify"
)

func TestVerifyPathMap(t *testing.T) {
	pm := parsePathMap(` /downloads = /data/downloads , D:\Torrents=/torrents,/downloads/tv=/tv,broken`)
	cases := map[string]string{
		"/downloads/movies/M (2020)/m.mkv": "/data/downloads/movies/M (2020)/m.mkv",
		"/downloads/tv/Show/s01e01.mkv":    "/tv/Show/s01e01.mkv", // longest prefix wins
		`D:\Torrents\Show\e01.mkv`:         "/torrents/Show/e01.mkv",
		"/downloadsX/a.mkv":                "/downloadsX/a.mkv", // not a path prefix
		"/other/a.mkv":                     "/other/a.mkv",
	}
	for in, want := range cases {
		if got := mapVerifyPath(pm, in); got != filepath.FromSlash(want) {
			t.Errorf("mapVerifyPath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := joinClientPath(`D:\Torrents\`, "Show/e01.mkv"); got != "D:/Torrents/Show/e01.mkv" {
		t.Errorf("join = %q", got)
	}
}

func TestVerifyLearnAndBlock(t *testing.T) {
	v := &Verification{recs: map[string]*VerifyRecord{}}
	q := []arr.QueueRecord{
		{ID: 1, SeriesID: 7, EpisodeID: 101, Title: "Show.S01.1080p.WEB-DL.x264-FAKE", DownloadID: "ABCDEF0123456789ABCDEF0123456789ABCDEF01"},
		{ID: 2, SeriesID: 7, EpisodeID: 102, Title: "Show.S01.1080p.WEB-DL.x264-FAKE", DownloadID: "ABCDEF0123456789ABCDEF0123456789ABCDEF01"},
		{ID: 3, Title: "no id"},
	}
	v.learn("sonarr", q)
	v.learn("sonarr", q) // idempotent
	recs := v.Records(0, 0)
	if len(recs) != 1 {
		t.Fatalf("records = %+v", recs)
	}
	r := recs[0]
	if r.Hash != "abcdef0123456789abcdef0123456789abcdef01" || r.Kind != "tv" || r.SeriesID != 7 || !reflect.DeepEqual(r.EpisodeIDs, []int{101, 102}) || r.State != "pending" {
		t.Fatalf("record = %+v", r)
	}
	if len(v.Records(8, 0)) != 0 || len(v.Records(7, 0)) != 1 {
		t.Fatal("series filter")
	}

	rel := Release{Title: "Show.S01.1080p.WEB-DL.x264-FAKE"}
	if v.annotate(rel).Rejected {
		t.Fatal("pending release must not be blocked")
	}
	v.update(r.Hash, func(rec *VerifyRecord) {
		rec.State, rec.Verdict = "done", &verify.Verdict{Level: verify.Fail, Summary: "only archives"}
	})
	got := v.annotate(rel)
	if !got.Rejected || len(got.Reasons) != 1 || got.Reasons[0] != "failed verification: only archives" {
		t.Fatalf("annotate = %+v", got)
	}
	// Same torrent under another title is caught by its hash.
	if !v.annotate(Release{Title: "x", InfoHash: "ABCDEF0123456789ABCDEF0123456789ABCDEF01"}).Rejected {
		t.Fatal("hash match")
	}
	v.mu.Lock()
	v.recs[r.Hash].Dismissed = true
	v.mu.Unlock()
	if v.annotate(rel).Rejected {
		t.Fatal("dismissed release must not be blocked")
	}
	var nilV *Verification
	if nilV.annotate(rel).Rejected || nilV.Enabled() || nilV.Records(0, 0) != nil {
		t.Fatal("nil verification must be inert")
	}
}
