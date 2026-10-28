// Package release parses indexer release titles and decides how good a
// release is for a given request.
//
// The parser is deliberately allocation-light and regexp-driven: it runs on
// every search result the moment an indexer answers, so it must stay well
// under a microsecond-scale budget per title (see parse_test.go benchmarks).
package release

import (
	"regexp"
	"strconv"
	"strings"
)

// Info is everything roarr understands about a release title.
type Info struct {
	Title      string `json:"title"` // show / movie name portion, cleaned
	Year       int    `json:"year,omitempty"`
	Seasons    []int  `json:"seasons,omitempty"`
	Episodes   []int  `json:"episodes,omitempty"` // within Seasons[0]
	Absolute   []int  `json:"absolute,omitempty"` // anime absolute numbering
	FullSeason bool   `json:"fullSeason,omitempty"`
	Complete   bool   `json:"complete,omitempty"` // "complete series" without explicit seasons
	Special    bool   `json:"special,omitempty"`

	Resolution int    `json:"resolution,omitempty"` // 2160, 1080, 720, 576, 480
	Source     Source `json:"source,omitempty"`
	Codec      string `json:"codec,omitempty"` // x265, x264, av1, xvid
	HDR        string `json:"hdr,omitempty"`   // DV, HDR10+, HDR
	Audio      string `json:"audio,omitempty"` // Atmos, TrueHD, DTS-HD MA, DTS, DDP, DD, AAC, FLAC, Opus
	Channels   string `json:"channels,omitempty"`
	BitDepth10 bool   `json:"tenBit,omitempty"`
	Service    string `json:"service,omitempty"` // AMZN, NF, ATVP ...
	Group      string `json:"group,omitempty"`

	Languages  []string `json:"languages,omitempty"` // explicitly tagged audio languages
	Multi      bool     `json:"multi,omitempty"`     // MULTi / dual audio
	DualAudio  bool     `json:"dualAudio,omitempty"`
	Subtitles  []string `json:"subtitles,omitempty"` // explicitly tagged subtitle languages ("Multi" for multi-sub)
	Subbed     bool     `json:"subbed,omitempty"`
	HardSubbed bool     `json:"hardSubbed,omitempty"`

	Proper   bool `json:"proper,omitempty"`
	Repack   bool `json:"repack,omitempty"`
	Remux    bool `json:"remux,omitempty"`
	Extended bool `json:"extended,omitempty"`
	Sample   bool `json:"sample,omitempty"`
	ThreeD   bool `json:"threeD,omitempty"`
}

// Source is the capture source of a release, ordered roughly by quality.
type Source string

const (
	SourceUnknown Source = ""
	SourceCam     Source = "CAM"
	SourceTS      Source = "TS"
	SourceScreen  Source = "SCR"
	SourceDVD     Source = "DVD"
	SourceHDTV    Source = "HDTV"
	SourceWEBRip  Source = "WEBRip"
	SourceWEB     Source = "WEB"
	SourceWEBDL   Source = "WEB-DL"
	SourceBluRay  Source = "BluRay"
	SourceRemux   Source = "Remux"
)

// IsPack reports whether the release covers more than one episode.
func (i *Info) IsPack() bool {
	return i.FullSeason || i.Complete || len(i.Seasons) > 1 || len(i.Episodes) > 1 || len(i.Absolute) > 1
}

var (
	// Separators are normalised to spaces before matching.
	sepRe = regexp.MustCompile(`[._]+`)

	// S01E02, S01E02E03, S01E02-E05, S01E02-05, S1E2
	sxeRe = regexp.MustCompile(`(?i)\bS(\d{1,2})[ -]?E(\d{1,4})((?:[ -]?-?[ -]?E?\d{1,4})*)\b`)
	// 1x02, 1x02-1x03
	nxnRe = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{2,3})(?:-(?:\d{1,2}x)?(\d{2,3}))?\b`)
	// S01-S06, S01-06, S01 - S06, S01.S02 (runs) — must be checked before single S01
	multiSRe = regexp.MustCompile(`(?i)\bS(\d{1,2})(?:\s*(?:-|~|to)\s*S?(\d{1,2})|((?:\s+S\d{1,2})+))\b`)
	// Season 1-3, Seasons 1 to 5, Season 1 & 2, Season 1
	seasonWordRe = regexp.MustCompile(`(?i)\bSeasons?\s*(\d{1,2})(?:\s*(?:-|~|to|&|and|\+)\s*(\d{1,2}))?\b`)
	// lone S01 (season pack)
	singleSRe  = regexp.MustCompile(`(?i)\bS(\d{1,2})\b`)
	completeRe = regexp.MustCompile(`(?i)\b(complete(?:\s+series)?|integrale|the\s+complete|all\s+seasons)\b`)

	// Anime: "[Group] Title - 05 (1080p)", "Title - 05v2", "Title - 01-28", "Title 01~12"
	animeEpRe    = regexp.MustCompile(`(?:\s-\s|\s#|\sE|\sEp\s?)(\d{1,4})(?:v\d)?(?:\s*[-~]\s*(\d{1,4})(?:v\d)?)?(?:\s|$|\[|\()`)
	animeBatchRe = regexp.MustCompile(`[\[(](\d{1,4})\s*[-~]\s*(\d{1,4})[\])]`)
	leadingGroup = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*`)
	hashSuffix   = regexp.MustCompile(`\[[0-9A-Fa-f]{8}\]`)

	resRe        = regexp.MustCompile(`(?i)\b(2160p|4k|uhd|1080[pi]|720p|576[pi]|480[pi]|3840x2160|1920x1080|1280x720)\b`)
	trailingGrp  = regexp.MustCompile(`-\s?([A-Za-z0-9][A-Za-z0-9&@$!]{1,24})(?:\s*\[[^\]]*\])*\s*(?:\.(?:mkv|mp4|avi))?$`)
	bracketGroup = regexp.MustCompile(`\[([A-Za-z0-9.&\- ]{2,24})\]\s*$`)
	sizeHints    = regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\s?(?:GB|MB|GiB|MiB)\b`)
)

type flagRule struct {
	re  *regexp.Regexp
	set func(*Info)
}

var flagRules = []flagRule{
	{regexp.MustCompile(`(?i)\bremux\b`), func(i *Info) { i.Remux = true }},
	{regexp.MustCompile(`(?i)\b(blu-?ray|bdrip|brrip|bd(?:25|50)?|bdremux|uhd\s?bd)\b`), func(i *Info) { setSource(i, SourceBluRay) }},
	{regexp.MustCompile(`(?i)\bweb-?dl\b|\bwebdl\b`), func(i *Info) { setSource(i, SourceWEBDL) }},
	{regexp.MustCompile(`(?i)\bweb-?rip\b`), func(i *Info) { setSource(i, SourceWEBRip) }},
	{regexp.MustCompile(`(?i)\bweb\b`), func(i *Info) { setSource(i, SourceWEB) }},
	{regexp.MustCompile(`(?i)\b(hdtv|pdtv|sdtv|dsr|tvrip|hdtvrip)\b`), func(i *Info) { setSource(i, SourceHDTV) }},
	{regexp.MustCompile(`(?i)\b(dvd-?rip|dvdr|dvd5|dvd9|dvd)\b`), func(i *Info) { setSource(i, SourceDVD) }},
	{regexp.MustCompile(`(?i)\b(hdcam|cam-?rip|camrip|cam)\b`), func(i *Info) { setSource(i, SourceCam) }},
	{regexp.MustCompile(`(?i)\b(telesync|hdts|ts-?rip|pdvd|ts)\b`), func(i *Info) { setSource(i, SourceTS) }},
	{regexp.MustCompile(`(?i)\b(dvdscr|screener|scr)\b`), func(i *Info) { setSource(i, SourceScreen) }},

	{regexp.MustCompile(`(?i)\b(x ?265|h ?265|hevc)\b`), func(i *Info) { setCodec(i, "x265") }},
	{regexp.MustCompile(`(?i)\b(x ?264|h ?264|avc)\b`), func(i *Info) { setCodec(i, "x264") }},
	{regexp.MustCompile(`(?i)\bav1\b`), func(i *Info) { setCodec(i, "av1") }},
	{regexp.MustCompile(`(?i)\b(xvid|divx)\b`), func(i *Info) { setCodec(i, "xvid") }},

	{regexp.MustCompile(`(?i)\b(dv|dovi|dolby\s?vision)\b`), func(i *Info) { i.HDR = "DV" }},
	{regexp.MustCompile(`(?i)\bhdr10(\+|plus)\b`), func(i *Info) { setHDR(i, "HDR10+") }},
	{regexp.MustCompile(`(?i)\bhdr(10)?\b`), func(i *Info) { setHDR(i, "HDR") }},
	{regexp.MustCompile(`(?i)\b10-?bit\b|\bhi10p?\b`), func(i *Info) { i.BitDepth10 = true }},

	{regexp.MustCompile(`(?i)\batmos\b`), func(i *Info) { setAudio(i, "Atmos") }},
	{regexp.MustCompile(`(?i)\btrue-?hd\b`), func(i *Info) { setAudio(i, "TrueHD") }},
	{regexp.MustCompile(`(?i)\bdts-?(hd|x)(\s?ma)?\b`), func(i *Info) { setAudio(i, "DTS-HD MA") }},
	{regexp.MustCompile(`(?i)\bdts\b`), func(i *Info) { setAudio(i, "DTS") }},
	{regexp.MustCompile(`(?i)\b(ddp|dd\+|e-?ac-?3)`), func(i *Info) { setAudio(i, "DDP") }},
	{regexp.MustCompile(`(?i)\b(dd|ac-?3|dolby\s?digital)(\d\.\d)?\b`), func(i *Info) { setAudio(i, "DD") }},
	{regexp.MustCompile(`(?i)\bflac\b`), func(i *Info) { setAudio(i, "FLAC") }},
	{regexp.MustCompile(`(?i)\bopus\b`), func(i *Info) { setAudio(i, "Opus") }},
	{regexp.MustCompile(`(?i)\baac(\d\.\d)?\b`), func(i *Info) { setAudio(i, "AAC") }},

	{regexp.MustCompile(`(?i)\bproper\b`), func(i *Info) { i.Proper = true }},
	{regexp.MustCompile(`(?i)\b(repack|rerip)\d?\b`), func(i *Info) { i.Repack = true }},
	{regexp.MustCompile(`(?i)\b(extended|uncut|directors?\s?cut)\b`), func(i *Info) { i.Extended = true }},
	{regexp.MustCompile(`(?i)\bsample\b`), func(i *Info) { i.Sample = true }},
	{regexp.MustCompile(`(?i)\b(3d|hsbs|h-sbs|half-ou)\b`), func(i *Info) { i.ThreeD = true }},
	{regexp.MustCompile(`(?i)\b(multi(?:[\s-]?audio)?|multi-?lang)\b`), func(i *Info) { i.Multi = true }},
	{regexp.MustCompile(`(?i)\bdual[\s-]?(audio|lang(uage)?)?\b|\b(eng|en)[\s+&-](jap?|jpn|ja)\b|\b(jap?|jpn|ja)[\s+&-](eng|en)\b`), func(i *Info) { i.DualAudio, i.Multi = true, true }},
	{regexp.MustCompile(`(?i)\b(hc|hardsub(bed|s)?|korsub)\b`), func(i *Info) { i.HardSubbed = true }},
	{regexp.MustCompile(`(?i)\b(subbed|multi-?subs?|eng-?subs?|softsubs?|subs?)\b`), func(i *Info) { i.Subbed = true }},
}

var services = map[string]string{
	"amzn": "AMZN", "amazon": "AMZN", "nf": "NF", "netflix": "NF", "atvp": "ATVP", "dsnp": "DSNP", "dsny": "DSNP",
	"hmax": "MAX", "max": "MAX", "hulu": "HULU", "pcok": "PCOK", "pmtp": "PMTP", "it": "", "cr": "CR", "crunchyroll": "CR",
	"stan": "STAN", "sho": "SHO", "hbo": "HBO", "red": "RED", "cc": "CC", "bcore": "BCORE", "mubi": "MUBI", "ip": "iP",
}

var languageTokens = map[string]string{
	"english": "English", "eng": "English",
	"french": "French", "vff": "French", "vfq": "French", "truefrench": "French",
	"german": "German", "ger": "German", "deutsch": "German",
	"spanish": "Spanish", "esp": "Spanish", "castellano": "Spanish", "latino": "Spanish",
	"italian": "Italian", "ita": "Italian",
	"russian": "Russian", "rus": "Russian",
	"portuguese": "Portuguese", "por": "Portuguese", "dublado": "Portuguese",
	"polish": "Polish", "pl": "Polish", "pldub": "Polish",
	"dutch": "Dutch", "nl": "Dutch", "flemish": "Dutch",
	"swedish": "Swedish", "swe": "Swedish", "danish": "Danish", "norwegian": "Norwegian", "nor": "Norwegian", "finnish": "Finnish",
	"hindi": "Hindi", "tamil": "Tamil", "telugu": "Telugu", "korean": "Korean", "kor": "Korean",
	"chinese": "Chinese", "chs": "Chinese", "cht": "Chinese", "mandarin": "Chinese",
	"japanese": "Japanese", "turkish": "Turkish", "tur": "Turkish", "hungarian": "Hungarian", "hun": "Hungarian",
	"czech": "Czech", "cz": "Czech", "greek": "Greek", "arabic": "Arabic", "hebrew": "Hebrew", "ukrainian": "Ukrainian", "ukr": "Ukrainian",
}

var sourceRank = map[Source]int{SourceCam: 0, SourceTS: 1, SourceScreen: 2, SourceDVD: 3, SourceHDTV: 4, SourceWEBRip: 5, SourceWEB: 6, SourceWEBDL: 7, SourceBluRay: 8, SourceRemux: 9}

func setSource(i *Info, s Source) {
	if i.Source == SourceUnknown || sourceRank[s] > sourceRank[i.Source] && s != SourceCam && s != SourceTS && s != SourceScreen {
		// "WEB" is a weaker signal than WEB-DL/WEBRip; TS/CAM only win when nothing else matched.
		if s == SourceWEB && (i.Source == SourceWEBDL || i.Source == SourceWEBRip) {
			return
		}
		i.Source = s
	}
}

func setCodec(i *Info, c string) {
	if i.Codec == "" {
		i.Codec = c
	}
}

func setHDR(i *Info, h string) {
	if i.HDR == "" {
		i.HDR = h
	}
}

func setAudio(i *Info, a string) {
	if i.Audio == "" {
		i.Audio = a
	}
}

var (
	channelsRe = regexp.MustCompile(`(?:^|[^0-9])([1-9])\.([01])(?:[^0-9]|$)`)
	multiAnyRe = regexp.MustCompile(`(?i)\bmulti`)
	multiSubRe = regexp.MustCompile(`(?i)\bmulti(?:ple)?[\s-]?sub`)

	// "Eng Sub", "ENGSUBS", "NLSUBS", "Multi-Subs", "Multiple Subtitle",
	// "VOSTFR" (French subs over original audio), "VOSTA" (English subs).
	subLangRe = regexp.MustCompile(`(?i)\b(eng|en|english|nl|dutch|swe|swedish|dk|dan|danish|nor|norwegian|fin|finnish|ger|german|ita|italian|spa|spanish|esp|por|pt|ptbr|portuguese|fr|fre|french|kor|korean|chs|cht|chi|chinese|jp|jpn|japanese|ara|arabic|heb|hebrew|pl|pol|polish|rus|russian|tur|turkish|multi|multiple)[\s.-]?(?:subs?|subtitles?|subbed)\b`)
	vostRe    = regexp.MustCompile(`(?i)\bvost(fr|a)?\b`)
)

var subLangNames = map[string]string{
	"eng": "English", "en": "English", "english": "English", "nl": "Dutch", "dutch": "Dutch",
	"swe": "Swedish", "swedish": "Swedish", "dk": "Danish", "dan": "Danish", "danish": "Danish",
	"nor": "Norwegian", "norwegian": "Norwegian", "fin": "Finnish", "finnish": "Finnish",
	"ger": "German", "german": "German", "ita": "Italian", "italian": "Italian",
	"spa": "Spanish", "spanish": "Spanish", "esp": "Spanish", "por": "Portuguese", "pt": "Portuguese", "ptbr": "Portuguese", "portuguese": "Portuguese",
	"fr": "French", "fre": "French", "french": "French", "kor": "Korean", "korean": "Korean",
	"chs": "Chinese", "cht": "Chinese", "chi": "Chinese", "chinese": "Chinese", "jp": "Japanese", "jpn": "Japanese", "japanese": "Japanese",
	"ara": "Arabic", "arabic": "Arabic", "heb": "Hebrew", "hebrew": "Hebrew", "pl": "Polish", "pol": "Polish", "polish": "Polish",
	"rus": "Russian", "russian": "Russian", "tur": "Turkish", "turkish": "Turkish", "multi": "Multi", "multiple": "Multi",
}

// Parse extracts structured information from a release title.
func Parse(raw string) Info {
	var info Info
	s := strings.TrimSpace(raw)
	for _, ext := range []string{".mkv", ".mp4", ".avi", ".torrent", ".nzb"} {
		if len(s) > len(ext) && strings.EqualFold(s[len(s)-len(ext):], ext) {
			s = s[:len(s)-len(ext)]
		}
	}

	// Leading [Group] is the anime convention.
	animeStyle := false
	if m := leadingGroup.FindStringSubmatch(s); m != nil {
		info.Group = strings.TrimSpace(m[1])
		s = s[len(m[0]):]
		animeStyle = true
	}
	s = hashSuffix.ReplaceAllString(s, " ")

	// Group from a trailing -GROUP before separators are flattened.
	if info.Group == "" {
		if m := trailingGrp.FindStringSubmatch(s); m != nil && !looksLikeTag(m[1]) {
			info.Group = m[1]
		} else if m := bracketGroup.FindStringSubmatch(s); m != nil && !looksLikeTag(m[1]) {
			info.Group = strings.TrimSpace(m[1])
		}
	}

	// Channels need the dot (5.1) so grab them before flattening.
	if m := channelsRe.FindStringSubmatch(s); m != nil {
		info.Channels = m[1] + "." + m[2]
	}

	flat := sepRe.ReplaceAllString(s, " ")
	flat = sizeHints.ReplaceAllString(flat, " ")

	for _, r := range flagRules {
		if r.re.MatchString(flat) {
			r.set(&info)
		}
	}
	if info.Remux {
		info.Source = SourceRemux
	}
	if info.Multi && !info.DualAudio && len(multiAnyRe.FindAllStringIndex(flat, -1)) == len(multiSubRe.FindAllStringIndex(flat, -1)) {
		info.Multi = false // "Multi-Subs" is about subtitles, not audio
	}

	if m := resRe.FindString(flat); m != "" {
		info.Resolution = resolution(m)
	}

	// Subtitle languages: tagged "XX Sub(s)" pairs, blanked out afterwards so
	// "Eng Sub" is not mistaken for English audio.
	langZone := flat
	for _, m := range subLangRe.FindAllStringSubmatch(flat, -1) {
		if lang, ok := subLangNames[strings.ToLower(m[1])]; ok && !contains(info.Subtitles, lang) {
			info.Subtitles = append(info.Subtitles, lang)
		}
	}
	if m := vostRe.FindStringSubmatch(flat); m != nil {
		lang := "French"
		if strings.EqualFold(m[1], "a") {
			lang = "English"
		}
		if !contains(info.Subtitles, lang) {
			info.Subtitles = append(info.Subtitles, lang)
		}
	}
	if len(info.Subtitles) > 0 {
		info.Subbed = true
		langZone = vostRe.ReplaceAllString(subLangRe.ReplaceAllString(flat, " "), " ")
	}

	// Services via a token walk over the whole title.
	titleEnd := len(flat)
	for _, tok := range strings.FieldsFunc(flat, func(r rune) bool { return r == ' ' || r == '-' || r == '[' || r == ']' || r == '(' || r == ')' }) {
		lt := strings.ToLower(tok)
		if sv, ok := services[lt]; ok && sv != "" && info.Service == "" && isUpper(tok) {
			info.Service = sv
		}
	}

	// Episode structure.
	cut := func(idx int) {
		if idx >= 0 && idx < titleEnd {
			titleEnd = idx
		}
	}
	if loc := sxeRe.FindStringSubmatchIndex(flat); loc != nil {
		season := atoi(flat[loc[2]:loc[3]])
		first := atoi(flat[loc[4]:loc[5]])
		info.Seasons = []int{season}
		info.Episodes = []int{first}
		if loc[6] >= 0 && loc[7] > loc[6] {
			info.Episodes = expandEpisodes(first, flat[loc[6]:loc[7]])
		}
		cut(loc[0])
	} else if loc := nxnRe.FindStringSubmatchIndex(flat); loc != nil {
		season := atoi(flat[loc[2]:loc[3]])
		first := atoi(flat[loc[4]:loc[5]])
		info.Seasons = []int{season}
		info.Episodes = []int{first}
		if loc[6] >= 0 {
			info.Episodes = rangeInts(first, atoi(flat[loc[6]:loc[7]]))
		}
		cut(loc[0])
	} else if loc := multiSRe.FindStringSubmatchIndex(flat); loc != nil {
		a := atoi(flat[loc[2]:loc[3]])
		if loc[4] >= 0 {
			info.Seasons = rangeInts(a, atoi(flat[loc[4]:loc[5]]))
		} else {
			info.Seasons = []int{a}
			for _, m := range singleSRe.FindAllStringSubmatch(flat[loc[6]:loc[7]], -1) {
				info.Seasons = append(info.Seasons, atoi(m[1]))
			}
		}
		info.FullSeason = true
		cut(loc[0])
	} else if loc := seasonWordRe.FindStringSubmatchIndex(flat); loc != nil {
		a := atoi(flat[loc[2]:loc[3]])
		if loc[4] >= 0 {
			info.Seasons = rangeInts(a, atoi(flat[loc[4]:loc[5]]))
		} else {
			info.Seasons = []int{a}
		}
		info.FullSeason = true
		cut(loc[0])
		// "Season 1 S01" style duplicates are harmless; anime batch ranges may follow.
	} else if loc := singleSRe.FindStringSubmatchIndex(flat); loc != nil {
		info.Seasons = []int{atoi(flat[loc[2]:loc[3]])}
		info.FullSeason = true
		cut(loc[0])
	}

	if len(info.Seasons) == 0 || animeStyle {
		// Anime absolute numbering.
		if loc := animeBatchRe.FindStringSubmatchIndex(flat); loc != nil {
			a, b := atoi(flat[loc[2]:loc[3]]), atoi(flat[loc[4]:loc[5]])
			if b > a && b-a < 2000 && !isYear(a) {
				info.Absolute = rangeInts(a, b)
				cut(loc[0])
			}
		}
		if len(info.Absolute) == 0 && len(info.Episodes) == 0 {
			if loc := animeEpRe.FindStringSubmatchIndex(flat + " "); loc != nil {
				a := atoi(flat[loc[2]:loc[3]])
				if !isYear(a) && !isResolution(a) {
					if loc[4] >= 0 {
						info.Absolute = rangeInts(a, atoi(flat[loc[4]:loc[5]]))
					} else {
						info.Absolute = []int{a}
					}
					cut(loc[0])
				}
			}
		}
		if (animeStyle || len(info.Absolute) > 0) && strings.Contains(strings.ToLower(flat), "batch") && len(info.Absolute) == 0 && len(info.Seasons) == 0 {
			info.Complete = true
		}
	}

	if len(info.Seasons) > 0 && len(info.Seasons) == 1 && info.Seasons[0] == 0 {
		info.Special = true
	}

	if completeRe.MatchString(flat) && len(info.Episodes) == 0 && len(info.Absolute) == 0 {
		if len(info.Seasons) == 0 {
			info.Complete = true
		}
		if loc := completeRe.FindStringIndex(flat); loc != nil {
			cut(loc[0])
		}
	}

	// Year: the last plausible year before the episode token. A leading
	// number is never the year so titles like "1923" survive.
	if y, at := lastYear(flat[:titleEnd]); y != 0 {
		info.Year = y
		cut(at)
	}
	if loc := resRe.FindStringIndex(flat); loc != nil {
		cut(loc[0])
	}
	info.Title = cleanTitle(flat[:titleEnd])

	// Audio languages: tags after the title, so "The French Dispatch" is not
	// tagged French while "Dune.2021.FRENCH" is.
	zone := langZone
	if titleEnd <= len(zone) {
		zone = zone[titleEnd:]
	}
	for _, tok := range strings.FieldsFunc(zone, func(r rune) bool { return r == ' ' || r == '-' || r == '[' || r == ']' || r == '(' || r == ')' }) {
		lt := strings.ToLower(tok)
		if lang, ok := languageTokens[lt]; ok && (isUpper(tok) || len(tok) > 3) {
			if !contains(info.Languages, lang) {
				info.Languages = append(info.Languages, lang)
			}
		}
	}
	return info
}

// lastYear finds the final standalone 19xx/20xx token that is not the first
// word, returning it and its byte offset.
func lastYear(s string) (int, int) {
	year, at := 0, -1
	start := -1
	for i := 0; i <= len(s); i++ {
		boundary := i == len(s) || s[i] == ' ' || s[i] == '(' || s[i] == ')' || s[i] == '[' || s[i] == ']'
		if !boundary {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && i-start == 4 && start > 0 && strings.TrimSpace(s[:start]) != "" {
			if n := atoi(s[start:i]); isYear(n) && strings.Trim(s[:start], " ([") != "" {
				year, at = n, start
				for at > 0 && (s[at-1] == ' ' || s[at-1] == '(' || s[at-1] == '[') {
					at--
				}
			}
		}
		start = -1
	}
	return year, at
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, " -[](){}")
	return strings.Join(strings.Fields(s), " ")
}

func looksLikeTag(s string) bool {
	l := strings.ToLower(s)
	switch l {
	case "dl", "rip", "web", "hd", "ma", "x", "audio", "sub", "subs", "hdr", "dv", "atmos", "mkv", "mp4", "bit", "10bit", "8bit", "uhd", "remux":
		return true
	}
	if resRe.MatchString(s) {
		return true
	}
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	return false
}

func expandEpisodes(first int, rest string) []int {
	nums := regexp.MustCompile(`\d{1,4}`).FindAllString(rest, -1)
	out := []int{first}
	isRange := strings.Contains(rest, "-")
	prev := first
	for _, n := range nums {
		v := atoi(n)
		if isRange && v > prev && v-prev < 200 {
			for e := prev + 1; e <= v; e++ {
				out = append(out, e)
			}
		} else if v != prev {
			out = append(out, v)
		}
		prev = v
	}
	return out
}

func rangeInts(a, b int) []int {
	if b < a || b-a > 2000 {
		return []int{a}
	}
	out := make([]int, 0, b-a+1)
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}

func resolution(m string) int {
	l := strings.ToLower(m)
	switch {
	case strings.HasPrefix(l, "2160"), l == "4k", l == "uhd", l == "3840x2160":
		return 2160
	case strings.HasPrefix(l, "1080"), l == "1920x1080":
		return 1080
	case strings.HasPrefix(l, "720"), l == "1280x720":
		return 720
	case strings.HasPrefix(l, "576"):
		return 576
	case strings.HasPrefix(l, "480"):
		return 480
	}
	return 0
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func isYear(n int) bool       { return n >= 1920 && n <= 2099 }
func isResolution(n int) bool { return n == 480 || n == 576 || n == 720 || n == 1080 || n == 2160 }

func isUpper(s string) bool {
	return s == strings.ToUpper(s)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Normalize folds a title for fuzzy comparison: lowercase ASCII words, no
// punctuation, no leading article, "and" for "&".
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			space = false
		case r == '&':
			if !space {
				b.WriteByte(' ')
			}
			b.WriteString("and ")
			space = true
		case r == '\'' || r == '’':
			// drop apostrophes: "Frieren's" -> "frierens"
		case r > 127:
			b.WriteRune(foldRune(r))
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimPrefix(out, "the ")
	return out
}

func foldRune(r rune) rune {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å':
		return 'a'
	case 'è', 'é', 'ê', 'ë':
		return 'e'
	case 'ì', 'í', 'î', 'ï':
		return 'i'
	case 'ò', 'ó', 'ô', 'õ', 'ö', 'ø':
		return 'o'
	case 'ù', 'ú', 'û', 'ü':
		return 'u'
	case 'ñ':
		return 'n'
	case 'ç':
		return 'c'
	}
	return r
}
