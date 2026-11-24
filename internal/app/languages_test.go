package app

import (
	"os"
	"reflect"
	"testing"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
)

// Languages picked for one show survive a restart, steer its scoring and
// leave other titles on the default.
func TestTitleLanguages(t *testing.T) {
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Settings: store, langs: openTitleLanguages(dir)}
	d := testShow()
	seeders := 20
	french := arr.SearchResult{Title: "The.Expanse.S02E01.FRENCH.1080p.WEB.h264-X", Size: 2e9, Seeders: &seeders}
	if !newAnnotator(Target{Kind: "tv", Series: d}, store.Get().Prefs, a.Languages).annotate(french, nil).Rejected {
		t.Fatal("French release allowed with the English default")
	}
	if err := a.SetLanguages("tv", d.TvdbID, "audio", []string{"french", "Japanese"}); err != nil {
		t.Fatal(err)
	}
	a = &App{Settings: store, langs: openTitleLanguages(dir)}
	if l := a.Languages("tv", d.TvdbID); !l.CustomAudio || !reflect.DeepEqual(l.AudioLanguages, []string{"French", "Japanese"}) || l.CustomSubtitles {
		t.Fatalf("languages = %+v", l)
	}
	if rel := newAnnotator(Target{Kind: "tv", Series: d}, store.Get().Prefs, a.Languages).annotate(french, nil); rel.Rejected {
		t.Fatalf("French release rejected with French picked: %v", rel.Reasons)
	}
	if l := a.Languages("tv", d.TvdbID+1); l.CustomAudio || !reflect.DeepEqual(l.AudioLanguages, []string{"English"}) {
		t.Fatalf("other show: %+v", l)
	}
	if err := a.SetLanguages("tv", d.TvdbID, "audio", []string{"Klingon"}); err == nil {
		t.Fatal("unknown language accepted")
	}

	// Subtitles are picked on their own; [] (none needed) survives a restart.
	vostfr := arr.SearchResult{Title: "The.Expanse.S02E01.VOSTFR.1080p.WEB.h264-X", Size: 2e9, Seeders: &seeders}
	if err := a.SetLanguages("tv", d.TvdbID, "subtitles", []string{"English"}); err != nil {
		t.Fatal(err)
	}
	if !newAnnotator(Target{Kind: "tv", Series: d}, store.Get().Prefs, a.Languages).annotate(vostfr, nil).Rejected {
		t.Fatal("French-subtitled release allowed with English subtitles picked")
	}
	// The verifier's parser sees English in SUBENGLISH, so search keeps it.
	both := arr.SearchResult{Title: "The.Expanse.S02E01.1080p.WEB.x264.FRENCHSUBS.SUBENGLISH-GRP", Size: 2e9, Seeders: &seeders}
	if rel := newAnnotator(Target{Kind: "tv", Series: d}, store.Get().Prefs, a.Languages).annotate(both, nil); rel.Rejected {
		t.Fatalf("release with English subtitles rejected: %v", rel.Reasons)
	}
	if err := a.SetLanguages("tv", d.TvdbID, "subtitles", []string{}); err != nil {
		t.Fatal(err)
	}
	a = &App{Settings: store, langs: openTitleLanguages(dir)}
	if l := a.Languages("tv", d.TvdbID); !l.CustomSubtitles || len(l.SubtitleLanguages) != 0 || l.SubtitleLanguages == nil {
		t.Fatalf("empty subtitle pick after restart: %+v", l)
	}

	if err := a.SetLanguages("tv", d.TvdbID, "audio", nil); err != nil {
		t.Fatal(err)
	}
	if l := a.Languages("tv", d.TvdbID); l.CustomAudio || !l.CustomSubtitles {
		t.Fatalf("audio reset: %+v", l)
	}

	// A pick that cannot be saved does not take effect.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := a.SetLanguages("tv", d.TvdbID, "audio", []string{"German"}); err == nil {
		t.Fatal("save into a read-only folder succeeded")
	}
	if l := a.Languages("tv", d.TvdbID); l.CustomAudio {
		t.Fatalf("failed save changed the rules: %+v", l)
	}
}
