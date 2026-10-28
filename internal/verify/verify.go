package verify

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image/png"
	"math/rand/v2"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/release"
)

// Target is one finished download to verify.
type Target struct {
	Release    string // release title as grabbed
	Kind       string // tv | movie
	RuntimeMin int    // expected minutes per episode (tv) or for the movie; 0 = unknown
	Files      []File
}

// Verifier runs the checks.
type Verifier struct {
	Tools  Tools
	Run    Runner
	Frames int // frames to sample; default 7
	// SubWindow limits how much of a file is read to count subtitle cues.
	SubWindow time.Duration
	Rand      *rand.Rand
}

// New returns a verifier using the real ffprobe/ffmpeg.
func New(t Tools) *Verifier {
	return &Verifier{Tools: t, Run: ExecRunner{}}
}

func (v *Verifier) frames() int {
	if v.Frames > 0 {
		return v.Frames
	}
	return 7
}

func (v *Verifier) subWindow() float64 {
	if v.SubWindow > 0 {
		return v.SubWindow.Seconds()
	}
	return 2400
}

func (v *Verifier) rng() *rand.Rand {
	if v.Rand != nil {
		return v.Rand
	}
	return rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x9e3779b97f4a7c15))
}

// Verify checks a download. It never returns an error: problems running
// the tools become Skip or Fail checks.
func (v *Verifier) Verify(ctx context.Context, t Target) Verdict {
	start := time.Now()
	vd := Verdict{At: start}
	defer func() { vd.TookMS = time.Since(start).Milliseconds() }()

	payload, videos := CheckPayload(t.Files)
	vd.add(payload)
	primary, ok := PickPrimary(videos, t.Kind)
	if payload.Level == Fail || !ok {
		vd.finish()
		return vd
	}
	vd.File = primary.Path
	if primary.Path == "" {
		vd.add(Check{Name: "streams", Level: Skip, Reason: "roarr cannot see the download's files (set VERIFY_PATH_MAP)"})
		vd.finish()
		return vd
	}
	if _, err := os.Stat(primary.Path); err != nil {
		vd.add(Check{Name: "streams", Level: Skip, Reason: "file not visible to roarr at " + primary.Path + " (set VERIFY_PATH_MAP)"})
		vd.finish()
		return vd
	}
	if v.Tools.FFprobe == "" {
		vd.add(Check{Name: "streams", Level: Skip, Reason: "ffprobe is not installed"})
		vd.finish()
		return vd
	}

	pctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	out, err := v.Run.Output(pctx, v.Tools.FFprobe, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", primary.Path)
	cancel()
	var p *Probe
	if err == nil {
		p, err = ParseProbe(out)
	}
	if err != nil {
		if ctx.Err() != nil {
			vd.add(Check{Name: "streams", Level: Skip, Reason: "cancelled"})
		} else {
			vd.add(Check{Name: "streams", Level: Fail, Reason: "ffprobe cannot read the file: " + firstLine(err.Error())})
		}
		vd.finish()
		return vd
	}
	vd.Media = summarize(p)
	video, hasVideo := p.Video()
	if !hasVideo {
		vd.add(Check{Name: "streams", Level: Fail, Reason: "the file has no video stream"})
		vd.finish()
		return vd
	}
	vd.add(Check{Name: "streams", Level: Pass, Reason: fmt.Sprintf("%s, %s %dx%d, %d audio, %d subtitle", p.Format.FormatName, video.CodecName, video.Width, video.Height, len(p.Kind("audio")), len(p.Kind("subtitle")))})

	base := path.Base(primary.Name)
	eps := len(release.Parse(base).Episodes)
	dur := p.Duration()
	vd.add(CheckDuration(dur, t.RuntimeMin, eps, t.Kind))

	if v.Tools.FFmpeg == "" {
		vd.add(Check{Name: "picture", Level: Skip, Reason: "ffmpeg is not installed"})
	} else if dur > 0 {
		vd.add(v.picture(ctx, primary.Path, video.Index, dur))
	}

	cl := ParseClaims(t.Release)
	cl = mergeClaims(cl, ParseClaims(base))
	if cl.AnySubs && v.Tools.FFmpeg != "" {
		v.countSubs(ctx, primary.Path, p, cl, dur)
	}
	for _, c := range CompareClaims(cl, p) {
		vd.add(c)
	}
	vd.finish()
	return vd
}

// mergeClaims fills what the release title leaves out from the file name.
func mergeClaims(a, b Claims) Claims {
	if a.Resolution == 0 {
		a.Resolution = b.Resolution
	}
	if a.Codec == "" {
		a.Codec = b.Codec
	}
	if a.HDR == "" {
		a.HDR = b.HDR
	}
	a.TenBit = a.TenBit || b.TenBit
	a.Multi = a.Multi || b.Multi
	a.AnySubs = a.AnySubs || b.AnySubs
	a.MultiSubs = a.MultiSubs || b.MultiSubs
	a.HardSubbed = a.HardSubbed || b.HardSubbed
	for _, l := range b.Audio {
		if !containsStr(a.Audio, l) {
			a.Audio = append(a.Audio, l)
		}
	}
	for _, l := range b.Subs {
		if !containsStr(a.Subs, l) {
			a.Subs = append(a.Subs, l)
		}
	}
	return a
}

// CheckDuration compares a file's length with the expected runtime.
// episodes is how many episodes the file holds (multi-episode files).
func CheckDuration(sec float64, runtimeMin, episodes int, kind string) Check {
	c := Check{Name: "duration", Level: Pass}
	if sec <= 0 {
		c.Level, c.Reason = Warn, "the file reports no duration"
		return c
	}
	got := fmtMin(sec)
	if sec < 120 {
		c.Level, c.Reason = Fail, "only "+got+" long: a sample or trailer"
		return c
	}
	if runtimeMin > 0 {
		expected := float64(runtimeMin*max(1, episodes)) * 60
		r := sec / expected
		switch {
		case r < 0.35:
			c.Level, c.Reason = Fail, fmt.Sprintf("%s long, expected about %s: a sample or a cut", got, fmtMin(expected))
		case r < 0.6:
			c.Level, c.Reason = Warn, fmt.Sprintf("%s long, shorter than the expected %s", got, fmtMin(expected))
		case r > 3.5:
			c.Level, c.Reason = Warn, fmt.Sprintf("%s long, far longer than the expected %s", got, fmtMin(expected))
		default:
			c.Reason = fmt.Sprintf("%s long, expected about %s", got, fmtMin(expected))
		}
		return c
	}
	// Runtime unknown: only extreme lengths stand out.
	switch {
	case kind == "movie" && sec < 20*60:
		c.Level, c.Reason = Fail, "only "+got+" long for a movie"
	case kind == "movie" && sec < 45*60:
		c.Level, c.Reason = Warn, "only "+got+" long for a movie"
	case kind == "movie" && sec > 6*3600:
		c.Level, c.Reason = Warn, got+" long, unusually long for a movie"
	case kind != "movie" && sec < 5*60:
		c.Level, c.Reason = Warn, "only "+got+" long for an episode"
	case kind != "movie" && sec > 4*3600:
		c.Level, c.Reason = Warn, got+" long, unusually long for an episode"
	default:
		c.Reason = got + " long"
	}
	return c
}

func fmtMin(sec float64) string {
	if sec < 90 {
		return fmt.Sprintf("%.0fs", sec)
	}
	return fmt.Sprintf("%.0f min", sec/60)
}

// picture samples frames and judges them.
func (v *Verifier) picture(ctx context.Context, file string, stream int, dur float64) Check {
	var frames []FrameStats
	failed := 0
	for _, at := range SampleTimes(dur, v.frames(), v.rng()) {
		if ctx.Err() != nil {
			return Check{Name: "picture", Level: Skip, Reason: "cancelled"}
		}
		st, err := v.frame(ctx, file, stream, at)
		if err != nil {
			failed++
			continue
		}
		frames = append(frames, st)
	}
	return JudgePicture(frames, failed)
}

// frame extracts one small grey frame at a timestamp.
func (v *Verifier) frame(ctx context.Context, file string, stream int, at float64) (FrameStats, error) {
	fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := v.Run.Output(fctx, v.Tools.FFmpeg, "-nostdin", "-v", "error",
		"-ss", strconv.FormatFloat(at, 'f', 2, 64), "-i", file,
		"-map", "0:"+strconv.Itoa(stream), "-frames:v", "1",
		"-vf", "scale=160:90:flags=area,format=gray",
		"-f", "image2pipe", "-c:v", "png", "-")
	if err != nil {
		return FrameStats{}, err
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		return FrameStats{}, fmt.Errorf("frame at %.0fs: %w", at, err)
	}
	st := Stats(img)
	st.At = at
	return st, nil
}

// countSubs fills in cue/packet counts for subtitle streams that carry no
// count in their tags, reading at most SubWindow of the file. Only tracks
// that matter for the claims are read.
func (v *Verifier) countSubs(ctx context.Context, file string, p *Probe, cl Claims, dur float64) {
	window := v.subWindow()
	whole := dur > 0 && window >= dur*0.9
	counted := 0
	for i := range p.Streams {
		s := &p.Streams[i]
		if s.CodecType != "subtitle" || s.Frames() >= 0 || counted >= 8 {
			continue
		}
		if len(cl.Subs) > 0 && s.Lang() != "" && !containsStr(cl.Subs, s.Lang()) {
			continue
		}
		counted++
		n := -1
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if s.TextSub() {
			out, err := v.Run.Output(cctx, v.Tools.FFmpeg, "-nostdin", "-v", "error", "-i", file,
				"-map", "0:"+strconv.Itoa(s.Index), "-t", strconv.FormatFloat(window, 'f', 0, 64), "-c:s", "srt", "-f", "srt", "-")
			if err == nil {
				n = CountCues(out)
			}
		} else if v.Tools.FFprobe != "" {
			out, err := v.Run.Output(cctx, v.Tools.FFprobe, "-v", "error", "-select_streams", strconv.Itoa(s.Index),
				"-read_intervals", "%+"+strconv.FormatFloat(window, 'f', 0, 64), "-count_packets",
				"-show_entries", "stream=nb_read_packets", "-print_format", "json", file)
			if err == nil {
				if pp, err := ParseProbe(out); err == nil && len(pp.Streams) > 0 {
					n, _ = strconv.Atoi(pp.Streams[0].NbReadPackets)
				}
			}
		}
		cancel()
		// Nothing in the first part of a long file proves little: a track
		// may start late. Only a read of (nearly) the whole file counts as
		// empty.
		if n == 0 && !whole {
			n = -1
		}
		if n >= 0 {
			s.NbReadPackets = strconv.Itoa(n)
		}
	}
}

// CountCues counts SubRip cues that hold visible text.
func CountCues(srt []byte) int {
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(srt))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	inCue, hasText := false, false
	flush := func() {
		if inCue && hasText {
			n++
		}
		inCue, hasText = false, false
	}
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		switch {
		case strings.Contains(line, "-->"):
			flush()
			inCue = true
		case line == "":
			flush()
		case inCue && stripTags(line) != "":
			hasText = true
		}
	}
	flush()
	return n
}

// stripTags removes <i>-style and {\an8}-style markup.
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<' || r == '{':
			depth++
		case (r == '>' || r == '}') && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
