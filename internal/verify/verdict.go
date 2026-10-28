// Package verify checks that a finished download is real, watchable media
// and that it is what its release name claims to be.
//
// It runs ffprobe and ffmpeg as external commands (through a Runner, so the
// decision logic is testable without them) and judges three things:
//
//  1. the payload: a video file is present, nothing executable, not only
//     archives, not a sample or trailer, a plausible duration;
//  2. the picture: frames sampled across the file show real, changing
//     images, not black, flat or one static slate;
//  3. the claims: resolution, codec, audio languages and subtitles named in
//     the release title exist in the file's streams.
package verify

import "time"

// Level grades one check or a whole verdict.
type Level string

const (
	Pass Level = "pass"
	Warn Level = "warn"
	Fail Level = "fail"
	// Skip means the check could not run (no ffmpeg, file not visible).
	Skip Level = "skip"
)

func (l Level) rank() int {
	switch l {
	case Fail:
		return 3
	case Warn:
		return 2
	case Pass:
		return 1
	}
	return 0
}

// Worse returns the more severe of two levels.
func Worse(a, b Level) Level {
	if b.rank() > a.rank() {
		return b
	}
	return a
}

// Check is the outcome of one test.
type Check struct {
	Name   string `json:"name"` // payload | streams | duration | picture | resolution | codec | hdr | bitdepth | audio | subtitles
	Level  Level  `json:"level"`
	Reason string `json:"reason"`
}

// Verdict is the combined result for one download.
type Verdict struct {
	Level   Level     `json:"level"`
	Summary string    `json:"summary"`
	File    string    `json:"file,omitempty"` // the file probed (path as roarr sees it)
	Checks  []Check   `json:"checks"`
	Media   *Media    `json:"media,omitempty"`
	At      time.Time `json:"at"`
	TookMS  int64     `json:"tookMs"`
}

// Media is a short description of what the probed file contains.
type Media struct {
	Container  string   `json:"container,omitempty"`
	Duration   float64  `json:"duration"` // seconds
	Width      int      `json:"width,omitempty"`
	Height     int      `json:"height,omitempty"`
	VideoCodec string   `json:"videoCodec,omitempty"`
	Audio      []string `json:"audio,omitempty"`     // "eng aac 2ch"
	Subtitles  []string `json:"subtitles,omitempty"` // "eng subrip", "eng hdmv_pgs_subtitle forced"
}

func (v *Verdict) add(c Check) {
	v.Checks = append(v.Checks, c)
}

// finish sets the overall level and a one-line summary: the reason of the
// first, most severe check.
func (v *Verdict) finish() {
	v.Level = Skip
	worst := -1
	unprobed := -1 // a passing file list alone proves nothing
	for i, c := range v.Checks {
		if c.Name == "streams" && c.Level == Skip {
			unprobed = i
			continue
		}
		v.Level = Worse(v.Level, c.Level)
		if c.Level.rank() > 1 && (worst < 0 || c.Level.rank() > v.Checks[worst].Level.rank()) {
			worst = i
		}
	}
	switch {
	case worst >= 0:
		v.Summary = v.Checks[worst].Reason
	case unprobed >= 0:
		v.Level, v.Summary = Skip, "could not be checked: "+v.Checks[unprobed].Reason
	case v.Level == Skip && len(v.Checks) > 0:
		v.Summary = "could not be checked: " + v.Checks[0].Reason
	case v.Level == Skip:
		v.Summary = "nothing was checked"
	default:
		v.Summary = "looks like real media and matches its release name"
	}
}
