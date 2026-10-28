package verify

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/noxaur/roarr/internal/release"
)

// Claims is what a release title promises about its streams.
type Claims struct {
	Resolution int      `json:"resolution,omitempty"`
	Codec      string   `json:"codec,omitempty"` // x265 | x264 | av1 | xvid
	HDR        string   `json:"hdr,omitempty"`
	TenBit     bool     `json:"tenBit,omitempty"`
	Audio      []string `json:"audio,omitempty"` // ISO 639-2/T codes named as audio
	Multi      bool     `json:"multi,omitempty"` // MULTi / dual audio / DL: two or more audio languages
	Subs       []string `json:"subs,omitempty"`  // subtitle languages named
	AnySubs    bool     `json:"anySubs,omitempty"`
	MultiSubs  bool     `json:"multiSubs,omitempty"`
	HardSubbed bool     `json:"hardSubbed,omitempty"`
}

var (
	tokenSplit = regexp.MustCompile(`[\s._\-\[\]()+,&]+`)
	// Single tokens that name a subtitle language: VOSTFR, SUBFRENCH, ENGSUB,
	// ESub, SUBITA, NLSubs.
	subPrefix = regexp.MustCompile(`(?i)^subs?([a-z]{2,10})$`)
	subSuffix = regexp.MustCompile(`(?i)^([a-z]{1,10})subs?$`)
	// "VOSTFR", "VOST" = original audio with French subtitles.
	vost = regexp.MustCompile(`(?i)^vost(fr)?$`)
)

// Audio-only language tokens from French scene names.
var frenchAudio = map[string]bool{"truefrench": true, "vff": true, "vfq": true, "vfi": true, "vf": true, "vf2": true, "french": true}

// ParseClaims reads a release title.
func ParseClaims(title string) Claims {
	info := release.Parse(title)
	c := Claims{Resolution: info.Resolution, Codec: info.Codec, HDR: info.HDR, TenBit: info.BitDepth10, Multi: info.Multi, HardSubbed: info.HardSubbed}

	toks := tokenSplit.Split(title, -1)
	// Drop a trailing file extension and the group.
	if n := len(toks); n > 0 && videoExts["."+strings.ToLower(toks[n-1])] {
		toks = toks[:n-1]
	}
	low := make([]string, len(toks))
	for i, t := range toks {
		low[i] = strings.ToLower(t)
	}
	isSub := func(i int) bool {
		return i >= 0 && i < len(low) && (low[i] == "sub" || low[i] == "subs" || low[i] == "subtitles" || low[i] == "subbed" || low[i] == "subtitled")
	}
	addSub := func(l string) {
		if l != "" && !containsStr(c.Subs, l) {
			c.Subs = append(c.Subs, l)
		}
	}
	addAudio := func(l string) {
		if l != "" && !containsStr(c.Audio, l) {
			c.Audio = append(c.Audio, l)
		}
	}
	// Words that are also language codes appear in ordinary titles ("It",
	// "No Country for Old Men"); a short code only counts when written in
	// capitals as scene names do, a full name in any case.
	langOf := func(i int) string {
		t := toks[i]
		if len(t) <= 3 && (t != strings.ToUpper(t) || ambiguousCodes[low[i]]) {
			return ""
		}
		return NormLang(t)
	}
	// Language tokens only count after the title: from the first token that
	// is a year, episode marker, resolution or source onwards.
	start := tagStart(low)
	subRun := false // inside "Sub ITA ENG": languages belong to the subtitles
	for i := start; i < len(toks); i++ {
		t := low[i]
		wasRun := subRun
		subRun = false
		switch {
		case vost.MatchString(t):
			addSub("fra")
			continue
		case frenchAudio[t]:
			addAudio("fra")
			if t == "vf2" {
				c.Multi = true
			}
			continue
		case t == "msubs" || t == "multisub" || t == "multisubs" || (t == "multi" && isSub(i+1)):
			c.MultiSubs, c.AnySubs = true, true
			continue
		case t == "dl" && i > 0 && low[i-1] != "web":
			// German scene "GERMAN DL": German plus the original audio.
			c.Multi = true
			continue
		case isSub(i):
			c.AnySubs = true
			subRun = t == "sub" || t == "subs"
			continue
		}
		if m := subPrefix.FindStringSubmatch(t); m != nil && NormLang(m[1]) != "" {
			addSub(NormLang(m[1]))
			continue
		}
		if m := subSuffix.FindStringSubmatch(t); m != nil {
			switch {
			case m[1] == "e" || m[1] == "eng":
				addSub("eng") // ESub, EngSub
				continue
			case m[1] == "multi" || m[1] == "m":
				c.MultiSubs, c.AnySubs = true, true
				continue
			case NormLang(m[1]) != "" && len(m[1]) >= 2:
				addSub(NormLang(m[1]))
				continue
			}
		}
		l := langOf(i)
		if l == "" {
			continue
		}
		// "Sub ITA ENG" and "ENG SUBS" name subtitle languages; in
		// "ITA ENG Sub ITA ENG" the first pair is audio.
		if wasRun || isSub(i+1) && (i+2 >= len(toks) || langOf(i+2) == "") {
			addSub(l)
			subRun = wasRun
			continue
		}
		addAudio(l)
	}
	if len(c.Subs) > 0 {
		c.AnySubs = true
	}
	// "MULTi" without named languages promises two or more audio tracks; with
	// named ones those must be there.
	if len(c.Audio) >= 2 {
		c.Multi = true
	}
	return c
}

// Two-letter codes that are usually something else in a release name
// (country, "hearing impaired", ordinary words).
var ambiguousCodes = map[string]bool{"uk": true, "hi": true, "no": true, "it": true, "he": true, "ar": true, "el": true, "ta": true, "te": true, "ko": true, "cs": true, "da": true}

var tagStartRe = regexp.MustCompile(`^(s\d{1,2}(e\d{1,4})*|\d{1,2}x\d{2,3}|(19|20)\d{2}|2160p|1080[pi]|720p|576[pi]|480[pi]|4k|uhd|web|webrip|webdl|bluray|bdrip|brrip|hdtv|dvdrip|remux|complete|season|multi|dual)$`)

// tagStart returns the index of the first token that is part of the release
// tags rather than the title. A title that is itself a year ("1917") does
// not count as the tag start.
func tagStart(low []string) int {
	for i := 1; i < len(low); i++ {
		if tagStartRe.MatchString(low[i]) {
			return i
		}
	}
	return len(low)
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// resolutionTier maps a picture size to the nominal resolution. Width
// decides for letterboxed films (1920x800 is 1080p); height for narrow
// formats (1440x1080 is 1080p).
func resolutionTier(w, h int) int {
	switch {
	case w >= 3200 || h >= 1800:
		return 2160
	case w >= 1700 || h >= 1000:
		return 1080
	case w >= 1180 || h >= 650:
		return 720
	case h >= 540 || w >= 1000:
		return 576
	case w > 0 || h > 0:
		return 480
	}
	return 0
}

var tiers = []int{480, 576, 720, 1080, 2160}

func tierIndex(r int) int {
	for i, t := range tiers {
		if t == r {
			return i
		}
	}
	return -1
}

var codecNames = map[string][]string{
	"x265": {"hevc", "h265"},
	"x264": {"h264", "avc"},
	"av1":  {"av1"},
	"xvid": {"mpeg4", "msmpeg4v3", "msmpeg4v2"},
}

// CompareClaims checks the claims against probed streams and the sidecar
// subtitle files that belong to the video.
func CompareClaims(cl Claims, p *Probe, sc Sidecars) []Check {
	var out []Check
	v, hasVideo := p.Video()
	if hasVideo && cl.Resolution > 0 && v.Width > 0 {
		got := resolutionTier(v.Width, v.Height)
		c := Check{Name: "resolution", Level: Pass, Reason: fmt.Sprintf("%dp claimed, %dx%d found", cl.Resolution, v.Width, v.Height)}
		if gap := tierIndex(cl.Resolution) - tierIndex(got); gap > 0 {
			c.Level = Warn
			if gap >= 2 {
				c.Level = Fail
			}
			c.Reason = fmt.Sprintf("claims %dp but the video is %dx%d (%dp)", cl.Resolution, v.Width, v.Height, got)
		}
		out = append(out, c)
	}
	if hasVideo && cl.Codec != "" {
		c := Check{Name: "codec", Level: Pass, Reason: fmt.Sprintf("%s claimed, %s found", cl.Codec, v.CodecName)}
		if want, ok := codecNames[cl.Codec]; ok && !containsStr(want, v.CodecName) {
			c.Level, c.Reason = Warn, fmt.Sprintf("claims %s but the video is %s", cl.Codec, v.CodecName)
		}
		out = append(out, c)
	}
	if hasVideo && cl.HDR != "" {
		if hdr, known := v.HDR(); known && !hdr {
			out = append(out, Check{Name: "hdr", Level: Warn, Reason: fmt.Sprintf("claims %s but the video is SDR (%s)", cl.HDR, v.ColorTransfer)})
		} else if hdr {
			out = append(out, Check{Name: "hdr", Level: Pass, Reason: cl.HDR + " claimed, HDR found"})
		}
	}
	if hasVideo && cl.TenBit && v.PixFmt != "" {
		if strings.Contains(v.PixFmt, "10") || strings.Contains(v.PixFmt, "12") {
			out = append(out, Check{Name: "bitdepth", Level: Pass, Reason: "10-bit claimed, " + v.PixFmt + " found"})
		} else {
			out = append(out, Check{Name: "bitdepth", Level: Warn, Reason: "claims 10-bit but the video is " + v.PixFmt})
		}
	}
	out = append(out, compareAudio(cl, p))
	if c, ok := compareSubs(cl, p, sc); ok {
		out = append(out, c)
	}
	return out
}

func compareAudio(cl Claims, p *Probe) Check {
	c := Check{Name: "audio", Level: Pass}
	var tracks []Stream
	for _, a := range p.Kind("audio") {
		if !a.IsCommentary() {
			tracks = append(tracks, a)
		}
	}
	if len(tracks) == 0 {
		if len(p.Kind("audio")) > 0 {
			c.Reason = "only commentary audio"
			c.Level = Warn
			return c
		}
		c.Level, c.Reason = Warn, "no audio track"
		if len(cl.Audio) > 0 || cl.Multi {
			c.Level = Fail
			c.Reason = "no audio track, though the name promises audio languages"
		}
		return c
	}
	langs := map[string]bool{}
	untagged := 0
	for _, a := range tracks {
		if l := a.Lang(); l != "" {
			langs[l] = true
		} else {
			untagged++
		}
	}
	found := make([]string, 0, len(langs))
	for _, a := range tracks {
		if l := a.Lang(); l != "" && !containsStr(found, LangName(l)) {
			found = append(found, LangName(l))
		}
	}
	desc := fmt.Sprintf("%d audio track(s)", len(tracks))
	if len(found) > 0 {
		desc += ": " + strings.Join(found, ", ")
	}
	if untagged > 0 {
		desc += fmt.Sprintf(" (%d untagged)", untagged)
	}
	var missing []string
	for _, l := range cl.Audio {
		if !langs[l] {
			missing = append(missing, LangName(l))
		}
	}
	switch {
	case len(missing) > 0 && untagged == 0:
		c.Level, c.Reason = Fail, "claims "+strings.Join(missing, ", ")+" audio, found "+desc
	case len(missing) > 0:
		c.Level, c.Reason = Warn, "cannot confirm "+strings.Join(missing, ", ")+" audio: "+desc
	case cl.Multi && len(tracks) < 2:
		c.Level, c.Reason = Fail, "claims several audio languages (MULTi/dual), found "+desc
	case cl.Multi && len(langs) < 2 && untagged == 0:
		c.Level, c.Reason = Fail, "claims several audio languages (MULTi/dual), but every track is "+strings.Join(found, ", ")
	case cl.Multi && len(langs) < 2:
		c.Level, c.Reason = Warn, "cannot confirm several audio languages: "+desc
	default:
		c.Reason = desc
	}
	return c
}

// subSource is one subtitle track, embedded or a sidecar file.
type subSource struct {
	lang    string
	forced  bool
	empty   bool
	sidecar string // file name, for sidecars
}

// compareSubs checks subtitle claims against embedded streams and sidecar
// files. A stream's emptiness comes from its frame count (tags or a counted
// read, see Verifier.countSubs; -1 is unknown), a sidecar's from its size.
// A claim only fails when the absence is certain: every track is tagged and
// no subtitle file in the download is left unexplained.
func compareSubs(cl Claims, p *Probe, sc Sidecars) (Check, bool) {
	if !cl.AnySubs && len(cl.Subs) == 0 {
		return Check{}, false
	}
	c := Check{Name: "subtitles", Level: Pass}
	var srcs []subSource
	for _, s := range p.Kind("subtitle") {
		srcs = append(srcs, subSource{lang: s.Lang(), forced: s.Is("forced"), empty: s.Frames() == 0})
	}
	for _, f := range sc.Matched {
		srcs = append(srcs, subSource{lang: f.Lang, forced: f.Forced, empty: f.Size == 0, sidecar: path.Base(f.Name)})
	}
	if len(srcs) == 0 {
		switch {
		case cl.HardSubbed:
			c.Reason = "hardsubbed: subtitles are burned into the picture"
		case sc.Other > 0:
			c.Level, c.Reason = Warn, fmt.Sprintf("claims subtitles; the file has no subtitle track and %d subtitle file(s) in the download could not be matched to it", sc.Other)
		default:
			c.Level, c.Reason = Fail, "claims subtitles but the file has no subtitle track and the download has no subtitle files"
		}
		return c, true
	}
	untagged := 0
	for _, s := range srcs {
		if s.lang == "" {
			untagged++
		}
	}
	unsure := untagged > 0 || sc.Other > 0
	var parts []string
	for _, want := range cl.Subs {
		name := LangName(want)
		var full, empty, forced int
		sidecar := ""
		for _, s := range srcs {
			if s.lang != want {
				continue
			}
			switch {
			case s.empty:
				empty++
			case s.forced:
				forced++
			default:
				full++
				if sidecar == "" {
					sidecar = s.sidecar
				}
			}
		}
		switch {
		case full > 0:
			msg := name + " subtitles present"
			if sidecar != "" {
				msg += " (sidecar " + sidecar + ")"
			}
			if empty > 0 {
				msg += fmt.Sprintf(" (%d empty track ignored)", empty)
			}
			parts = append(parts, msg)
		case forced > 0:
			c.Level = Worse(c.Level, Warn)
			parts = append(parts, fmt.Sprintf("only forced %s subtitles (foreign parts only)", name))
		case unsure:
			c.Level = Worse(c.Level, Warn)
			parts = append(parts, fmt.Sprintf("cannot confirm %s subtitles: %s", name, unsureWhy(untagged, sc.Other)))
		case empty > 0:
			c.Level = Worse(c.Level, Fail)
			parts = append(parts, fmt.Sprintf("the %s subtitle track is empty", name))
		default:
			c.Level = Worse(c.Level, Fail)
			parts = append(parts, fmt.Sprintf("claims %s subtitles but none exist (found %s)", name, subLangList(srcs)))
		}
	}
	nonEmpty, langs := 0, map[string]bool{}
	for _, s := range srcs {
		if !s.empty {
			nonEmpty++
			if s.lang != "" {
				langs[s.lang] = true
			}
		}
	}
	switch {
	case nonEmpty == 0 && sc.Other > 0:
		c.Level = Worse(c.Level, Warn)
		parts = append(parts, "every matched subtitle track is empty; "+unsureWhy(0, sc.Other))
	case nonEmpty == 0:
		c.Level = Worse(c.Level, Fail)
		parts = append(parts, "every subtitle track is empty")
	case cl.MultiSubs && len(langs) < 2 && nonEmpty < 2:
		c.Level = Worse(c.Level, Warn)
		parts = append(parts, "claims multiple subtitle languages, found "+subLangList(srcs))
	case len(cl.Subs) == 0:
		parts = append(parts, fmt.Sprintf("%d subtitle track(s): %s", len(srcs), subLangList(srcs)))
	}
	c.Reason = strings.Join(parts, "; ")
	return c, true
}

func unsureWhy(untagged, other int) string {
	var why []string
	if untagged > 0 {
		why = append(why, fmt.Sprintf("%d track(s) without a language tag", untagged))
	}
	if other > 0 {
		why = append(why, fmt.Sprintf("%d subtitle file(s) not matched to this video", other))
	}
	return strings.Join(why, ", ")
}

func subLangList(srcs []subSource) string {
	var out []string
	for _, s := range srcs {
		n := LangName(s.lang)
		if !containsStr(out, n) {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}
