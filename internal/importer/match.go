package importer

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/noxaur/roarr/internal/release"
)

// Title is what the importer knows about a show or movie. It comes from
// TMDB (through Seerr) or from Sonarr/Radarr when they are still around.
type Title struct {
	Kind     string    `json:"kind"` // tv | movie
	Title    string    `json:"title"`
	Year     int       `json:"year,omitempty"`
	TmdbID   int       `json:"tmdbId,omitempty"`
	TvdbID   int       `json:"tvdbId,omitempty"`
	ImdbID   string    `json:"imdbId,omitempty"`
	Aliases  []string  `json:"aliases,omitempty"`
	Anime    bool      `json:"anime,omitempty"`
	Episodes []Episode `json:"episodes,omitempty"`
}

// Episode is one episode in a title's episode list.
type Episode struct {
	Season   int    `json:"season"`
	Number   int    `json:"number"`
	Absolute int    `json:"absolute,omitempty"`
	Title    string `json:"title,omitempty"`
	AirDate  string `json:"airDate,omitempty"`
}

// Key identifies a title in the store: "tv:tmdb:1399", "movie:tmdb:27205",
// with TVDB/IMDb/name fallbacks when TMDB is unknown.
func (t Title) Key() string {
	switch {
	case t.TmdbID != 0:
		return t.Kind + ":tmdb:" + strconv.Itoa(t.TmdbID)
	case t.TvdbID != 0:
		return t.Kind + ":tvdb:" + strconv.Itoa(t.TvdbID)
	case t.ImdbID != "":
		return t.Kind + ":imdb:" + t.ImdbID
	}
	return fmt.Sprintf("%s:name:%s:%d", t.Kind, release.Normalize(t.Title), t.Year)
}

// EpisodeID is the id an episode gets in the release.Show used for
// matching: season*10000 + number.
func EpisodeID(season, number int) int { return season*10000 + number }

// Show converts the title into the release package's matching model.
// Absolute numbers are filled in from the episode order when the metadata
// has none (TMDB does not number anime absolutely).
func (t Title) Show() release.Show {
	s := release.Show{Titles: append([]string{t.Title}, t.Aliases...), Year: t.Year, Anime: t.Anime}
	eps := append([]Episode(nil), t.Episodes...)
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].Season != eps[j].Season {
			return eps[i].Season < eps[j].Season
		}
		return eps[i].Number < eps[j].Number
	})
	hasAbs := false
	for _, e := range eps {
		hasAbs = hasAbs || e.Absolute > 0
	}
	abs := 0
	for _, e := range eps {
		a := e.Absolute
		if e.Season > 0 {
			abs++
			if !hasAbs {
				a = abs
			}
		}
		s.Episodes = append(s.Episodes, release.Episode{ID: EpisodeID(e.Season, e.Number), Season: e.Season, Number: e.Number, Absolute: a, Aired: true})
	}
	return s
}

// Placement is one video file and where its episodes are.
type Placement struct {
	Video     SourceFile    `json:"video"`
	Season    int           `json:"season,omitempty"`
	Episodes  []int         `json:"episodes,omitempty"` // empty for movies
	Subtitles []SubtitleRef `json:"subtitles,omitempty"`
	// Unverified is true when the file names an episode the metadata does
	// not list (yet); it is placed by the numbers in its name.
	Unverified bool `json:"unverified,omitempty"`
}

// SubtitleRef is a subtitle file that belongs to a placement's video.
type SubtitleRef struct {
	File SourceFile   `json:"file"`
	Info SubtitleInfo `json:"info"`
}

// Skip is a file the importer will not place, and why.
type Skip struct {
	File   SourceFile `json:"file"`
	Reason string     `json:"reason"`
}

// PlanTV maps the files of a download to episodes of t. releaseTitle is the
// torrent name; it supplies the season for files that only say "E05".
// Samples, extras, files that match no episode and non-media files are
// returned as skips. Two files claiming the same episode keep the larger.
func PlanTV(files []SourceFile, releaseTitle string, t Title) ([]Placement, []Skip) {
	show := t.Show()
	pack := release.Parse(releaseTitle)
	var out []Placement
	var skips []Skip
	var subs []SourceFile
	for _, f := range files {
		switch Classify(f.Rel) {
		case ClassSubtitle:
			subs = append(subs, f)
			continue
		case ClassSample:
			skips = append(skips, Skip{f, "sample"})
			continue
		case ClassExtra:
			skips = append(skips, Skip{f, "extra"})
			continue
		case ClassOther:
			continue
		}
		season, eps, verified := episodesOf(f.Rel, pack, show, t.Anime)
		if len(eps) == 0 {
			skips = append(skips, Skip{f, "matches no episode of " + t.Title})
			continue
		}
		out = append(out, Placement{Video: f, Season: season, Episodes: eps, Unverified: !verified})
	}
	out, dup := dedupe(out)
	skips = append(skips, dup...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Episodes[0] < out[j].Episodes[0]
	})
	attachSubtitles(out, subs, pack, show, t.Anime)
	return out, skips
}

// dedupe keeps one file per episode: the larger one (a proper pack rarely
// carries two, but a mislabelled extra can claim a real episode's number).
func dedupe(ps []Placement) ([]Placement, []Skip) {
	best := map[int]int{} // episode id -> index into ps
	drop := map[int]bool{}
	for i, p := range ps {
		for _, e := range p.Episodes {
			id := EpisodeID(p.Season, e)
			if j, ok := best[id]; ok && j != i {
				loser := i
				if ps[i].Video.Size > ps[j].Video.Size {
					loser = j
					best[id] = i
				}
				drop[loser] = true
				continue
			}
			best[id] = i
		}
	}
	var out []Placement
	var skips []Skip
	for i, p := range ps {
		if drop[i] {
			skips = append(skips, Skip{p.Video, "another file in the download has the same episode"})
			continue
		}
		out = append(out, p)
	}
	return out, skips
}

// episodesOf works out which episodes a file in a TV download holds. It
// tries the file name, then the parent folders ("Season 2/05.mkv"), then
// the pack's season. verified is false when the episode is taken from the
// name although the metadata does not list it.
func episodesOf(rel string, pack release.Info, show release.Show, anime bool) (season int, eps []int, verified bool) {
	rel = strings.TrimSuffix(strings.ReplaceAll(rel, `\`, "/"), ".!qB")
	base := path.Base(rel)
	info := release.Parse(base)
	// Folder season: the nearest parent that names exactly one season.
	dirSeason := -1
	for d := path.Dir(rel); d != "." && d != "/" && d != ""; d = path.Dir(d) {
		di := release.Parse(path.Base(d))
		if len(di.Seasons) == 1 {
			dirSeason = di.Seasons[0]
			break
		}
	}
	if dirSeason < 0 && len(pack.Seasons) == 1 {
		dirSeason = pack.Seasons[0]
	}
	switch {
	case len(info.Seasons) > 0 && len(info.Episodes) > 0:
	case len(info.Absolute) > 0 && !anime && dirSeason >= 0:
		// "Season 2/Show - 05.mkv": a number inside a known season.
		info.Seasons, info.Episodes, info.Absolute = []int{dirSeason}, info.Absolute, nil
	case len(info.Absolute) > 0:
	case dirSeason >= 0:
		if n := episodeOnly(base); n > 0 {
			info.Seasons, info.Episodes = []int{dirSeason}, []int{n}
		} else if n := leadingNumber(base); n > 0 {
			info.Seasons, info.Episodes = []int{dirSeason}, []int{n}
		}
	}
	if len(info.Episodes) == 0 && len(info.Absolute) == 0 {
		return 0, nil, false // a file never stands for a whole season
	}
	info.Complete, info.FullSeason = false, false
	ids := release.Covers(info, show)
	if len(ids) == 0 {
		if len(info.Seasons) > 0 && len(info.Episodes) > 0 {
			return info.Seasons[0], append([]int(nil), info.Episodes...), false
		}
		return 0, nil, false
	}
	season = ids[0] / 10000
	for _, id := range ids {
		if id/10000 == season {
			eps = append(eps, id%10000)
		}
	}
	return season, eps, true
}

// episodeOnly finds a lone "E05" / "Ep05" / "Episode 5" in a name.
func episodeOnly(name string) int {
	l := strings.ToLower(name)
	for _, prefix := range []string{"episode", "ep", "e"} {
		for i := 0; i+len(prefix) < len(l); i++ {
			if !strings.HasPrefix(l[i:], prefix) || (i > 0 && l[i-1] >= 'a' && l[i-1] <= 'z') {
				continue
			}
			k := i + len(prefix)
			for k < len(l) && (l[k] == ' ' || l[k] == '.' || l[k] == '_') && prefix != "e" {
				k++
			}
			n, digits := 0, 0
			for k < len(l) && l[k] >= '0' && l[k] <= '9' && digits < 4 {
				n = n*10 + int(l[k]-'0')
				k++
				digits++
			}
			if digits > 0 && (k == len(l) || l[k] < '0' || l[k] > '9') {
				return n
			}
		}
	}
	return 0
}

// leadingNumber reads "01 - Pilot.mkv" / "05.mkv" style names: one to
// three digits at the start, followed by a separator or the extension.
func leadingNumber(name string) int {
	n, k := 0, 0
	for k < len(name) && k < 3 && name[k] >= '0' && name[k] <= '9' {
		n = n*10 + int(name[k]-'0')
		k++
	}
	if k == 0 || k == len(name) {
		return 0
	}
	switch name[k] {
	case ' ', '.', '-', '_':
		return n
	}
	return 0
}

// PlanMovie picks the main video of a movie download: the largest video
// that is not a sample or extra. Other videos are skipped as extras.
func PlanMovie(files []SourceFile, releaseTitle string, t Title) ([]Placement, []Skip) {
	var main *SourceFile
	var skips []Skip
	var subs, videos []SourceFile
	for _, f := range files {
		switch Classify(f.Rel) {
		case ClassSubtitle:
			subs = append(subs, f)
		case ClassSample:
			skips = append(skips, Skip{f, "sample"})
		case ClassExtra:
			skips = append(skips, Skip{f, "extra"})
		case ClassVideo:
			videos = append(videos, f)
		}
	}
	for i := range videos {
		if main == nil || videos[i].Size > main.Size {
			main = &videos[i]
		}
	}
	if main == nil {
		return nil, skips
	}
	for _, v := range videos {
		if v.Rel != main.Rel {
			skips = append(skips, Skip{v, "extra"})
		}
	}
	out := []Placement{{Video: *main}}
	attachSubtitles(out, subs, release.Info{}, release.Show{}, false)
	return out, skips
}

// attachSubtitles gives each subtitle to the video it belongs to:
//  1. its name starts with the video's name ("Show.S01E02.en.srt"),
//  2. it sits in a folder named after the video ("Subs/Show.S01E02/2_English.srt"),
//  3. it names the same episodes,
//  4. the download has a single video.
//
// Anything else is dropped rather than guessed.
func attachSubtitles(ps []Placement, subs []SourceFile, pack release.Info, show release.Show, anime bool) {
	if len(ps) == 0 {
		return
	}
	stem := func(rel string) string {
		b := path.Base(strings.ReplaceAll(rel, `\`, "/"))
		return strings.TrimSuffix(b, path.Ext(b))
	}
	for _, s := range subs {
		rel := strings.ReplaceAll(s.Rel, `\`, "/")
		sStem := strings.ToLower(stem(rel))
		owner, owned := -1, ""
		for i, p := range ps {
			vs := strings.ToLower(stem(p.Video.Rel))
			if strings.HasPrefix(sStem, vs+".") || strings.HasPrefix(sStem, vs+"_") || sStem == vs {
				owner, owned = i, stem(p.Video.Rel)
				break
			}
			if strings.EqualFold(path.Base(path.Dir(rel)), vs) {
				owner = i
				break
			}
		}
		if owner < 0 && show.Episodes != nil {
			if season, eps, _ := episodesOf(rel, pack, show, anime); len(eps) > 0 {
				for i, p := range ps {
					if p.Season == season && sameInts(p.Episodes, eps) {
						owner = i
						break
					}
				}
			}
		}
		if owner < 0 && len(ps) == 1 {
			owner = 0
		}
		if owner < 0 {
			continue
		}
		ps[owner].Subtitles = append(ps[owner].Subtitles, SubtitleRef{File: s, Info: ParseSubtitle(rel, owned)})
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
