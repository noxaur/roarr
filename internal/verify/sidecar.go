package verify

import (
	"path"
	"strings"
)

var subExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".sub": true, ".idx": true, ".sup": true, ".smi": true}

var subDirs = map[string]bool{"subs": true, "sub": true, "subtitles": true, "subtitle": true}

// Sidecar is a subtitle file shipped next to a video.
type Sidecar struct {
	Name   string `json:"name"`
	Lang   string `json:"lang,omitempty"` // ISO 639-2/T, "" when the name does not say
	Forced bool   `json:"forced,omitempty"`
	Size   int64  `json:"size"`
}

// Sidecars are the subtitle files of a download relative to one video.
type Sidecars struct {
	Matched []Sidecar // belong to the video
	Other   int       // subtitle files that could not be tied to it
}

// FindSidecars finds subtitle files that belong to primary: in its folder
// with its name as prefix ("Movie.en.srt", "Movie.eng.forced.srt"), or in a
// Subs/ folder, either under the video's name ("Subs/Show.S01E01/2_English.srt")
// or loose when the download holds one video ("Subs/English.srt"). The
// language comes from what follows the video's name.
func FindSidecars(files []File, primary File, videos int) Sidecars {
	var sc Sidecars
	pDir, pBase := splitName(primary.Name)
	for _, f := range files {
		name := strings.TrimSuffix(f.Name, ".!qB")
		if !subExts[strings.ToLower(path.Ext(name))] {
			continue
		}
		dir, base := splitName(name)
		rest, ok := "", false
		switch {
		case dir == pDir && strings.HasPrefix(base, pBase):
			rest, ok = base[len(pBase):], true
		case inSubDir(dir, pDir):
			switch {
			case strings.HasPrefix(base, pBase):
				rest, ok = base[len(pBase):], true
			case hasComponent(dir, pBase):
				rest, ok = base, true
			case videos == 1:
				rest, ok = base, true
			}
		}
		if !ok {
			sc.Other++
			continue
		}
		s := Sidecar{Name: name, Size: f.Size}
		for _, tok := range strings.FieldsFunc(rest, func(r rune) bool { return r < 'a' || r > 'z' }) {
			switch {
			case tok == "forced":
				s.Forced = true
			case s.Lang == "":
				s.Lang = NormLang(tok)
			}
		}
		sc.Matched = append(sc.Matched, s)
	}
	return sc
}

// splitName returns the lowercase folder and file name without extension.
func splitName(name string) (string, string) {
	name = strings.ToLower(name)
	dir, file := path.Split(name)
	return strings.TrimSuffix(dir, "/"), strings.TrimSuffix(file, path.Ext(file))
}

// inSubDir reports a folder at or below root that passes through a Subs
// folder.
func inSubDir(dir, root string) bool {
	if root != "" {
		if !strings.HasPrefix(dir, root+"/") {
			return false
		}
		dir = dir[len(root)+1:]
	}
	for _, d := range strings.Split(dir, "/") {
		if subDirs[d] {
			return true
		}
	}
	return false
}

func hasComponent(dir, name string) bool {
	for _, d := range strings.Split(dir, "/") {
		if d == name {
			return true
		}
	}
	return false
}
