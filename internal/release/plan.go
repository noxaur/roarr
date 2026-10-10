package release

import "sort"

// Option is a candidate release from the planner's point of view.
type Option struct {
	Key      string // stable identifier of the release
	Score    float64
	Rejected bool
	Covers   []int // episode IDs
}

// Pick is one release chosen by the planner and the wanted episodes it
// is responsible for.
type Pick struct {
	Key    string `json:"key"`
	Fills  []int  `json:"fills"`
	Extras int    `json:"extras"` // episodes it brings that were not wanted
}

// Plan is the planner's answer: which releases to grab and which wanted
// episodes nothing could satisfy.
type Plan struct {
	Picks   []Pick `json:"picks"`
	Missing []int  `json:"missing"`
}

const (
	// perGrab is the score cost of every extra release in a plan. It makes
	// one season pack beat ten equivalent singles, while a clearly better
	// single (by more than ~perGrab points) still wins.
	perGrab = 8.0
	// wasteFactor is how much of an episode's value an unwanted episode
	// in a pack costs (bandwidth, disk, duplicate files).
	wasteFactor = 0.35
)

// PlanEpisodes chooses releases that cover the wanted episodes with the
// best quality per episode, preferring fewer releases when quality ties.
// It is the greedy weighted set-cover heuristic: repeatedly take the release
// with the best value per newly covered episode.
func PlanEpisodes(wanted []int, opts []Option) Plan {
	remaining := make(map[int]bool, len(wanted))
	for _, id := range wanted {
		remaining[id] = true
	}
	want := make(map[int]bool, len(wanted))
	for _, id := range wanted {
		want[id] = true
	}
	used := make(map[string]bool)
	var plan Plan
	for len(remaining) > 0 {
		best := -1
		bestRate := 0.0
		var bestFills []int
		bestExtras := 0
		for idx, o := range opts {
			if o.Rejected || used[o.Key] || o.Score <= 0 {
				continue
			}
			var fills []int
			extras := 0
			for _, id := range o.Covers {
				if remaining[id] {
					fills = append(fills, id)
				} else if !want[id] {
					extras++
				}
			}
			if len(fills) == 0 {
				continue
			}
			n := float64(len(fills))
			rate := (o.Score*n - wasteFactor*o.Score*float64(extras) - perGrab) / n
			if best < 0 || rate > bestRate || (rate == bestRate && len(fills) > len(bestFills)) {
				best, bestRate, bestFills, bestExtras = idx, rate, fills, extras
			}
		}
		if best < 0 || bestRate <= 0 {
			break
		}
		used[opts[best].Key] = true
		for _, id := range bestFills {
			delete(remaining, id)
		}
		plan.Picks = append(plan.Picks, Pick{Key: opts[best].Key, Fills: bestFills, Extras: bestExtras})
	}
	for id := range remaining {
		plan.Missing = append(plan.Missing, id)
	}
	sort.Ints(plan.Missing)
	return plan
}
