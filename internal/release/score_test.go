package release

import "testing"

func TestScoreCodecAndSizePrefs(t *testing.T) {
	cand := func(title string, gb float64) Candidate {
		return Candidate{Title: title, Size: int64(gb * 1e9), Seeders: 50, Info: Parse(title)}
	}
	av1 := cand("Show.S01E01.1080p.WEB-DL.Opus.5.1.AV1-GRP", 1.0)
	h264 := cand("Show.S01E01.1080p.WEB-DL.DDP5.1.H.264-GRP", 3.0)

	p := DefaultPrefs()
	if a, b := Score(av1, p, 60).Score, Score(h264, p, 60).Score; a <= b {
		t.Fatalf("defaults should prefer AV1+Opus: av1=%v h264=%v", a, b)
	}

	// Flipping the codec order and asking for bigger files flips the pick.
	p.VideoCodecs = []string{"x264", "x265", "av1"}
	p.AudioCodecs = []string{"DDP"}
	p.Size = "larger"
	if a, b := Score(av1, p, 60).Score, Score(h264, p, 60).Score; a >= b {
		t.Fatalf("x264-first prefs should prefer H.264: av1=%v h264=%v", a, b)
	}
}

func TestScoreLanguages(t *testing.T) {
	cand := func(title string) Candidate {
		return Candidate{Title: title, Size: 2e9, Seeders: 10, Protocol: "torrent", Info: Parse(title)}
	}
	french := cand("Show.S01E01.FRENCH.1080p.WEB.x264-GRP")
	p := DefaultPrefs()
	if !Score(french, p, 45).Rejected {
		t.Error("French-only release allowed for English")
	}
	p.Languages = []string{"English", "French"}
	if Score(french, p, 45).Rejected {
		t.Errorf("French-only release rejected with French allowed: %v", Score(french, p, 45).Reasons)
	}
	p.Languages = nil
	if Score(french, p, 45).Rejected {
		t.Error("no languages should allow any")
	}
}
