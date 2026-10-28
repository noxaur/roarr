package transcode

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Encoder names as ffmpeg knows them.
const (
	EncSVT = "libsvtav1"
	EncAOM = "libaom-av1"
)

// Options is the converter's tuning.
type Options struct {
	Enabled     bool
	Concurrency int
	Nice        int
	CRF         int // 0 = automatic from the resolution
	Preset      int
	FilmGrain   int // 0 = off
	MinSavings  int // percent
}

// AutoCRF picks a quality level for a picture size. SVT-AV1's CRF scale is
// 0-63 (lower is better); about 30 suits 1080p. The height is taken from the
// width as well, so cropped 2.39:1 "1080p" (1920x800) counts as 1080p.
func AutoCRF(width, height int) int {
	h := max(height, width*9/16)
	switch {
	case h <= 0:
		return 30
	case h <= 480:
		return 26
	case h <= 720:
		return 28
	case h <= 1080:
		return 30
	case h <= 1440:
		return 32
	default:
		return 34
	}
}

// containerFor returns the ffmpeg muxer for a file name, or "" when roarr
// does not convert that container.
func containerFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv":
		return "matroska"
	case ".mp4", ".m4v":
		return "mp4"
	}
	return ""
}

// BuildArgs builds the ffmpeg command line that converts in to out. Every
// stream is carried over: audio, subtitles, attachments, chapters and
// metadata are copied, video is encoded to 10-bit AV1 with its HDR colour
// tags kept.
func BuildArgs(opts Options, enc string, info Info, in, out string) []string {
	mux := containerFor(in)
	args := []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error", "-nostats", "-progress", "pipe:1", "-i", in}
	if mux == "mp4" {
		// MP4 cannot hold attachments, data tracks or most subtitle formats.
		args = append(args, "-map", "0:v", "-map", "0:a?", "-map", "0:s?", "-c", "copy", "-c:s", "mov_text")
	} else {
		args = append(args, "-map", "0", "-c", "copy")
	}
	args = append(args, "-map_chapters", "0", "-map_metadata", "0")

	ord := 0
	for _, s := range info.Streams {
		if s.CodecType != "video" {
			continue
		}
		i := strconv.Itoa(ord)
		ord++
		if s.Disposition.AttachedPic != 0 {
			continue // cover art stays as it is
		}
		crf := opts.CRF
		if crf <= 0 {
			crf = AutoCRF(s.Width, s.Height)
		}
		sp := ":v:" + i
		args = append(args, "-c"+sp, enc, "-crf"+sp, strconv.Itoa(crf), "-pix_fmt"+sp, "yuv420p10le")
		switch enc {
		case EncAOM:
			args = append(args, "-b"+sp, "0", "-cpu-used"+sp, strconv.Itoa(min(max(opts.Preset-2, 1), 8)), "-row-mt"+sp, "1")
			if opts.FilmGrain > 0 {
				args = append(args, "-aom-params"+sp, fmt.Sprintf("denoise-noise-level=%d", opts.FilmGrain*2))
			}
		default:
			params := "tune=0:enable-qm=1"
			if opts.FilmGrain > 0 {
				params += fmt.Sprintf(":film-grain=%d:film-grain-denoise=0", opts.FilmGrain)
			}
			args = append(args, "-preset"+sp, strconv.Itoa(opts.Preset), "-svtav1-params"+sp, params)
		}
		// Keep HDR / wide-gamut signalling exactly as the source has it.
		for _, c := range [][2]string{
			{"color_primaries", s.ColorPrimaries}, {"color_trc", s.ColorTransfer},
			{"colorspace", s.ColorSpace}, {"color_range", s.ColorRange},
		} {
			if c[1] != "" && c[1] != "unknown" {
				args = append(args, "-"+c[0]+sp, c[1])
			}
		}
	}
	if mux == "mp4" {
		args = append(args, "-movflags", "+faststart")
	}
	return append(args, "-f", mux, out)
}

// CRFFor reports the CRF BuildArgs uses for the first real video stream.
func CRFFor(opts Options, info Info) int {
	if opts.CRF > 0 {
		return opts.CRF
	}
	if v := info.MainVideo(); len(v) > 0 {
		return AutoCRF(v[0].Width, v[0].Height)
	}
	return AutoCRF(0, 0)
}
