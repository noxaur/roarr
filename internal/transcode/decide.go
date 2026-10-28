package transcode

import (
	"fmt"
	"math"
)

// Skip returns why a file should not be converted, or "" when it should be.
func Skip(info Info, path string) string {
	if containerFor(path) == "" {
		return "container not supported (only .mkv and .mp4)"
	}
	video := info.MainVideo()
	if len(video) == 0 {
		return "no video stream"
	}
	for _, v := range video {
		if v.CodecName == "av1" {
			return "already AV1"
		}
		if v.DolbyVision() {
			return "Dolby Vision (converting would drop the Dolby layer)"
		}
	}
	if info.Duration <= 0 {
		return "unknown duration"
	}
	return ""
}

// WorthKeeping reports whether an encode saved at least minPercent.
func WorthKeeping(src, out int64, minPercent int) bool {
	if src <= 0 || out <= 0 {
		return false
	}
	return float64(out) <= float64(src)*(1-float64(minPercent)/100)
}

// savedPercent is how much smaller out is than src.
func savedPercent(src, out int64) float64 {
	if src <= 0 {
		return 0
	}
	return (1 - float64(out)/float64(src)) * 100
}

// Verify checks that a converted file still holds everything the original
// did: same length (within one second), same number of video, audio and
// subtitle streams, same chapters, and AV1 video.
func Verify(src, out Info) error {
	if out.Size <= 0 {
		return fmt.Errorf("output is empty")
	}
	if d := math.Abs(src.Duration - out.Duration); d > 1 || math.IsNaN(d) {
		return fmt.Errorf("duration differs: %.1fs before, %.1fs after", src.Duration, out.Duration)
	}
	for _, t := range []string{"video", "audio", "subtitle"} {
		if a, b := src.Count(t), out.Count(t); a != b {
			return fmt.Errorf("%s stream count differs: %d before, %d after", t, a, b)
		}
	}
	if len(src.Chapters) != len(out.Chapters) {
		return fmt.Errorf("chapter count differs: %d before, %d after", len(src.Chapters), len(out.Chapters))
	}
	main := out.MainVideo()
	if len(main) == 0 {
		return fmt.Errorf("output has no video")
	}
	for _, v := range main {
		if v.CodecName != "av1" {
			return fmt.Errorf("output video is %s, not AV1", v.CodecName)
		}
	}
	return nil
}
