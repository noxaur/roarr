package verify

import "strings"

// Languages are compared as ISO 639-2/T codes ("eng", "fra", "deu").
// Stream tags use 639-2/B ("fre", "ger"), 639-1 ("en") or, rarely, a name;
// release titles use names and scene shorthands.
var langCodes = map[string]string{
	"en": "eng", "eng": "eng", "english": "eng",
	"fr": "fra", "fra": "fra", "fre": "fra", "french": "fra",
	"de": "deu", "deu": "deu", "ger": "deu", "german": "deu", "deutsch": "deu",
	"es": "spa", "spa": "spa", "spanish": "spa", "castellano": "spa", "latino": "spa", "esp": "spa",
	"it": "ita", "ita": "ita", "italian": "ita",
	"ru": "rus", "rus": "rus", "russian": "rus",
	"pt": "por", "por": "por", "portuguese": "por",
	"pl": "pol", "pol": "pol", "polish": "pol",
	"nl": "nld", "nld": "nld", "dut": "nld", "dutch": "nld", "flemish": "nld",
	"sv": "swe", "swe": "swe", "swedish": "swe",
	"da": "dan", "dan": "dan", "danish": "dan",
	"no": "nor", "nor": "nor", "nob": "nor", "nno": "nor", "norwegian": "nor",
	"fi": "fin", "fin": "fin", "finnish": "fin",
	"hi": "hin", "hin": "hin", "hindi": "hin",
	"ta": "tam", "tam": "tam", "tamil": "tam",
	"te": "tel", "tel": "tel", "telugu": "tel",
	"ko": "kor", "kor": "kor", "korean": "kor",
	"zh": "zho", "zho": "zho", "chi": "zho", "chinese": "zho", "mandarin": "zho", "cantonese": "zho", "chs": "zho", "cht": "zho",
	"ja": "jpn", "jpn": "jpn", "jap": "jpn", "japanese": "jpn",
	"tr": "tur", "tur": "tur", "turkish": "tur",
	"hu": "hun", "hun": "hun", "hungarian": "hun",
	"cs": "ces", "ces": "ces", "cze": "ces", "czech": "ces",
	"el": "ell", "ell": "ell", "gre": "ell", "greek": "ell",
	"ar": "ara", "ara": "ara", "arabic": "ara",
	"he": "heb", "heb": "heb", "hebrew": "heb",
	"uk": "ukr", "ukr": "ukr", "ukrainian": "ukr",
}

// langNames turns a code back into a word for messages.
var langNames = map[string]string{
	"eng": "English", "fra": "French", "deu": "German", "spa": "Spanish", "ita": "Italian", "rus": "Russian",
	"por": "Portuguese", "pol": "Polish", "nld": "Dutch", "swe": "Swedish", "dan": "Danish", "nor": "Norwegian",
	"fin": "Finnish", "hin": "Hindi", "tam": "Tamil", "tel": "Telugu", "kor": "Korean", "zho": "Chinese",
	"jpn": "Japanese", "tur": "Turkish", "hun": "Hungarian", "ces": "Czech", "ell": "Greek", "ara": "Arabic",
	"heb": "Hebrew", "ukr": "Ukrainian",
}

// NormLang maps a language tag or name to its ISO 639-2/T code. Unknown and
// undetermined tags ("und", "", "zxx") return "".
func NormLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_"); i > 0 { // "en-US", "pt_BR"
		s = s[:i]
	}
	return langCodes[s]
}

// LangName is a readable name for a code ("eng" -> "English").
func LangName(code string) string {
	if n, ok := langNames[code]; ok {
		return n
	}
	if code == "" {
		return "untagged"
	}
	return code
}
