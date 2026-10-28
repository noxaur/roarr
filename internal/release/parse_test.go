package release

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Info
	}{
		{"The.Expanse.S02E05.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb", Info{Title: "The Expanse", Seasons: []int{2}, Episodes: []int{5}, Resolution: 1080, Source: SourceWEBDL, Codec: "x264", Audio: "DDP", Channels: "5.1", Service: "AMZN", Group: "NTb"}},
		{"Severance.S01E03.2160p.WEB-DL.DV.HDR.DDP5.1.Atmos.H.265-FLUX", Info{Title: "Severance", Seasons: []int{1}, Episodes: []int{3}, Resolution: 2160, Source: SourceWEBDL, Codec: "x265", HDR: "DV", Audio: "Atmos", Channels: "5.1", Group: "FLUX"}},
		{"Breaking.Bad.S05E01E02.720p.HDTV.x264-KILLERS", Info{Title: "Breaking Bad", Seasons: []int{5}, Episodes: []int{1, 2}, Resolution: 720, Source: SourceHDTV, Codec: "x264", Group: "KILLERS"}},
		{"Breaking.Bad.S05E01-E04.1080p.BluRay.x264-ROVERS", Info{Title: "Breaking Bad", Seasons: []int{5}, Episodes: []int{1, 2, 3, 4}, Resolution: 1080, Source: SourceBluRay, Codec: "x264", Group: "ROVERS"}},
		{"The.Expanse.S03.1080p.BluRay.x265.10bit.AAC.5.1-Silence", Info{Title: "The Expanse", Seasons: []int{3}, FullSeason: true, Resolution: 1080, Source: SourceBluRay, Codec: "x265", BitDepth10: true, Audio: "AAC", Channels: "5.1", Group: "Silence"}},
		{"The Expanse (2015) Season 1-6 S01-S06 (1080p BluRay x265 HEVC 10bit AAC 5.1 Silence) [QxR]", Info{Title: "The Expanse", Year: 2015, Seasons: []int{1, 2, 3, 4, 5, 6}, FullSeason: true, Resolution: 1080, Source: SourceBluRay, Codec: "x265", BitDepth10: true, Audio: "AAC", Channels: "5.1", Group: "QxR"}},
		{"The.Expanse.S01-S06.COMPLETE.1080p.WEB-DL.DDP5.1.H.264-NTb", Info{Title: "The Expanse", Seasons: []int{1, 2, 3, 4, 5, 6}, FullSeason: true, Resolution: 1080, Source: SourceWEBDL, Codec: "x264", Audio: "DDP", Channels: "5.1", Group: "NTb"}},
		{"Breaking.Bad.Complete.Series.1080p.BluRay.x265.10bit.AAC.5.1-Silence", Info{Title: "Breaking Bad", Complete: true, Resolution: 1080, Source: SourceBluRay, Codec: "x265", BitDepth10: true, Audio: "AAC", Channels: "5.1", Group: "Silence"}},
		{"Severance (2022) Season 2 S02 (1080p ATVP WEB-DL x265 HEVC 10bit EAC3 Atmos 5.1 Silence) [QxR]", Info{Title: "Severance", Year: 2022, Seasons: []int{2}, FullSeason: true, Resolution: 1080, Source: SourceWEBDL, Codec: "x265", BitDepth10: true, Audio: "Atmos", Channels: "5.1", Service: "ATVP", Group: "QxR"}},
		{"[SubsPlease] Sousou no Frieren - 05 (1080p) [ABCD1234].mkv", Info{Title: "Sousou no Frieren", Absolute: []int{5}, Resolution: 1080, Group: "SubsPlease"}},
		{"[Erai-raws] Sousou no Frieren - 27v2 [1080p][Multiple Subtitle]", Info{Title: "Sousou no Frieren", Absolute: []int{27}, Resolution: 1080, Group: "Erai-raws", Subtitles: []string{"Multi"}, Subbed: true}},
		{"[Judas] Sousou no Frieren (Season 1) [1080p][HEVC x265 10bit][Multi-Subs] (Batch)", Info{Title: "Sousou no Frieren", Seasons: []int{1}, FullSeason: true, Resolution: 1080, Codec: "x265", BitDepth10: true, Group: "Judas", Subtitles: []string{"Multi"}, Subbed: true}},
		{"[Anime Time] Sousou no Frieren (01-28) [Dual Audio][1080p][HEVC 10bit x265][AAC][Eng Sub] [Batch]", Info{Title: "Sousou no Frieren", Absolute: rangeInts(1, 28), Resolution: 1080, Codec: "x265", BitDepth10: true, Audio: "AAC", Group: "Anime Time", DualAudio: true, Multi: true, Subtitles: []string{"English"}, Subbed: true}},
		{"Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.DV.HDR.HEVC.TrueHD.Atmos.7.1-FraMeSToR", Info{Title: "Dune Part Two", Year: 2024, Resolution: 2160, Source: SourceRemux, Codec: "x265", HDR: "DV", Audio: "Atmos", Channels: "7.1", Group: "FraMeSToR", Remux: true}},
		{"The Matrix (1999) [1080p] [BluRay] [x265] [10bit] [5.1] [YTS.MX]", Info{Title: "The Matrix", Year: 1999, Resolution: 1080, Source: SourceBluRay, Codec: "x265", BitDepth10: true, Channels: "5.1", Group: "YTS.MX"}},
		{"Blade.Runner.2049.2017.1080p.BluRay.x264-SPARKS", Info{Title: "Blade Runner 2049", Year: 2017, Resolution: 1080, Source: SourceBluRay, Codec: "x264", Group: "SPARKS"}},
		{"1923.S02E01.1080p.WEB.h264-ETHEL", Info{Title: "1923", Seasons: []int{2}, Episodes: []int{1}, Resolution: 1080, Source: SourceWEB, Codec: "x264", Group: "ETHEL"}},
		{"Dune.2021.FRENCH.1080p.WEB.x264-FW", Info{Title: "Dune", Year: 2021, Resolution: 1080, Source: SourceWEB, Codec: "x264", Group: "FW", Languages: []string{"French"}}},
		{"Some.Movie.2023.HDCAM.x264-NOGRP", Info{Title: "Some Movie", Year: 2023, Source: SourceCam, Codec: "x264", Group: "NOGRP"}},
		{"Show.Name.1x05.HDTV.XviD-LOL", Info{Title: "Show Name", Seasons: []int{1}, Episodes: []int{5}, Source: SourceHDTV, Codec: "xvid", Group: "LOL"}},
		{"Show Name S01 S02 S03 1080p WEB-DL", Info{Title: "Show Name", Seasons: []int{1, 2, 3}, FullSeason: true, Resolution: 1080, Source: SourceWEBDL}},
		{"Pluribus.2025.S01E04.1080p.WEB.H264-GLHF", Info{Title: "Pluribus", Year: 2025, Seasons: []int{1}, Episodes: []int{4}, Resolution: 1080, Source: SourceWEB, Codec: "x264", Group: "GLHF"}},
		{"The.French.Dispatch.2021.1080p.WEB-DL.DDP5.1.H.264-NTb", Info{Title: "The French Dispatch", Year: 2021, Resolution: 1080, Source: SourceWEBDL, Codec: "x264", Audio: "DDP", Channels: "5.1", Group: "NTb"}},
		{"Dune.2021.MULTi.VFF.ENGLISH.1080p.WEB.x264-FW", Info{Title: "Dune", Year: 2021, Resolution: 1080, Source: SourceWEB, Codec: "x264", Group: "FW", Languages: []string{"French", "English"}, Multi: true}},
		{"Dune.2021.VOSTFR.1080p.WEB.x264-FW", Info{Title: "Dune", Year: 2021, Resolution: 1080, Source: SourceWEB, Codec: "x264", Group: "FW", Subtitles: []string{"French"}, Subbed: true}},
		{"Squid.Game.S01E01.1080p.NF.WEB-DL.DDP5.1.x264.NLSUBS-GROUP", Info{Title: "Squid Game", Seasons: []int{1}, Episodes: []int{1}, Resolution: 1080, Source: SourceWEBDL, Codec: "x264", Audio: "DDP", Channels: "5.1", Service: "NF", Group: "GROUP", Subtitles: []string{"Dutch"}, Subbed: true}},
		{"Severance.S01E01.REPACK.1080p.WEB.H264-GLHF", Info{Title: "Severance", Seasons: []int{1}, Episodes: []int{1}, Resolution: 1080, Source: SourceWEB, Codec: "x264", Group: "GLHF", Repack: true}},
	}
	for _, c := range cases {
		got := Parse(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q)\n got  %+v\n want %+v", c.in, got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"The Expanse":                   "expanse",
		"Frieren: Beyond Journey's End": "frieren beyond journeys end",
		"Law & Order":                   "law and order",
		"Pokémon":                       "pokemon",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

var benchTitles = []string{
	"The.Expanse.S02E05.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb",
	"The Expanse (2015) Season 1-6 S01-S06 (1080p BluRay x265 HEVC 10bit AAC 5.1 Silence) [QxR]",
	"[SubsPlease] Sousou no Frieren - 05 (1080p) [ABCD1234].mkv",
	"Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.DV.HDR.HEVC.TrueHD.Atmos.7.1-FraMeSToR",
}

func BenchmarkParse(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Parse(benchTitles[i%len(benchTitles)])
	}
}
