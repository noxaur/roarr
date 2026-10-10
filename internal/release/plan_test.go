package release

import (
	"reflect"
	"testing"
)

func seq(a, b int) []int { return rangeInts(a, b) }

func TestPlanPrefersPackOnTie(t *testing.T) {
	opts := []Option{{Key: "pack", Score: 70, Covers: seq(1, 10)}}
	for e := 1; e <= 10; e++ {
		opts = append(opts, Option{Key: "single" + itoa(e), Score: 72, Covers: []int{e}})
	}
	p := PlanEpisodes(seq(1, 10), opts)
	if len(p.Picks) != 1 || p.Picks[0].Key != "pack" {
		t.Fatalf("want the pack, got %+v", p)
	}
}

func TestPlanPrefersClearlyBetterSingles(t *testing.T) {
	opts := []Option{{Key: "pack720", Score: 50, Covers: seq(1, 10)}}
	for e := 1; e <= 10; e++ {
		opts = append(opts, Option{Key: "single" + itoa(e), Score: 75, Covers: []int{e}})
	}
	p := PlanEpisodes(seq(1, 10), opts)
	if len(p.Picks) != 10 {
		t.Fatalf("want 10 singles, got %+v", p)
	}
}

func TestPlanSingleEpisodeAvoidsPack(t *testing.T) {
	opts := []Option{{Key: "pack", Score: 80, Covers: seq(1, 10)}, {Key: "e3", Score: 70, Covers: []int{3}}}
	p := PlanEpisodes([]int{3}, opts)
	if len(p.Picks) != 1 || p.Picks[0].Key != "e3" {
		t.Fatalf("want e3, got %+v", p)
	}
}

func TestPlanCompleteVsSeasons(t *testing.T) {
	opts := []Option{
		{Key: "complete", Score: 70, Covers: seq(1, 30)},
		{Key: "s1", Score: 70, Covers: seq(1, 10)},
		{Key: "s2", Score: 70, Covers: seq(11, 20)},
		{Key: "s3", Score: 70, Covers: seq(21, 30)},
	}
	p := PlanEpisodes(seq(1, 30), opts)
	if len(p.Picks) != 1 || p.Picks[0].Key != "complete" {
		t.Fatalf("want complete, got %+v", p)
	}
}

func TestPlanFillsGapsAndReportsMissing(t *testing.T) {
	opts := []Option{
		{Key: "s1", Score: 70, Covers: seq(1, 10)},
		{Key: "e12", Score: 60, Covers: []int{12}},
		{Key: "bad", Score: 90, Rejected: true, Covers: seq(11, 20)},
	}
	p := PlanEpisodes(seq(1, 13), opts)
	keys := []string{}
	for _, pk := range p.Picks {
		keys = append(keys, pk.Key)
	}
	if !reflect.DeepEqual(keys, []string{"s1", "e12"}) || !reflect.DeepEqual(p.Missing, []int{11, 13}) {
		t.Fatalf("got picks %v missing %v", keys, p.Missing)
	}
}
