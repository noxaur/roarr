package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Runner runs an external command and returns its standard output. Tests
// replace it to serve fixtures instead of calling ffprobe/ffmpeg.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs real commands.
type ExecRunner struct{}

// Output runs name with args. A failure includes the start of stderr.
func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		if msg != "" {
			return out, fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return out, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// Tools are the resolved ffprobe and ffmpeg binaries ("" when missing).
type Tools struct {
	FFprobe string `json:"ffprobe"`
	FFmpeg  string `json:"ffmpeg"`
}

// FindTools looks for ffprobe and ffmpeg: FFPROBE_PATH / FFMPEG_PATH first,
// then $PATH.
func FindTools() Tools {
	find := func(env, name string) string {
		if p := strings.TrimSpace(os.Getenv(env)); p != "" {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
			return ""
		}
		p, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		return p
	}
	return Tools{FFprobe: find("FFPROBE_PATH", "ffprobe"), FFmpeg: find("FFMPEG_PATH", "ffmpeg")}
}

// Probe is the part of `ffprobe -show_format -show_streams` output roarr
// reads.
type Probe struct {
	Streams []Stream `json:"streams"`
	Format  Format   `json:"format"`
}

// Stream is one ffprobe stream.
type Stream struct {
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	PixFmt        string            `json:"pix_fmt"`
	ColorTransfer string            `json:"color_transfer"`
	Channels      int               `json:"channels"`
	Duration      string            `json:"duration"`
	NbFrames      string            `json:"nb_frames"`
	NbReadPackets string            `json:"nb_read_packets"`
	Disposition   map[string]int    `json:"disposition"`
	Tags          map[string]string `json:"tags"`
	SideData      []map[string]any  `json:"side_data_list"`
}

// Format is the ffprobe container section.
type Format struct {
	FormatName string            `json:"format_name"`
	Duration   string            `json:"duration"`
	Size       string            `json:"size"`
	BitRate    string            `json:"bit_rate"`
	Tags       map[string]string `json:"tags"`
}

// ParseProbe decodes ffprobe JSON.
func ParseProbe(b []byte) (*Probe, error) {
	var p Probe
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("ffprobe output is not JSON: %w", err)
	}
	if len(p.Streams) == 0 && p.Format.FormatName == "" {
		return nil, errors.New("ffprobe found no streams")
	}
	return &p, nil
}

// Tag reads a tag case-insensitively. Matroska writes statistics tags with a
// language suffix ("NUMBER_OF_FRAMES-eng"); those match too.
func (s Stream) Tag(key string) string {
	for k, v := range s.Tags {
		if strings.EqualFold(k, key) || len(k) > len(key) && strings.EqualFold(k[:len(key)], key) && k[len(key)] == '-' {
			return v
		}
	}
	return ""
}

// Lang is the stream's language as an ISO 639-2/T code, or "" when the tag is
// missing or undetermined. An untagged stream whose title names a language
// ("English SDH") uses that.
func (s Stream) Lang() string {
	if l := NormLang(s.Tag("language")); l != "" {
		return l
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(s.Tag("title")), func(r rune) bool { return r < 'a' || r > 'z' }) {
		if len(w) > 3 { // names only; "it", "no" are ordinary words
			if l := NormLang(w); l != "" {
				return l
			}
		}
	}
	return ""
}

// Is reports a disposition flag ("forced", "attached_pic", "comment").
func (s Stream) Is(flag string) bool { return s.Disposition[flag] == 1 }

// IsPicture reports a real video stream, not cover art.
func (s Stream) IsPicture() bool {
	if s.CodecType != "video" || s.Is("attached_pic") {
		return false
	}
	switch s.CodecName {
	case "mjpeg", "png", "bmp", "gif", "webp":
		return false
	}
	return true
}

// IsCommentary reports an audio track that is commentary, not a dub.
func (s Stream) IsCommentary() bool {
	return s.Is("comment") || strings.Contains(strings.ToLower(s.Tag("title")), "commentary")
}

// TextSub reports subtitle codecs that carry text (cues can be read).
func (s Stream) TextSub() bool {
	switch s.CodecName {
	case "subrip", "srt", "ass", "ssa", "webvtt", "mov_text", "text", "ttml", "microdvd", "subviewer", "subviewer1", "realtext", "sami", "stl", "jacosub", "mpl2", "pjs", "vplayer":
		return true
	}
	return false
}

// Frames is the stream's frame/packet count from tags or a counted read, or
// -1 when unknown.
func (s Stream) Frames() int {
	for _, v := range []string{s.NbReadPackets, s.Tag("NUMBER_OF_FRAMES"), s.NbFrames} {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return -1
}

// Video returns the main picture stream.
func (p *Probe) Video() (Stream, bool) {
	for _, s := range p.Streams {
		if s.IsPicture() {
			return s, true
		}
	}
	return Stream{}, false
}

// Kind lists streams of one codec type.
func (p *Probe) Kind(t string) []Stream {
	var out []Stream
	for _, s := range p.Streams {
		if s.CodecType == t {
			out = append(out, s)
		}
	}
	return out
}

// Duration in seconds: the container's, else the longest stream's (from the
// stream field or Matroska's DURATION tag).
func (p *Probe) Duration() float64 {
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil && d > 0 {
		return d
	}
	best := 0.0
	for _, s := range p.Streams {
		d, err := strconv.ParseFloat(s.Duration, 64)
		if err != nil || d <= 0 {
			d = parseClock(s.Tag("DURATION"))
		}
		best = max(best, d)
	}
	return best
}

// parseClock reads "01:02:03.456000000".
func parseClock(s string) float64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	sec, e3 := strconv.ParseFloat(parts[2], 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0
	}
	return float64(h*3600+m*60) + sec
}

// HDR reports whether the video stream carries an HDR transfer function or
// Dolby Vision metadata.
func (s Stream) HDR() (bool, bool) {
	known := s.ColorTransfer != "" && s.ColorTransfer != "unknown"
	hdr := s.ColorTransfer == "smpte2084" || s.ColorTransfer == "arib-std-b67"
	for _, sd := range s.SideData {
		if t, _ := sd["side_data_type"].(string); strings.Contains(strings.ToLower(t), "dovi") || strings.Contains(strings.ToLower(t), "dolby vision") {
			return true, true
		}
	}
	return hdr, known
}

// summarize describes a probe for the verdict.
func summarize(p *Probe) *Media {
	m := &Media{Container: p.Format.FormatName, Duration: p.Duration()}
	if v, ok := p.Video(); ok {
		m.Width, m.Height, m.VideoCodec = v.Width, v.Height, v.CodecName
	}
	for _, a := range p.Kind("audio") {
		l := a.Lang()
		if l == "" {
			l = "und"
		}
		desc := fmt.Sprintf("%s %s %dch", l, a.CodecName, a.Channels)
		if a.IsCommentary() {
			desc += " commentary"
		}
		m.Audio = append(m.Audio, desc)
	}
	for _, s := range p.Kind("subtitle") {
		l := s.Lang()
		if l == "" {
			l = "und"
		}
		desc := l + " " + s.CodecName
		if s.Is("forced") {
			desc += " forced"
		}
		m.Subtitles = append(m.Subtitles, desc)
	}
	return m
}
