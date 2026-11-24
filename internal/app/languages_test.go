package app

import (
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
}
