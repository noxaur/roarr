package verify

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/noxaur/roarr/internal/release"
)

// File is one file of a download.
type File struct {
	Name string `json:"name"` // relative to the download, "/"-separated
	Path string `json:"path"` // where roarr can open it ("" when not visible)
	Size int64  `json:"size"`
}

var (
	videoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".ts": true, ".m2ts": true, ".wmv": true, ".mov": true, ".webm": true, ".mpg": true, ".mpeg": true, ".ogv": true, ".flv": true}
	// Nothing in a TV or movie release needs to run.
	dangerExts  = map[string]bool{".exe": true, ".scr": true, ".lnk": true, ".bat": true, ".cmd": true, ".pif": true, ".msi": true, ".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true, ".ps1": true, ".jar": true, ".hta": true, ".cpl": true, ".dll": true, ".apk": true, ".dmg": true, ".sh": true}
	archiveExts = map[string]bool{".rar": true, ".zip": true, ".7z": true, ".tar": true, ".gz": true, ".tgz": true, ".bz2": true, ".xz": true, ".zst": true}
	discExts    = map[string]bool{".iso": true, ".img": true, ".bin": true}
	// split archives: .r00, .r01 ..., .001, .002
	splitArchive = regexp.MustCompile(`(?i)\.(r\d{2}|\d{3})$`)
	sampleWord   = regexp.MustCompile(`(?i)(^|[^a-z])sample([^a-z]|$)`)
	trailerEnd   = regexp.MustCompile(`(?i)(^|[^a-z])(trailer|teaser|promo)\d?$`)
	extraDirs    = map[string]bool{"sample": true, "samples": true, "trailer": true, "trailers": true, "extras": true, "featurettes": true, "behind the scenes": true, "deleted scenes": true, "interviews": true, "shorts": true}
)

// IsExtra reports a sample, trailer or bonus file: by a "sample" word in its
// name, a name ending in "trailer", or an extras folder. Titles such as
// "Trailer Park Boys" are not extras.
func IsExtra(name string) bool {
	parts := strings.Split(name, "/")
	for _, d := range parts[:len(parts)-1] {
		if extraDirs[strings.ToLower(d)] {
			return true
		}
	}
	base := parts[len(parts)-1]
	base = strings.TrimSuffix(base, path.Ext(base))
	return sampleWord.MatchString(base) || trailerEnd.MatchString(base)
}

// CheckPayload judges the file list of a download and returns the video
// files worth probing (no samples or trailers), best first.
func CheckPayload(files []File) (Check, []File) {
	var videos, extras []File
	var danger, archives, discs []string
	for _, f := range files {
		name := strings.TrimSuffix(f.Name, ".!qB")
		ext := strings.ToLower(path.Ext(name))
		switch {
		case strings.EqualFold(path.Base(name), "RARBG_DO_NOT_MIRROR.exe"):
			// A harmless marker in every old RARBG release.
		case dangerExts[ext]:
			danger = append(danger, path.Base(name))
		case videoExts[ext]:
			if IsExtra(name) {
				extras = append(extras, f)
			} else {
				videos = append(videos, f)
			}
		case archiveExts[ext] || splitArchive.MatchString(name):
			archives = append(archives, path.Base(name))
		case discExts[ext]:
			discs = append(discs, path.Base(name))
		}
	}
	c := Check{Name: "payload", Level: Pass}
	switch {
	case len(danger) > 0:
		c.Level, c.Reason = Fail, "contains executable files ("+list(danger)+"); real releases never do"
	case len(videos) == 0 && len(extras) > 0:
		c.Level, c.Reason = Fail, "only a sample or trailer, no full video ("+list(names(extras))+")"
	case len(videos) == 0 && len(archives) > 0:
		c.Level, c.Reason = Fail, "only archives, no video file ("+list(archives)+")"
	case len(videos) == 0 && len(discs) > 0:
		c.Level, c.Reason = Warn, "a disc image, not a video file ("+list(discs)+"); Sonarr/Radarr cannot import it"
	case len(videos) == 0:
		c.Level, c.Reason = Fail, "no video file in the download"
	default:
		c.Reason = fmt.Sprintf("%d video file(s)", len(videos))
		if len(extras) > 0 {
			c.Reason += fmt.Sprintf(", %d sample/extra ignored", len(extras))
		}
	}
	// A "video" much smaller than its siblings is a sample even when the
	// name does not say so; keep only files within 1/20 of the largest.
	var biggest int64
	for _, v := range videos {
		biggest = max(biggest, v.Size)
	}
	kept := videos[:0:0]
	for _, v := range videos {
		if biggest == 0 || v.Size*20 >= biggest {
			kept = append(kept, v)
		}
	}
	return c, kept
}

// PickPrimary chooses the file to examine closely: the first episode of a
// pack (lowest season, then episode; specials last), or the largest file
// for a movie.
func PickPrimary(videos []File, kind string) (File, bool) {
	if len(videos) == 0 {
		return File{}, false
	}
	vs := append([]File(nil), videos...)
	if kind == "movie" {
		sort.SliceStable(vs, func(i, j int) bool { return vs[i].Size > vs[j].Size })
		return vs[0], true
	}
	type key struct{ s, e int }
	keyOf := func(f File) key {
		info := release.Parse(path.Base(f.Name))
		k := key{1 << 20, 1 << 20}
		if len(info.Seasons) > 0 && len(info.Episodes) > 0 {
			k = key{info.Seasons[0], info.Episodes[0]}
			if k.s == 0 {
				k.s = 1 << 19 // specials after regular seasons
			}
		} else if len(info.Absolute) > 0 {
			k = key{1, info.Absolute[0]}
		}
		return k
	}
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := keyOf(vs[i]), keyOf(vs[j])
		if a.s != b.s {
			return a.s < b.s
		}
		if a.e != b.e {
			return a.e < b.e
		}
		return vs[i].Name < vs[j].Name
	})
	return vs[0], true
}

func names(fs []File) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = path.Base(f.Name)
	}
	return out
}

func list(xs []string) string {
	if len(xs) > 3 {
		return strings.Join(xs[:3], ", ") + fmt.Sprintf(" and %d more", len(xs)-3)
	}
	return strings.Join(xs, ", ")
}
