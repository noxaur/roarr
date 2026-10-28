// Package importer is roarr's own import pipeline: it turns finished
// download files into a Jellyfin-friendly library without Sonarr or Radarr.
//
//	Show Name (2019)/Season 01/Show Name - S01E02.mkv
//	Show Name (2019)/Season 01/Show Name - S01E02.en.forced.srt
//	Movie Name (2010)/Movie Name (2010).mkv
//
// The package is free of network calls: callers hand it title metadata and
// file paths, it decides where every file goes, places it (hardlink, copy or
// move), remembers what it placed, and calls the registered hooks.
package importer

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// CleanName makes a title safe as a file or folder name on every common
// filesystem (ext4, NTFS, SMB shares): reserved characters are removed,
// "Title: Subtitle" becomes "Title - Subtitle", and whitespace collapses.
func CleanName(s string) string {
	s = strings.ReplaceAll(s, ": ", " - ")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ':' || r == '/' || r == '\\' || r == '|':
			b.WriteRune('-')
		case r == '<' || r == '>' || r == '"' || r == '?' || r == '*':
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	// Windows refuses names ending in a dot or space.
	out = strings.TrimRight(out, ". ")
	if out == "" {
		return "Unknown"
	}
	return out
}

// TitleWithYear is "Title (Year)", unless the title already carries the year.
func TitleWithYear(title string, year int) string {
	t := CleanName(title)
	if year <= 0 {
		return t
	}
	y := strconv.Itoa(year)
	if strings.HasSuffix(t, "("+y+")") {
		return t
	}
	return t + " (" + y + ")"
}

// SeasonFolder is "Season 01"; specials go to "Season 00", which Jellyfin
// treats as specials.
func SeasonFolder(season int) string { return fmt.Sprintf("Season %02d", season) }

// EpisodeToken is "S01E02", or "S01E02-E03" for a multi-episode file (the
// form Jellyfin recognises as one file holding a range).
func EpisodeToken(season int, episodes []int) string {
	if len(episodes) == 0 {
		return fmt.Sprintf("S%02d", season)
	}
	lo, hi := episodes[0], episodes[0]
	for _, e := range episodes {
		lo, hi = min(lo, e), max(hi, e)
	}
	if lo == hi {
		return fmt.Sprintf("S%02dE%02d", season, lo)
	}
	return fmt.Sprintf("S%02dE%02d-E%02d", season, lo, hi)
}

// EpisodePath returns the library path (without extension) of an episode:
// root/Show (Year)/Season 01/Show - S01E02.
func EpisodePath(root, show string, year, season int, episodes []int) string {
	base := CleanName(show) + " - " + EpisodeToken(season, episodes)
	return filepath.Join(root, TitleWithYear(show, year), SeasonFolder(season), base)
}

// MoviePath returns the library path (without extension) of a movie:
// root/Movie (Year)/Movie (Year).
func MoviePath(root, title string, year int) string {
	name := TitleWithYear(title, year)
	return filepath.Join(root, name, name)
}

// SubtitleName is the sidecar name for a subtitle next to a video whose
// path without extension is videoBase: "<base>.en.forced.srt".
func SubtitleName(videoBase string, s SubtitleInfo, ext string) string {
	parts := []string{videoBase}
	if s.Lang != "" {
		parts = append(parts, s.Lang)
	}
	if s.Forced {
		parts = append(parts, "forced")
	}
	if s.SDH {
		parts = append(parts, "sdh")
	}
	return strings.Join(parts, ".") + strings.ToLower(ext)
}
