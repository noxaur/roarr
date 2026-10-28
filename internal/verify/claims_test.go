package verify

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func loadProbe(t *testing.T, name string) *Probe {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProbe(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseClaims(t *testing.T) {
	cases := []struct {
		title string
		want  Claims
	}{
		{"The.Expanse.S01E01.1080p.WEB-DL.DDP5.1.H.264-NTb", Claims{Resolution: 1080, Codec: "x264"}},
		{"Dune.Part.Two.2024.MULTi.TRUEFRENCH.2160p.UHD.BluRay.x265.10bit.HDR.DTS-HD.MA.7.1-GROUP", Claims{Resolution: 2160, Codec: "x265", HDR: "HDR", TenBit: true, Multi: true, Audio: []string{"fra"}}},
		{"Show.S02E03.720p.HDTV.x264.ENG.SUBS-GRP", Claims{Resolution: 720, Codec: "x264", AnySubs: true, Subs: []string{"eng"}}},
		{"Movie.2019.1080p.WEBRip.x264.ESub-GRP", Claims{Resolution: 1080, Codec: "x264", AnySubs: true, Subs: []string{"eng"}}},
		{"Le.Film.2021.VOSTFR.1080p.WEB.H264-GRP", Claims{Resolution: 1080, Codec: "x264", AnySubs: true, Subs: []string{"fra"}}},
		{"Der.Film.2020.German.DL.1080p.BluRay.x264-GRP", Claims{Resolution: 1080, Codec: "x264", Multi: true, Audio: []string{"deu"}}},
		{"[SubsPlease] Frieren - 05 (1080p) [ABCD1234].mkv", Claims{Resolution: 1080}},
		{"Anime.S01.1080p.BluRay.x265.Dual.Audio.Multi-Subs-GRP", Claims{Resolution: 1080, Codec: "x265", Multi: true, MultiSubs: true, AnySubs: true}},
		{"Film.2022.1080p.WEB-DL.ITA.ENG.Sub.ITA.ENG-GRP", Claims{Resolution: 1080, Multi: true, Audio: []string{"ita", "eng"}, AnySubs: true, Subs: []string{"ita", "eng"}}},
		// Language-looking title words do not count.
		{"It.2017.1080p.BluRay.x264-GRP", Claims{Resolution: 1080, Codec: "x264"}},
		{"The.Office.UK.S01E01.720p.WEB-DL-GRP", Claims{Resolution: 720}},
		{"Show.S01E01.1080p.WEB-DL.x264.HC-GRP", Claims{Resolution: 1080, Codec: "x264", HardSubbed: true}},
	}
	for _, c := range cases {
		got := ParseClaims(c.title)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.title, got, c.want)
		}
	}
}

func checkByName(cs []Check, name string) (Check, bool) {
	for _, c := range cs {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestCompareClaimsMatches(t *testing.T) {
	p := loadProbe(t, "multi.json")
	cl := ParseClaims("Movie.2024.MULTi.VFF.1080p.BluRay.x265.10bit.HDR.ENG.SUBS-GRP")
	cs := CompareClaims(cl, p)
	for _, name := range []string{"resolution", "codec", "hdr", "bitdepth", "audio", "subtitles"} {
		c, ok := checkByName(cs, name)
		if !ok {
			t.Fatalf("no %s check in %+v", name, cs)
		}
		if c.Level != Pass {
			t.Errorf("%s = %s: %s", name, c.Level, c.Reason)
		}
	}
	// The cover art is not the video stream.
	if v, _ := p.Video(); v.CodecName != "hevc" {
		t.Fatalf("video stream = %s", v.CodecName)
	}
}

func TestCompareClaimsMismatches(t *testing.T) {
	p := loadProbe(t, "fake.json")
	cl := ParseClaims("Big.Movie.2023.MULTi.2160p.WEB-DL.x265.ENG.SUBS-FAKE")
	cs := CompareClaims(cl, p)
	want := map[string]Level{"resolution": Fail, "codec": Warn, "audio": Fail, "subtitles": Fail}
	for name, lvl := range want {
		c, ok := checkByName(cs, name)
		if !ok {
			t.Fatalf("no %s check", name)
		}
		if c.Level != lvl {
			t.Errorf("%s = %s (%s), want %s", name, c.Level, c.Reason, lvl)
		}
	}
	if c, _ := checkByName(cs, "subtitles"); !strings.Contains(c.Reason, "empty") {
		t.Errorf("subtitle reason = %q", c.Reason)
	}
	if d := p.Duration(); d != 6730 {
		t.Errorf("duration from DURATION tag = %v", d)
	}
}

func TestCompareAudioLanguage(t *testing.T) {
	p := loadProbe(t, "fake.json")
	c := compareAudio(ParseClaims("Film.2020.GERMAN.1080p.BluRay.x264-GRP"), p)
	if c.Level != Fail || !strings.Contains(c.Reason, "German") {
		t.Fatalf("german claim vs russian audio: %s %s", c.Level, c.Reason)
	}
	// Untagged audio cannot be confirmed either way.
	p.Streams[1].Tags = nil
	c = compareAudio(ParseClaims("Film.2020.GERMAN.1080p.BluRay.x264-GRP"), p)
	if c.Level != Warn {
		t.Fatalf("untagged: %s %s", c.Level, c.Reason)
	}
	// No claim: one track is fine.
	if c := compareAudio(ParseClaims("Film.2020.1080p.BluRay.x264-GRP"), p); c.Level != Pass {
		t.Fatalf("no claim: %s %s", c.Level, c.Reason)
	}
}

func TestSubtitleChecks(t *testing.T) {
	p := loadProbe(t, "multi.json")
	// Only a forced French track: claim of full French subs is a warning.
	c, _ := compareSubs(ParseClaims("Film.2020.1080p.BluRay.x264.SUBFRENCH-GRP"), p)
	if c.Level != Warn || !strings.Contains(c.Reason, "forced") {
		t.Fatalf("forced only: %s %s", c.Level, c.Reason)
	}
	c, _ = compareSubs(ParseClaims("Film.2020.1080p.BluRay.x264.Sub.SPANISH-GRP"), p)
	if c.Level != Fail {
		t.Fatalf("missing spanish: %s %s", c.Level, c.Reason)
	}
	if _, ok := compareSubs(ParseClaims("Film.2020.1080p.BluRay.x264-GRP"), p); ok {
		t.Fatal("no claim should produce no subtitle check")
	}
	// Hardsubs need no track.
	p.Streams = p.Streams[:4]
	c, _ = compareSubs(ParseClaims("Show.S01E01.1080p.WEB.x264.HC.SUBBED-GRP"), p)
	if c.Level != Pass {
		t.Fatalf("hardsubbed: %s %s", c.Level, c.Reason)
	}
}

func TestResolutionTier(t *testing.T) {
	cases := map[[2]int]int{{3840, 2160}: 2160, {3840, 1600}: 2160, {1920, 800}: 1080, {1440, 1080}: 1080, {1280, 720}: 720, {1280, 534}: 720, {720, 576}: 576, {720, 480}: 480, {640, 360}: 480}
	for wh, want := range cases {
		if got := resolutionTier(wh[0], wh[1]); got != want {
			t.Errorf("%dx%d = %d, want %d", wh[0], wh[1], got, want)
		}
	}
}

func TestStreamLang(t *testing.T) {
	s := Stream{Tags: map[string]string{"language": "und", "title": "English (SDH)"}}
	if s.Lang() != "eng" {
		t.Fatalf("title fallback = %q", s.Lang())
	}
	s = Stream{Tags: map[string]string{"LANGUAGE": "pt-BR"}}
	if s.Lang() != "por" {
		t.Fatalf("region tag = %q", s.Lang())
	}
	if (Stream{}).Lang() != "" {
		t.Fatal("untagged should be empty")
	}
}
