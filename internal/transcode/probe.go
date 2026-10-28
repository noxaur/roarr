package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// Stream is the part of an ffprobe stream roarr looks at.
type Stream struct {
	Index          int    `json:"index"`
	CodecType      string `json:"codec_type"`
	CodecName      string `json:"codec_name"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	PixFmt         string `json:"pix_fmt"`
	ColorRange     string `json:"color_range"`
	ColorSpace     string `json:"color_space"`
	ColorTransfer  string `json:"color_transfer"`
	ColorPrimaries string `json:"color_primaries"`
	Disposition    struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
	SideData []struct {
		Type string `json:"side_data_type"`
	} `json:"side_data_list"`
}

// Info is a parsed ffprobe answer.
type Info struct {
	Streams  []Stream          `json:"streams"`
	Chapters []json.RawMessage `json:"chapters"`
	Format   struct {
		Name     string `json:"format_name"`
		Duration string `json:"duration"`
		Size     string `json:"size"`
	} `json:"format"`

	Duration float64 `json:"-"` // seconds
	Size     int64   `json:"-"`
}

// IsVideo reports whether s is real video (not cover art).
func (s Stream) IsVideo() bool {
	return s.CodecType == "video" && s.Disposition.AttachedPic == 0
}

// DolbyVision reports whether the stream carries a Dolby Vision layer, which
// re-encoding would silently drop.
func (s Stream) DolbyVision() bool {
	for _, sd := range s.SideData {
		if sd.Type == "DOVI configuration record" {
			return true
		}
	}
	return false
}

// Count returns how many streams of a codec type there are.
func (i Info) Count(codecType string) int {
	n := 0
	for _, s := range i.Streams {
		if s.CodecType == codecType {
			n++
		}
	}
	return n
}

// MainVideo returns the real video streams.
func (i Info) MainVideo() []Stream {
	var out []Stream
	for _, s := range i.Streams {
		if s.IsVideo() {
			out = append(out, s)
		}
	}
	return out
}

// ParseProbe parses the JSON printed by ffprobe.
func ParseProbe(b []byte) (Info, error) {
	var info Info
	if err := json.Unmarshal(b, &info); err != nil {
		return info, fmt.Errorf("unreadable ffprobe output: %w", err)
	}
	info.Duration, _ = strconv.ParseFloat(info.Format.Duration, 64)
	info.Size, _ = strconv.ParseInt(info.Format.Size, 10, 64)
	if len(info.Streams) == 0 {
		return info, fmt.Errorf("ffprobe found no streams")
	}
	return info, nil
}

// Probe runs ffprobe on a file.
func Probe(ctx context.Context, r Runner, ffprobe, path string) (Info, error) {
	out, err := r.Output(ctx, ffprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", "-show_chapters", path)
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe: %w", err)
	}
	return ParseProbe(out)
}
