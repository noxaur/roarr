package release

import (
	"fmt"
	"math"
	"strings"
)

// Prefs are the user's taste. Defaults aim at "good 1080p WEB, compact,
// well seeded, in a language I understand".
type Prefs struct {
	// Resolutions in order of preference. Anything not listed is rejected.
	Resolutions []int `json:"resolutions"`
	// PreferHEVC favours x265/HEVC (smaller files at the same quality).
	PreferHEVC bool `json:"preferHevc"`
	// AllowRemux keeps full-disc remuxes in play (they are huge).
	AllowRemux bool `json:"allowRemux"`
	// AllowHDR keeps HDR/DV releases in play.
	AllowHDR bool `json:"allowHdr"`
	// MaxGBPerHour caps size per hour of runtime (0 = no cap).
	MaxGBPerHour float64 `json:"maxGbPerHour"`
	// MinSeeders rejects torrents with fewer seeders.
	MinSeeders int `json:"minSeeders"`
	// Language is the audio language the user wants ("English").
	// Releases explicitly tagged with only other languages are rejected.
	Language string `json:"language"`
	// PreferDualAudio boosts dual-audio/MULTi releases (anime viewers).
	PreferDualAudio bool `json:"preferDualAudio"`
	// PreferredGroups get a bonus; BlockedTerms reject on substring match.
	PreferredGroups []string `json:"preferredGroups"`
	BlockedTerms    []string `json:"blockedTerms"`
}

// DefaultPrefs returns sensible defaults.
func DefaultPrefs() Prefs {
	return Prefs{
		Resolutions:  []int{1080, 2160, 720},
		PreferHEVC:   false,
		AllowRemux:   false,
		AllowHDR:     true,
		MaxGBPerHour: 12,
		MinSeeders:   1,
		Language:     "English",
	}
}

// Candidate is a release as seen by the scorer.
type Candidate struct {
	Title    string
	Size     int64
	Seeders  int
	Protocol string // torrent | usenet
	AgeHours float64
	Info     Info
}

// Verdict is the scorer's opinion on one candidate.
type Verdict struct {
	Score    float64  `json:"score"`
	Rejected bool     `json:"rejected"`
	Reasons  []string `json:"reasons,omitempty"` // why it was rejected
	Notes    []string `json:"notes,omitempty"`   // what moved the score
}

// Score rates a candidate. runtimeMin is the total runtime the release is
// expected to contain (episodes covered × episode runtime) so that size
// checks are fair for packs and singles alike.
func Score(c Candidate, p Prefs, runtimeMin float64) Verdict {
	var v Verdict
	add := func(pts float64, note string) {
		v.Score += pts
		if note != "" {
			v.Notes = append(v.Notes, fmt.Sprintf("%+.0f %s", pts, note))
		}
	}
	reject := func(reason string) {
		v.Rejected = true
		v.Reasons = append(v.Reasons, reason)
	}
	i := c.Info

	// Resolution preference: first choice 40, then -12 per rank.
	if i.Resolution == 0 {
		add(5, "unknown resolution")
	} else {
		rank := -1
		for idx, r := range p.Resolutions {
			if r == i.Resolution {
				rank = idx
			}
		}
		if rank < 0 {
			reject(fmt.Sprintf("%dp not wanted", i.Resolution))
		} else {
			add(40-float64(rank)*12, fmt.Sprintf("%dp", i.Resolution))
		}
	}

	switch i.Source {
	case SourceCam, SourceTS, SourceScreen:
		reject("cam/telesync")
	case SourceRemux:
		if !p.AllowRemux {
			reject("remux")
		} else {
			add(18, "remux")
		}
	case SourceBluRay:
		add(18, "BluRay")
	case SourceWEBDL:
		add(17, "WEB-DL")
	case SourceWEB:
		add(15, "WEB")
	case SourceWEBRip:
		add(12, "WEBRip")
	case SourceHDTV:
		add(6, "HDTV")
	case SourceDVD:
		add(3, "DVD")
	}

	switch i.Codec {
	case "x265":
		if p.PreferHEVC {
			add(10, "HEVC")
		}
	case "av1":
		if p.PreferHEVC {
			add(6, "AV1")
		}
	case "xvid":
		add(-15, "XviD")
	}
	if i.HDR != "" && !p.AllowHDR {
		reject("HDR")
	}
	if i.ThreeD {
		reject("3D")
	}
	if i.Sample {
		reject("sample")
	}
	if i.HardSubbed {
		add(-20, "hardsubs")
	}
	if i.Proper || i.Repack {
		add(4, "proper/repack")
	}

	// Language: reject foreign-only releases.
	if p.Language != "" && len(i.Languages) > 0 && !i.Multi && !i.DualAudio && !contains(i.Languages, p.Language) {
		reject(strings.Join(i.Languages, "/") + " audio")
	}
	if p.PreferDualAudio && (i.DualAudio || i.Multi) {
		add(15, "dual audio")
	}

	// Size sanity per hour of content.
	if runtimeMin > 0 && c.Size > 0 {
		gbh := float64(c.Size) / 1e9 / (runtimeMin / 60)
		minGBH := 0.08
		if i.Resolution >= 1080 {
			minGBH = 0.25
		}
		switch {
		case gbh < minGBH:
			reject(fmt.Sprintf("suspiciously small (%.2f GB/h)", gbh))
		case p.MaxGBPerHour > 0 && gbh > p.MaxGBPerHour:
			reject(fmt.Sprintf("too large (%.1f GB/h)", gbh))
		case p.MaxGBPerHour > 0:
			// Mild preference for leaner files inside the allowed band.
			add(-6*gbh/p.MaxGBPerHour, "size")
		}
	}

	// Seeders matter most for speed: log-scaled, saturating around 200.
	if c.Protocol != "usenet" {
		if c.Seeders < p.MinSeeders {
			reject(fmt.Sprintf("%d seeders", c.Seeders))
		}
		add(math.Min(20, 20*math.Log2(1+float64(c.Seeders))/math.Log2(201)), "seeders")
	}

	for _, g := range p.PreferredGroups {
		if g != "" && strings.EqualFold(g, i.Group) {
			add(12, "preferred group")
		}
	}
	lt := strings.ToLower(c.Title)
	for _, b := range p.BlockedTerms {
		if b != "" && strings.Contains(lt, strings.ToLower(b)) {
			reject("blocked: " + b)
		}
	}
	v.Score = math.Round(v.Score*10) / 10
	return v
}
