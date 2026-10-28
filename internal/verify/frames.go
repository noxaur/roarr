package verify

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"sort"
)

// FrameStats describes one sampled frame in luminance terms (0-255).
type FrameStats struct {
	At      float64   `json:"at"`      // seconds into the file
	Mean    float64   `json:"mean"`    // average brightness
	Std     float64   `json:"std"`     // brightness spread
	Entropy float64   `json:"entropy"` // histogram entropy, bits (0-8)
	Edges   float64   `json:"edges"`   // mean absolute neighbour difference
	Grid    []float64 `json:"-"`       // gridW x gridH block means, to compare frames
}

const (
	gridW = 16
	gridH = 9
)

// Thresholds for one frame. Real footage, even a dark scene, has detail and
// a spread of brightness; a black frame or a single flat colour does not,
// and a text slate has a few brightness values only (low entropy).
const (
	blackMean   = 20.0
	blackStd    = 8.0
	flatStd     = 5.0
	flatEntropy = 2.5
	flatEdges   = 1.0
	// Two frames minutes apart in real footage differ by far more than this
	// mean block difference; re-encodes of one still image do not.
	staticDiff = 2.5
)

// Stats measures an image.
func Stats(img image.Image) FrameStats {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var st FrameStats
	if w == 0 || h == 0 {
		return st
	}
	luma := make([]float64, w*h)
	var hist [256]int
	sum := 0.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := float64(color.GrayModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray).Y)
			luma[y*w+x] = l
			hist[int(l)]++
			sum += l
		}
	}
	n := float64(w * h)
	st.Mean = sum / n
	varSum := 0.0
	for _, l := range luma {
		varSum += (l - st.Mean) * (l - st.Mean)
	}
	st.Std = math.Sqrt(varSum / n)
	for _, c := range hist {
		if c > 0 {
			p := float64(c) / n
			st.Entropy -= p * math.Log2(p)
		}
	}
	edges, cnt := 0.0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x+1 < w {
				edges += math.Abs(luma[y*w+x] - luma[y*w+x+1])
				cnt++
			}
			if y+1 < h {
				edges += math.Abs(luma[y*w+x] - luma[(y+1)*w+x])
				cnt++
			}
		}
	}
	if cnt > 0 {
		st.Edges = edges / float64(cnt)
	}
	st.Grid = make([]float64, gridW*gridH)
	counts := make([]int, gridW*gridH)
	for y := 0; y < h; y++ {
		gy := y * gridH / h
		for x := 0; x < w; x++ {
			gx := x * gridW / w
			st.Grid[gy*gridW+gx] += luma[y*w+x]
			counts[gy*gridW+gx]++
		}
	}
	for i := range st.Grid {
		if counts[i] > 0 {
			st.Grid[i] /= float64(counts[i])
		}
	}
	return st
}

// Black reports a frame that is (nearly) black.
func (s FrameStats) Black() bool { return s.Mean < blackMean && s.Std < blackStd }

// Flat reports a frame with no picture: one colour, or a few brightness
// levels without detail.
func (s FrameStats) Flat() bool {
	return s.Std < flatStd || s.Entropy < flatEntropy || s.Edges < flatEdges
}

// Real reports a frame that looks like picture content.
func (s FrameStats) Real() bool { return !s.Black() && !s.Flat() }

// Diff is the mean absolute difference between two frames' block grids.
func Diff(a, b FrameStats) float64 {
	if len(a.Grid) != len(b.Grid) || len(a.Grid) == 0 {
		return math.Inf(1)
	}
	d := 0.0
	for i := range a.Grid {
		d += math.Abs(a.Grid[i] - b.Grid[i])
	}
	return d / float64(len(a.Grid))
}

// JudgePicture grades sampled frames. failed counts frames ffmpeg could not
// decode.
func JudgePicture(frames []FrameStats, failed int) Check {
	c := Check{Name: "picture", Level: Pass}
	total := len(frames) + failed
	if len(frames) == 0 {
		c.Level, c.Reason = Fail, "no frame could be decoded"
		if total == 0 {
			c.Level, c.Reason = Skip, "no frames sampled"
		}
		return c
	}
	var real []FrameStats
	black, flat := 0, 0
	for _, f := range frames {
		switch {
		case f.Black():
			black++
		case f.Flat():
			flat++
		default:
			real = append(real, f)
		}
	}
	// Largest difference between any two real frames: one moving shot is
	// enough to prove the file is not a still.
	maxDiff := 0.0
	for i := range real {
		for j := i + 1; j < len(real); j++ {
			maxDiff = max(maxDiff, Diff(real[i], real[j]))
		}
	}
	switch {
	case len(real) == 0:
		c.Level, c.Reason = Fail, fmt.Sprintf("all %d sampled frames are black (%d) or flat (%d): no real picture", len(frames), black, flat)
	case len(real) >= 2 && maxDiff < staticDiff:
		c.Level, c.Reason = Fail, fmt.Sprintf("the %d sampled frames all show the same still image (slate)", len(real))
	case len(real) == 1 && len(frames) >= 4:
		c.Level, c.Reason = Fail, fmt.Sprintf("only 1 of %d sampled frames shows a picture", len(frames))
	case len(real)*2 < len(frames):
		c.Level, c.Reason = Warn, fmt.Sprintf("only %d of %d sampled frames show a picture (%d black, %d flat)", len(real), len(frames), black, flat)
	case failed*2 > total:
		c.Level, c.Reason = Warn, fmt.Sprintf("%d of %d frames could not be decoded; the file may be damaged", failed, total)
	default:
		c.Reason = fmt.Sprintf("%d of %d sampled frames show changing picture", len(real), len(frames))
	}
	if failed > 0 && c.Level == Pass {
		c.Reason += fmt.Sprintf(" (%d failed to decode)", failed)
	}
	return c
}

// SampleTimes picks n timestamps spread over the middle 90% of a file: one
// random point inside each of n equal slices, so samples never bunch up.
func SampleTimes(duration float64, n int, rng *rand.Rand) []float64 {
	if duration <= 0 || n <= 0 {
		return nil
	}
	lo, hi := duration*0.05, duration*0.95
	slice := (hi - lo) / float64(n)
	out := make([]float64, n)
	for i := range out {
		out[i] = lo + slice*(float64(i)+rng.Float64())
	}
	sort.Float64s(out)
	return out
}
