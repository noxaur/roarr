package verify

import (
	"context"
	"image"
	"reflect"
	"strings"
	"testing"
)

func TestFindSidecars(t *testing.T) {
	movie := File{Name: "Movie.2020.1080p/Movie.2020.1080p.mkv"}
	files := []File{
		movie,
		{Name: "Movie.2020.1080p/Movie.2020.1080p.en.srt", Size: 9},
		{Name: "Movie.2020.1080p/Movie.2020.1080p.eng.forced.srt", Size: 9},
		{Name: "Movie.2020.1080p/Movie.2020.1080p.srt", Size: 9},
		{Name: "Movie.2020.1080p/Subs/3_French.srt", Size: 9},
		{Name: "Movie.2020.1080p/Movie.2020.1080p.nfo", Size: 9},
	}
	got := FindSidecars(files, movie, 1)
	want := Sidecars{Matched: []Sidecar{
		{Name: "Movie.2020.1080p/Movie.2020.1080p.en.srt", Lang: "eng", Size: 9},
		{Name: "Movie.2020.1080p/Movie.2020.1080p.eng.forced.srt", Lang: "eng", Forced: true, Size: 9},
		{Name: "Movie.2020.1080p/Movie.2020.1080p.srt", Size: 9},
		{Name: "Movie.2020.1080p/Subs/3_French.srt", Lang: "fra", Size: 9},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("movie sidecars:\n got %+v\nwant %+v", got, want)
	}

	// A pack: only the first episode's subtitles belong to it.
	ep := File{Name: "Show.S01/Show.S01E01.mkv"}
	files = []File{
		ep, {Name: "Show.S01/Show.S01E02.mkv"},
		{Name: "Show.S01/Subs/Show.S01E01/2_English.srt", Size: 9},
		{Name: "Show.S01/Subs/Show.S01E02/2_English.srt", Size: 9},
		{Name: "Show.S01/Show.S01E02.en.srt", Size: 9},
	}
	got = FindSidecars(files, ep, 2)
	if len(got.Matched) != 1 || got.Matched[0].Lang != "eng" || got.Other != 2 {
		t.Fatalf("pack sidecars: %+v", got)
	}
	// Loose Subs/ in a pack cannot be tied to an episode.
	got = FindSidecars([]File{ep, {Name: "Show.S01/Show.S01E02.mkv"}, {Name: "Show.S01/Subs/English.srt", Size: 9}}, ep, 2)
	if len(got.Matched) != 0 || got.Other != 1 {
		t.Fatalf("loose pack subs: %+v", got)
	}
}

func TestSubtitleClaimWithSidecars(t *testing.T) {
	p := loadProbe(t, "multi.json")
	p.Streams = p.Streams[:4] // no embedded subtitles
	cl := ParseClaims("Movie.2020.1080p.BluRay.x264.ENG.SUBS-GRP")
	cases := []struct {
		name string
		sc   Sidecars
		want Level
	}{
		{"english sidecar", Sidecars{Matched: []Sidecar{{Name: "m.en.srt", Lang: "eng", Size: 900}}}, Pass},
		{"untagged sidecar", Sidecars{Matched: []Sidecar{{Name: "m.srt", Size: 900}}}, Warn},
		{"unmatched subtitle files", Sidecars{Other: 3}, Warn},
		{"french sidecar plus unmatched", Sidecars{Matched: []Sidecar{{Name: "m.fr.srt", Lang: "fra", Size: 9}}, Other: 1}, Warn},
		{"only another language", Sidecars{Matched: []Sidecar{{Name: "m.fr.srt", Lang: "fra", Size: 9}}}, Fail},
		{"empty english sidecar", Sidecars{Matched: []Sidecar{{Name: "m.en.srt", Lang: "eng"}}}, Fail},
		{"nothing at all", Sidecars{}, Fail},
	}
	for _, c := range cases {
		got, _ := compareSubs(cl, p, c.sc)
		if got.Level != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got.Level, got.Reason, c.want)
		}
	}
	// An untagged embedded track makes a missing language uncertain too.
	p = loadProbe(t, "multi.json")
	p.Streams[5].Tags = nil
	if got, _ := compareSubs(cl, p, Sidecars{}); got.Level != Warn {
		t.Errorf("untagged embedded: %s (%s)", got.Level, got.Reason)
	}
}

// A release with English subtitles only as a sidecar passes end to end.
func TestVerifyUsesSidecarSubtitles(t *testing.T) {
	v, fr, file := newFake(t, "multi.json", func(at float64) image.Image { return scene(int(at)) })
	p, _ := ParseProbe(fr.probe)
	p.Streams = p.Streams[:4]
	fr.probe = mustJSON(t, p)
	vd := v.Verify(context.Background(), Target{
		Release:    "Movie.2024.MULTi.VFF.1080p.BluRay.x265.10bit.HDR.ENG.SUBS-GRP",
		Kind:       "movie",
		RuntimeMin: 44,
		Files:      []File{{Name: "Movie/Movie.mkv", Path: file, Size: 1 << 30}, {Name: "Movie/Movie.eng.srt", Size: 40000}},
	})
	c, _ := checkByName(vd.Checks, "subtitles")
	if c.Level != Pass || !strings.Contains(c.Reason, "sidecar") || vd.Level != Pass {
		t.Fatalf("subtitles %s (%s); verdict %s: %s", c.Level, c.Reason, vd.Level, vd.Summary)
	}
}
