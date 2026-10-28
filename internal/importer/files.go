package importer

import (
	"path"
	"regexp"
	"strings"

	"github.com/noxaur/roarr/internal/release"
)

// SourceFile is one file of a finished (or finishing) download.
type SourceFile struct {
	// Rel is the path inside the torrent, with forward slashes
	// ("Show.S01.1080p/Show.S01E02.1080p.mkv").
	Rel string `json:"rel"`
	// Path is where roarr can read the file (after path mapping).
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// FileClass is what a download file is to the importer.
type FileClass string

const (
	ClassVideo    FileClass = "video"
	ClassSubtitle FileClass = "subtitle"
	ClassSample   FileClass = "sample"
	ClassExtra    FileClass = "extra"
	ClassOther    FileClass = "other" // nfo, txt, images, archives
)

var videoExts = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".m4v": true, ".ts": true, ".m2ts": true, ".wmv": true, ".mov": true, ".webm": true, ".mpg": true, ".mpeg": true}

var subtitleExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".idx": true, ".vtt": true, ".sup": true, ".smi": true}

// IsVideo reports whether name has a video extension.
func IsVideo(name string) bool { return videoExts[strings.ToLower(path.Ext(name))] }

// IsSubtitle reports whether name has a subtitle extension.
func IsSubtitle(name string) bool { return subtitleExts[strings.ToLower(path.Ext(name))] }

var (
	sampleRe = regexp.MustCompile(`(?i)(^|[.\-_ \[(])sample([.\-_ \])]|$)`)
	extraRe  = regexp.MustCompile(`(?i)(^|[.\-_ ])(featurettes?|trailers?|behind[.\-_ ]the[.\-_ ]scenes|deleted[.\-_ ]scenes?|making[.\-_ ]of|interviews?|bloopers|gag[.\-_ ]reel|extras|bonus|promo)([.\-_ ]|$)`)
)

// extraDirs are folder names that only ever hold bonus material.
var extraDirs = map[string]bool{
	"extras": true, "extra": true, "featurettes": true, "featurette": true, "behind the scenes": true,
	"deleted scenes": true, "interviews": true, "scenes": true, "shorts": true, "trailers": true,
	"bonus": true, "other": true, "bts": true,
}

// Classify sorts a download file into video, subtitle, sample, extra or
// other, from its path inside the torrent.
func Classify(rel string) FileClass {
	rel = strings.TrimSuffix(strings.ReplaceAll(rel, `\`, "/"), ".!qB")
	base := path.Base(rel)
	stem := strings.TrimSuffix(base, path.Ext(base))
	dirs := strings.Split(path.Dir(rel), "/")
	sub := IsSubtitle(base)
	if !sub && !IsVideo(base) {
		return ClassOther
	}
	for _, d := range dirs {
		ld := strings.ToLower(d)
		if ld == "sample" || ld == "samples" {
			return ClassSample
		}
		if extraDirs[ld] {
			if sub {
				return ClassOther
			}
			return ClassExtra
		}
	}
	if sub {
		return ClassSubtitle
	}
	if sampleRe.MatchString(stem) || release.Parse(base).Sample {
		return ClassSample
	}
	if extraRe.MatchString(stem) {
		return ClassExtra
	}
	return ClassVideo
}

// SubtitleInfo is what a subtitle's file name says about it.
type SubtitleInfo struct {
	Lang   string `json:"lang,omitempty"` // ISO 639-1 where known ("en"), else ""
	Forced bool   `json:"forced,omitempty"`
	SDH    bool   `json:"sdh,omitempty"`
}

// languages maps codes and names found in subtitle file names to ISO 639-1.
var languages = map[string]string{}

func init() {
	for code, names := range map[string][]string{
		"en": {"en", "eng", "english"}, "es": {"es", "spa", "spanish", "espanol", "español", "castellano", "latino"},
		"fr": {"fr", "fre", "fra", "french", "francais", "français", "vff", "vfq"}, "de": {"de", "ger", "deu", "german", "deutsch"},
		"it": {"it", "ita", "italian", "italiano"}, "pt": {"pt", "por", "portuguese", "português", "brazilian", "ptbr", "pt-br"},
		"nl": {"nl", "dut", "nld", "dutch", "nederlands", "flemish"}, "sv": {"sv", "swe", "swedish", "svenska"},
		"no": {"no", "nor", "nob", "nb", "norwegian", "norsk"}, "da": {"da", "dan", "danish", "dansk"},
		"fi": {"fi", "fin", "finnish", "suomi"}, "is": {"is", "ice", "isl", "icelandic"},
		"pl": {"pl", "pol", "polish", "polski"}, "cs": {"cs", "cze", "ces", "czech"}, "sk": {"sk", "slo", "slk", "slovak"},
		"hu": {"hu", "hun", "hungarian", "magyar"}, "ro": {"ro", "rum", "ron", "romanian"}, "bg": {"bg", "bul", "bulgarian"},
		"el": {"el", "gre", "ell", "greek"}, "tr": {"tr", "tur", "turkish"}, "ru": {"ru", "rus", "russian"},
		"uk": {"uk", "ukr", "ukrainian"}, "hr": {"hr", "hrv", "croatian"}, "sr": {"sr", "srp", "scc", "serbian"},
		"sl": {"sl", "slv", "slovenian"}, "et": {"et", "est", "estonian"}, "lv": {"lv", "lav", "latvian"},
		"lt": {"lt", "lit", "lithuanian"}, "ar": {"ar", "ara", "arabic"}, "he": {"he", "heb", "hebrew"},
		"fa": {"fa", "per", "fas", "persian", "farsi"}, "hi": {"hin", "hindi"}, "th": {"th", "tha", "thai"},
		"vi": {"vi", "vie", "vietnamese"}, "id": {"id", "ind", "indonesian"}, "ms": {"ms", "may", "msa", "malay"},
		"zh": {"zh", "chi", "zho", "chinese", "chs", "cht", "mandarin", "cantonese"}, "ja": {"ja", "jpn", "japanese"},
		"ko": {"ko", "kor", "korean"}, "ca": {"ca", "cat", "catalan"}, "eu": {"eu", "baq", "eus", "basque"},
		"gl": {"gl", "glg", "galician"},
	} {
		for _, n := range names {
			languages[n] = code
		}
	}
}

// ParseSubtitle reads language and forced/SDH flags from a subtitle path.
// It understands "Movie.2020.en.forced.srt", "Show.S01E02.eng.sdh.srt" and
// the "Subs/<release>/3_English.srt" layout. videoStem is the name of the
// video the subtitle belongs to, without extension, when known: tokens
// after it are trusted fully. Without it, two- and three-letter codes are
// only trusted near the end and never as the first word, so "It" or "No"
// at the start of a title is not taken for a language.
func ParseSubtitle(rel, videoStem string) SubtitleInfo {
	base := path.Base(strings.ReplaceAll(rel, `\`, "/"))
	stem := strings.TrimSuffix(base, path.Ext(base))
	trustAll := false
	if videoStem != "" && len(stem) > len(videoStem) && strings.EqualFold(stem[:len(videoStem)], videoStem) {
		stem, trustAll = stem[len(videoStem):], true
	}
	toks := strings.FieldsFunc(strings.ToLower(stem), func(r rune) bool {
		return r == '.' || r == '_' || r == ' ' || r == '[' || r == ']' || r == '(' || r == ')' || r == ','
	})
	var si SubtitleInfo
	// Walk from the end: language and flags trail the name.
	for i := len(toks) - 1; i >= 0; i-- {
		t := strings.Trim(toks[i], "-")
		trusted := trustAll || len(toks) == 1 || (i > 0 && len(toks)-i <= 3)
		switch t {
		case "forced", "foreign":
			si.Forced = true
			continue
		case "sdh", "cc", "hoh":
			si.SDH = true
			continue
		case "hi":
			if trusted {
				si.SDH = true
				continue
			}
		}
		if si.Lang != "" {
			continue
		}
		if code, ok := languages[t]; ok && (len(t) > 3 || trusted) {
			si.Lang = code
		}
	}
	return si
}
