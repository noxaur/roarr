package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/verify"
)

// Languages are the audio and subtitle languages a title is grabbed and
// verified in.
type Languages struct {
	AudioLanguages []string `json:"audioLanguages"`
	// CustomAudio is true when the audio languages were picked for this
	// title rather than taken from the default.
	CustomAudio       bool     `json:"customAudio,omitempty"`
	SubtitleLanguages []string `json:"subtitleLanguages"`
	CustomSubtitles   bool     `json:"customSubtitles,omitempty"`
}

func (l Languages) apply(p release.Prefs) release.Prefs {
	p.Languages, p.SubtitleLanguages = l.AudioLanguages, l.SubtitleLanguages
	return p
}

// titlePick is one title's entry in languages.json. null keeps the
// default; [] is a pick of its own (any audio, no subtitles needed).
type titlePick struct {
	Audio     []string `json:"audio"`
	Subtitles []string `json:"subtitles"`
}

// titleLanguages are the languages picked for single titles
// (languages.json), over the defaults in Prefs. A show is keyed by its TVDB
// id and a movie by its TMDB id, the ids every source of a title (page,
// *arr queue, native job) knows.
type titleLanguages struct {
	path string
	mu   sync.Mutex
	m    map[string]titlePick
}

func openTitleLanguages(dir string) *titleLanguages {
	t := &titleLanguages{m: map[string]titlePick{}}
	if dir == "" {
		return t
	}
	t.path = filepath.Join(dir, "languages.json")
	if b, err := os.ReadFile(t.path); err == nil {
		_ = json.Unmarshal(b, &t.m)
	}
	return t
}

func languageKey(kind string, id int) string {
	t := importer.Title{Kind: kind}
	if kind == "tv" {
		t.TvdbID = id
	} else {
		t.TmdbID = id
	}
	return t.Key()
}

// Languages returns the languages for a title (kind tv with a TVDB id,
// movie with a TMDB id).
func (a *App) Languages(kind string, id int) Languages {
	p := a.Settings.Get().Prefs
	out := Languages{AudioLanguages: p.Languages, SubtitleLanguages: p.SubtitleLanguages}
	if id == 0 || a.langs == nil {
		return out
	}
	a.langs.mu.Lock()
	pick, ok := a.langs.m[languageKey(kind, id)]
	a.langs.mu.Unlock()
	if ok && pick.Audio != nil {
		out.AudioLanguages, out.CustomAudio = pick.Audio, true
	}
	if ok && pick.Subtitles != nil {
		out.SubtitleLanguages, out.CustomSubtitles = pick.Subtitles, true
	}
	return out
}

// SetLanguages picks the audio or subtitle languages (which) of one title;
// nil goes back to the default. Names are checked against the languages
// roarr knows.
func (a *App) SetLanguages(kind string, id int, which string, langs []string) error {
	if id <= 0 || kind != "tv" && kind != "movie" {
		return fmt.Errorf("no such title %s %d", kind, id)
	}
	if which != "audio" && which != "subtitles" {
		return fmt.Errorf("unknown language list %q", which)
	}
	var clean []string
	if langs != nil {
		clean = []string{}
	}
	for _, l := range langs {
		code := verify.NormLang(l)
		if code == "" {
			return fmt.Errorf("unknown language %q", l)
		}
		if n := verify.LangName(code); !containsStr(clean, n) {
			clean = append(clean, n)
		}
	}
	t := a.langs
	t.mu.Lock()
	defer t.mu.Unlock()
	key := languageKey(kind, id)
	next := make(map[string]titlePick, len(t.m)+1)
	for k, v := range t.m {
		next[k] = v
	}
	pick := next[key]
	if which == "audio" {
		pick.Audio = clean
	} else {
		pick.Subtitles = clean
	}
	if pick.Audio == nil && pick.Subtitles == nil {
		delete(next, key)
	} else {
		next[key] = pick
	}
	// The pick takes effect only once it is saved, so an error leaves the
	// rules as they were.
	if t.path != "" {
		b, err := json.MarshalIndent(next, "", " ")
		if err != nil {
			return err
		}
		tmp := t.path + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, t.path); err != nil {
			return err
		}
	}
	t.m = next
	return nil
}
