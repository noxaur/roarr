package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestNaming(t *testing.T) {
	cases := []struct{ got, want string }{
		{EpisodePath("/tv", "Severance", 2022, 1, []int{2}), "/tv/Severance (2022)/Season 01/Severance - S01E02"},
		{EpisodePath("/tv", "Show", 0, 3, []int{4, 5, 6}), "/tv/Show/Season 03/Show - S03E04-E06"},
		{EpisodePath("/tv", "Doctor Who", 2005, 0, []int{1}), "/tv/Doctor Who (2005)/Season 00/Doctor Who - S00E01"},
		{MoviePath("/movies", "Inception", 2010), "/movies/Inception (2010)/Inception (2010)"},
		{MoviePath("/movies", "Mission: Impossible", 1996), "/movies/Mission - Impossible (1996)/Mission - Impossible (1996)"},
		{TitleWithYear("The Office (US)", 2005), "The Office (US) (2005)"},
		{TitleWithYear("Dune (2021)", 2021), "Dune (2021)"},
		{CleanName(`What If...?`), "What If"},
		{CleanName(`AC/DC: Live <at> "River"`), "AC-DC - Live at River"},
		{SubtitleName("/tv/S/Season 01/S - S01E01", SubtitleInfo{Lang: "en", Forced: true}, ".SRT"), "/tv/S/Season 01/S - S01E01.en.forced.srt"},
		{SubtitleName("/m/M (2000)/M (2000)", SubtitleInfo{}, ".ass"), "/m/M (2000)/M (2000).ass"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]FileClass{
		"Show.S01E01.1080p.WEB.mkv":              ClassVideo,
		"Show.S01/Sample/show.s01e01.sample.mkv": ClassSample,
		"Movie.2010.1080p-sample.mkv":            ClassSample,
		"Movie.2010/Featurettes/Making Of.mkv":   ClassExtra,
		"Movie.2010/Movie.2010.Trailer.mp4":      ClassExtra,
		"Movie.2010/Subs/2_English.srt":          ClassSubtitle,
		"Movie.2010/Movie.2010.nfo":              ClassOther,
		"Show.S01E02.mkv.!qB":                    ClassVideo,
		"Samuel.Jackson.Story.2019.mkv":          ClassVideo, // "Sam..." is not a sample
	}
	for rel, want := range cases {
		if got := Classify(rel); got != want {
			t.Errorf("Classify(%q) = %s, want %s", rel, got, want)
		}
	}
}

func TestParseSubtitle(t *testing.T) {
	cases := []struct {
		rel, video string
		want       SubtitleInfo
	}{
		{"Movie.2010.1080p.en.srt", "Movie.2010.1080p", SubtitleInfo{Lang: "en"}},
		{"Movie.2010.1080p.eng.forced.srt", "Movie.2010.1080p", SubtitleInfo{Lang: "en", Forced: true}},
		{"Show.S01E02.English.SDH.srt", "", SubtitleInfo{Lang: "en", SDH: true}},
		{"Subs/Show.S01E02/3_Spanish.srt", "", SubtitleInfo{Lang: "es"}},
		{"Subs/2_English.srt", "", SubtitleInfo{Lang: "en"}},
		{"It.2017.srt", "", SubtitleInfo{}}, // "It" is the title, not Italian
		{"It.2017.it.srt", "", SubtitleInfo{Lang: "it"}},
		{"Show.S01E02.hi.srt", "Show.S01E02", SubtitleInfo{SDH: true}},
		{"Show.S01E02.pt-br.srt", "Show.S01E02", SubtitleInfo{Lang: "pt"}},
	}
	for _, c := range cases {
		if got := ParseSubtitle(c.rel, c.video); got != c.want {
			t.Errorf("ParseSubtitle(%q, %q) = %+v, want %+v", c.rel, c.video, got, c.want)
		}
	}
}

func showTitle(seasons ...int) Title {
	t := Title{Kind: "tv", Title: "The Show", Year: 2020, TmdbID: 42}
	for s, n := range seasons {
		for e := 1; e <= n; e++ {
			t.Episodes = append(t.Episodes, Episode{Season: s + 1, Number: e})
		}
	}
	return t
}

func rels(files ...string) []SourceFile {
	out := make([]SourceFile, len(files))
	for i, f := range files {
		out[i] = SourceFile{Rel: f, Path: "/dl/" + f, Size: int64(1000 + i)}
	}
	return out
}

func placements(ps []Placement) []string {
	var out []string
	for _, p := range ps {
		out = append(out, EpisodeToken(p.Season, p.Episodes))
	}
	return out
}

func TestPlanTVSingleEpisode(t *testing.T) {
	ps, skips := PlanTV(rels("The.Show.S01E02.1080p.WEB-DL.mkv"), "The.Show.S01E02.1080p.WEB-DL", showTitle(8))
	if got := placements(ps); !reflect.DeepEqual(got, []string{"S01E02"}) || len(skips) != 0 {
		t.Fatalf("got %v skips %v", got, skips)
	}
}

func TestPlanTVSeasonPack(t *testing.T) {
	files := rels(
		"The.Show.S02.1080p.BluRay-GRP/The.Show.S02E01.1080p.BluRay-GRP.mkv",
		"The.Show.S02.1080p.BluRay-GRP/The.Show.S02E02.1080p.BluRay-GRP.mkv",
		"The.Show.S02.1080p.BluRay-GRP/The.Show.S02E03E04.1080p.BluRay-GRP.mkv",
		"The.Show.S02.1080p.BluRay-GRP/Sample/the.show.s02e01.sample.mkv",
		"The.Show.S02.1080p.BluRay-GRP/Featurettes/Behind the Scenes.mkv",
		"The.Show.S02.1080p.BluRay-GRP/The.Show.S02E01.1080p.BluRay-GRP.en.srt",
		"The.Show.S02.1080p.BluRay-GRP/Subs/The.Show.S02E02.1080p.BluRay-GRP/2_English.srt",
		"The.Show.S02.1080p.BluRay-GRP/Subs/The.Show.S02E02.1080p.BluRay-GRP/3_French.srt",
		"The.Show.S02.1080p.BluRay-GRP/GRP.nfo",
	)
	ps, skips := PlanTV(files, "The.Show.S02.1080p.BluRay-GRP", showTitle(8, 6))
	if got := placements(ps); !reflect.DeepEqual(got, []string{"S02E01", "S02E02", "S02E03-E04"}) {
		t.Fatalf("placements %v", got)
	}
	if len(skips) != 2 {
		t.Fatalf("want sample + extra skipped, got %v", skips)
	}
	if n := len(ps[0].Subtitles); n != 1 || ps[0].Subtitles[0].Info.Lang != "en" {
		t.Fatalf("E01 subtitles %+v", ps[0].Subtitles)
	}
	if n := len(ps[1].Subtitles); n != 2 || ps[1].Subtitles[0].Info.Lang != "en" || ps[1].Subtitles[1].Info.Lang != "fr" {
		t.Fatalf("E02 subtitles %+v", ps[1].Subtitles)
	}
}

func TestPlanTVEpisodeOnlyNamesUseFolderSeason(t *testing.T) {
	files := rels(
		"The Show Complete/Season 1/01 - Pilot.mkv",
		"The Show Complete/Season 1/E02 - Second.mkv",
		"The Show Complete/Season 2/Episode 3.mkv",
	)
	ps, skips := PlanTV(files, "The Show Complete Series", showTitle(8, 6))
	if got := placements(ps); !reflect.DeepEqual(got, []string{"S01E01", "S01E02", "S02E03"}) {
		t.Fatalf("placements %v skips %v", got, skips)
	}
}

func TestPlanTVAnimeAbsolute(t *testing.T) {
	ti := showTitle(12, 12)
	ti.Anime = true
	files := rels("[Grp] The Show - 13 (1080p) [ABCD1234].mkv", "[Grp] The Show - 02 (1080p) [ABCD1235].mkv")
	ps, _ := PlanTV(files, "[Grp] The Show (01-24) (1080p)", ti)
	if got := placements(ps); !reflect.DeepEqual(got, []string{"S01E02", "S02E01"}) {
		t.Fatalf("placements %v", got)
	}
}

func TestPlanTVUnknownEpisodeIsUnverified(t *testing.T) {
	ps, _ := PlanTV(rels("The.Show.S03E01.720p.HDTV.mkv"), "The.Show.S03E01.720p.HDTV", showTitle(8))
	if len(ps) != 1 || !ps[0].Unverified || ps[0].Season != 3 {
		t.Fatalf("got %+v", ps)
	}
}

func TestPlanTVDuplicateEpisodeKeepsLarger(t *testing.T) {
	files := []SourceFile{{Rel: "P/The.Show.S01E01.mkv", Size: 10}, {Rel: "P/The.Show.S01E01.proper.mkv", Size: 900}}
	ps, skips := PlanTV(files, "P", showTitle(8))
	if len(ps) != 1 || ps[0].Video.Size != 900 || len(skips) != 1 {
		t.Fatalf("got %+v skips %+v", ps, skips)
	}
}

func TestPlanMovie(t *testing.T) {
	files := []SourceFile{
		{Rel: "Inception.2010.1080p/Inception.2010.1080p.mkv", Size: 9000},
		{Rel: "Inception.2010.1080p/Inception.2010.1080p.sample.mkv", Size: 50},
		{Rel: "Inception.2010.1080p/Bonus Clip.mkv", Size: 400},
		{Rel: "Inception.2010.1080p/Subs/2_English.srt", Size: 1},
		{Rel: "Inception.2010.1080p/Subs/5_Dutch.srt", Size: 1},
	}
	ps, skips := PlanMovie(files, "Inception.2010.1080p", Title{Kind: "movie", Title: "Inception", Year: 2010})
	if len(ps) != 1 || ps[0].Video.Size != 9000 || len(ps[0].Subtitles) != 2 || len(skips) != 2 {
		t.Fatalf("got %+v skips %+v", ps, skips)
	}
}

func TestPathMap(t *testing.T) {
	m, err := ParsePathMap("/downloads=/data/downloads, /downloads/tv=/tv-dl ; D:\\Torrents=/mnt/t")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"/downloads/x/a.mkv":     "/data/downloads/x/a.mkv",
		"/downloads/tv/a.mkv":    "/tv-dl/a.mkv",
		"/downloadsX/a.mkv":      "/downloadsX/a.mkv",
		`d:\Torrents\Show\a.mkv`: "/mnt/t/Show/a.mkv",
		"/elsewhere/a.mkv":       "/elsewhere/a.mkv",
	}
	for in, want := range cases {
		if got := m.Map(in); got != want {
			t.Errorf("Map(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParsePathMap("/nope"); err == nil {
		t.Error("want error for a rule without =")
	}
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTransferModes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "dl", "a.mkv")
	write(t, src, "video")

	how, err := Transfer(src, filepath.Join(dir, "lib", "x", "a.mkv"), ModeHardlink)
	if err != nil || how != "hardlink" {
		t.Fatalf("hardlink: %s %v", how, err)
	}
	si, _ := os.Stat(src)
	di, _ := os.Stat(filepath.Join(dir, "lib", "x", "a.mkv"))
	if !os.SameFile(si, di) {
		t.Fatal("hardlink did not link")
	}
	// Importing the same link again is a no-op, not an error.
	if _, err := Transfer(src, filepath.Join(dir, "lib", "x", "a.mkv"), ModeHardlink); err != nil {
		t.Fatalf("relink: %v", err)
	}

	how, err = Transfer(src, filepath.Join(dir, "lib", "copy.mkv"), ModeCopy)
	if err != nil || how != "copy" {
		t.Fatalf("copy: %s %v", how, err)
	}
	ci, _ := os.Stat(filepath.Join(dir, "lib", "copy.mkv"))
	if os.SameFile(si, ci) {
		t.Fatal("copy produced a link")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "lib", "copy.mkv")); string(b) != "video" {
		t.Fatalf("copy content %q", b)
	}

	write(t, filepath.Join(dir, "lib", "taken.mkv"), "other")
	if _, err := Transfer(src, filepath.Join(dir, "lib", "taken.mkv"), ModeCopy); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}

	mv := filepath.Join(dir, "dl", "b.mkv")
	write(t, mv, "move me")
	how, err = Transfer(mv, filepath.Join(dir, "lib", "b.mkv"), ModeMove)
	if err != nil || how != "move" {
		t.Fatalf("move: %s %v", how, err)
	}
	if _, err := os.Stat(mv); !os.IsNotExist(err) {
		t.Fatal("move left the source behind")
	}
}

// TestTransferHardlinkFallsBackToCopy simulates a library on another
// filesystem (or Docker volume): the link fails with EXDEV and the file is
// copied instead.
func TestTransferHardlinkFallsBackToCopy(t *testing.T) {
	orig := linkFile
	defer func() { linkFile = orig }()
	linkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }

	dir := t.TempDir()
	src := filepath.Join(dir, "dl", "a.mkv")
	write(t, src, "data")
	dst := filepath.Join(dir, "lib", "a.mkv")
	how, err := Transfer(src, dst, ModeHardlink)
	if err != nil || how != "copy" {
		t.Fatalf("got %s, %v; want copy", how, err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "data" {
		t.Fatalf("content %q", got)
	}
	si, _ := os.Stat(src)
	di, _ := os.Stat(dst)
	if os.SameFile(si, di) {
		t.Fatal("fallback produced a link")
	}
	if ents, _ := os.ReadDir(filepath.Dir(dst)); len(ents) != 1 {
		t.Fatalf("temporary files left behind: %v", ents)
	}
}

func newImporter(t *testing.T) (*Importer, string, *Hooks) {
	t.Helper()
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, "config", "library.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &Hooks{}
	return New(Config{TVRoot: filepath.Join(dir, "tv"), MovieRoot: filepath.Join(dir, "movies"), Mode: ModeHardlink}, st, h), dir, h
}

func TestImportEpisodeWithSubtitlesAndHooks(t *testing.T) {
	im, dir, hooks := newImporter(t)
	var events []Event
	hooks.AfterImport(func(_ context.Context, e Event) { events = append(events, e) })

	dl := filepath.Join(dir, "dl", "The.Show.S01.1080p.WEB-DL-GRP")
	write(t, filepath.Join(dl, "The.Show.S01E02.1080p.WEB-DL-GRP.mkv"), "ep2")
	write(t, filepath.Join(dl, "The.Show.S01E02.1080p.WEB-DL-GRP.en.srt"), "subs")
	write(t, filepath.Join(dl, "The.Show.S01E02.1080p.WEB-DL-GRP.en.forced.srt"), "forced")
	var files []SourceFile
	for _, n := range []string{"The.Show.S01E02.1080p.WEB-DL-GRP.mkv", "The.Show.S01E02.1080p.WEB-DL-GRP.en.srt", "The.Show.S01E02.1080p.WEB-DL-GRP.en.forced.srt"} {
		files = append(files, SourceFile{Rel: "The.Show.S01.1080p.WEB-DL-GRP/" + n, Path: filepath.Join(dl, n)})
	}
	ti := showTitle(8)
	ps, _ := PlanTV(files, "The.Show.S01.1080p.WEB-DL-GRP", ti)
	if len(ps) != 1 {
		t.Fatalf("plan %+v", ps)
	}
	res, err := im.Import(context.Background(), Request{Title: ti, Placement: ps[0], Release: "The.Show.S01.1080p.WEB-DL-GRP", Hash: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "tv", "The Show (2020)", "Season 01", "The Show - S01E02")
	if res.Path != base+".mkv" || res.Method != "hardlink" {
		t.Fatalf("result %+v", res)
	}
	for _, p := range []string{base + ".mkv", base + ".en.srt", base + ".en.forced.srt"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if len(events) != 1 || events[0].Path != res.Path || len(events[0].Subtitles) != 2 || events[0].Hash != "abc" {
		t.Fatalf("events %+v", events)
	}
	if !im.Store().HasEpisode(ti.Key(), 1, 2) {
		t.Fatal("store does not know the import")
	}
	// The store survives a restart.
	st2, err := OpenStore(filepath.Join(dir, "config", "library.json"))
	if err != nil || len(st2.Files(ti.Key())) != 1 {
		t.Fatalf("reopened store: %v %v", err, st2.Files(ti.Key()))
	}

	// Same file again: nothing to do.
	res, err = im.Import(context.Background(), Request{Title: ti, Placement: ps[0], Release: "x"})
	if err != nil || !res.Skipped {
		t.Fatalf("reimport: %+v %v", res, err)
	}
}

func TestImportUpgradeAndDowngrade(t *testing.T) {
	im, dir, _ := newImporter(t)
	ti := showTitle(8)
	place := func(name, body string, replace bool) (Result, error) {
		p := filepath.Join(dir, "dl", name)
		write(t, p, body)
		pl := Placement{Video: SourceFile{Rel: name, Path: p}, Season: 1, Episodes: []int{1}}
		return im.Import(context.Background(), Request{Title: ti, Placement: pl, Release: strings.TrimSuffix(name, ".mkv"), Replace: replace})
	}
	if _, err := place("The.Show.S01E01.720p.HDTV.mkv", "720", false); err != nil {
		t.Fatal(err)
	}
	// An old sidecar next to the 720p file goes with it on upgrade.
	old := filepath.Join(dir, "tv", "The Show (2020)", "Season 01", "The Show - S01E01")
	write(t, old+".en.srt", "old subs")

	res, err := place("The.Show.S01E01.1080p.WEB-DL.mp4", "1080", false)
	if err != nil || res.Skipped || len(res.Replaced) != 1 {
		t.Fatalf("upgrade: %+v %v", res, err)
	}
	if _, err := os.Stat(old + ".mkv"); !os.IsNotExist(err) {
		t.Fatal("old video still there")
	}
	if _, err := os.Stat(old + ".en.srt"); !os.IsNotExist(err) {
		t.Fatal("old subtitle still there")
	}
	if b, _ := os.ReadFile(old + ".mp4"); string(b) != "1080" {
		t.Fatalf("new file %q", b)
	}

	res, err = place("The.Show.S01E01.480p.DVD.mkv", "480", false)
	if err != nil || !res.Skipped {
		t.Fatalf("downgrade should be skipped: %+v %v", res, err)
	}
	// Explicitly chosen: replaces even though it is worse.
	res, err = place("The.Show.S01E01.720p.WEBRip.mkv", "720b", true)
	if err != nil || res.Skipped {
		t.Fatalf("explicit replace: %+v %v", res, err)
	}
	if got := im.Existing(ti, 1, []int{1}); len(got) != 1 || !strings.HasSuffix(got[0], "S01E01.mkv") {
		t.Fatalf("existing after replace %v", got)
	}
	if n := len(im.Store().Files(ti.Key())); n != 1 {
		t.Fatalf("store has %d records, want 1", n)
	}
}

func TestImportKeepsUnknownAndMultiEpisodeFiles(t *testing.T) {
	im, dir, _ := newImporter(t)
	ti := showTitle(8)
	season := filepath.Join(dir, "tv", "The Show (2020)", "Season 01")
	write(t, filepath.Join(season, "The Show - S01E01.mkv"), "hand placed")
	write(t, filepath.Join(season, "The Show - S01E02-E03.mkv"), "double")

	src := filepath.Join(dir, "dl", "The.Show.S01E01.1080p.WEB-DL.mkv")
	write(t, src, "new")
	pl := Placement{Video: SourceFile{Rel: filepath.Base(src), Path: src}, Season: 1, Episodes: []int{1}}
	res, err := im.Import(context.Background(), Request{Title: ti, Placement: pl, Release: "The.Show.S01E01.1080p.WEB-DL"})
	if err != nil || !res.Skipped || !strings.Contains(res.Reason, "unknown") {
		t.Fatalf("unknown quality: %+v %v", res, err)
	}

	src2 := filepath.Join(dir, "dl", "The.Show.S01E02.1080p.WEB-DL.mkv")
	write(t, src2, "new2")
	pl2 := Placement{Video: SourceFile{Rel: filepath.Base(src2), Path: src2}, Season: 1, Episodes: []int{2}}
	res, err = im.Import(context.Background(), Request{Title: ti, Placement: pl2, Release: "x", Replace: true})
	if err != nil || !res.Skipped || !strings.Contains(res.Reason, "S01E03") {
		t.Fatalf("multi-episode: %+v %v", res, err)
	}
}

func TestImportMovieAndBeforeHook(t *testing.T) {
	im, dir, hooks := newImporter(t)
	block := true
	hooks.BeforeImport(func(_ context.Context, c Candidate) error {
		if block {
			return ErrRetryLater
		}
		return nil
	})
	src := filepath.Join(dir, "dl", "Inception.2010.1080p.BluRay.mkv")
	write(t, src, "movie")
	ti := Title{Kind: "movie", Title: "Inception", Year: 2010, TmdbID: 27205}
	pl := Placement{Video: SourceFile{Rel: filepath.Base(src), Path: src}}
	if _, err := im.Import(context.Background(), Request{Title: ti, Placement: pl}); !errors.Is(err, ErrRetryLater) {
		t.Fatalf("want retry, got %v", err)
	}
	block = false
	res, err := im.Import(context.Background(), Request{Title: ti, Placement: pl})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "movies", "Inception (2010)", "Inception (2010).mkv"); res.Path != want {
		t.Fatalf("path %s, want %s", res.Path, want)
	}
}
