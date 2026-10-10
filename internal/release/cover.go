package release

import "sort"

// Episode is the minimum the coverage logic needs to know about an episode.
type Episode struct {
	ID       int  `json:"id"`
	Season   int  `json:"season"`
	Number   int  `json:"number"`
	Absolute int  `json:"absolute,omitempty"`
	HasFile  bool `json:"hasFile"`
	Aired    bool `json:"aired"`
}

// Show describes a series for matching purposes.
type Show struct {
	Titles   []string // canonical title first, then aliases
	Year     int
	Anime    bool
	Episodes []Episode
}

// TitleMatches reports whether a parsed release title names this show (or
// movie). Comparison is on normalised titles, tolerant of a trailing year
// and of "Title Year" vs "Title (Year)".
func TitleMatches(parsed string, titles []string, year int) bool {
	p := Normalize(parsed)
	if p == "" {
		return false
	}
	for _, t := range titles {
		n := Normalize(t)
		if n == "" {
			continue
		}
		if p == n {
			return true
		}
		// "Severance 2022" style suffix that the parser kept in the title.
		if year > 0 && (p == n+" "+itoa(year) || n == p+" "+itoa(year)) {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Covers returns the IDs of the show's episodes that a release contains.
// Specials (season 0) are only covered when the release names them.
func Covers(i Info, s Show) []int {
	var out []int
	switch {
	case i.Complete:
		for _, e := range s.Episodes {
			if e.Season > 0 {
				out = append(out, e.ID)
			}
		}
	case len(i.Absolute) > 0 && len(i.Episodes) == 0:
		want := make(map[int]bool, len(i.Absolute))
		for _, a := range i.Absolute {
			want[a] = true
		}
		hasAbs := false
		for _, e := range s.Episodes {
			if e.Absolute > 0 {
				hasAbs = true
				break
			}
		}
		for _, e := range s.Episodes {
			if hasAbs && e.Absolute > 0 && want[e.Absolute] {
				out = append(out, e.ID)
			} else if !hasAbs && e.Season == 1 && want[e.Number] {
				out = append(out, e.ID)
			}
		}
	case len(i.Seasons) > 0 && len(i.Episodes) > 0:
		want := make(map[int]bool, len(i.Episodes))
		for _, n := range i.Episodes {
			want[n] = true
		}
		for _, e := range s.Episodes {
			if e.Season == i.Seasons[0] && want[e.Number] {
				out = append(out, e.ID)
			}
		}
	case len(i.Seasons) > 0:
		want := make(map[int]bool, len(i.Seasons))
		for _, n := range i.Seasons {
			want[n] = true
		}
		for _, e := range s.Episodes {
			if want[e.Season] {
				out = append(out, e.ID)
			}
		}
	}
	sort.Ints(out)
	return out
}

// Kind classifies what a release is relative to its show.
type Kind string

const (
	KindEpisode     Kind = "episode"      // one episode
	KindMultiEp     Kind = "multi"        // a few episodes in one file/release
	KindSeason      Kind = "season"       // one full season
	KindMultiSeason Kind = "multi-season" // several seasons
	KindComplete    Kind = "complete"     // whole show
	KindMovie       Kind = "movie"
	KindUnknown     Kind = "unknown"
)

// Classify names the shape of a TV release.
func Classify(i Info) Kind {
	switch {
	case i.Complete:
		return KindComplete
	case len(i.Seasons) > 1:
		return KindMultiSeason
	case i.FullSeason:
		return KindSeason
	case len(i.Episodes) == 1 || len(i.Absolute) == 1:
		return KindEpisode
	case len(i.Episodes) > 1 || len(i.Absolute) > 1:
		return KindMultiEp
	}
	return KindUnknown
}
