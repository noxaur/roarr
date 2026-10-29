package transcode

import (
	"slices"
	"time"
)

// Window is a weekly span of local time in which files may be converted.
type Window struct {
	Days []time.Weekday // days the window starts on; empty = every day
	From int            // minutes after midnight
	To   int            // minutes after midnight, up to 1440; To <= From runs past midnight
}

func (w Window) startsOn(d time.Weekday) bool {
	return len(w.Days) == 0 || slices.Contains(w.Days, d)
}

// covers reports whether t falls inside the window. A window that runs past
// midnight belongs to the day it starts on; From == To is a full 24 hours.
func (w Window) covers(t time.Time) bool {
	n := t.Hour()*60 + t.Minute()
	day := t.Weekday()
	if w.From < w.To {
		return w.startsOn(day) && n >= w.From && n < w.To
	}
	return (w.startsOn(day) && n >= w.From) || (w.startsOn((day+6)%7) && n < w.To)
}

// Open reports whether conversion may run at t. No windows means any time.
func Open(ws []Window, t time.Time) bool {
	if len(ws) == 0 {
		return true
	}
	for _, w := range ws {
		if w.covers(t) {
			return true
		}
	}
	return false
}

// NextChange returns the first minute after t at which Open changes, or the
// zero time when there are no windows or nothing changes within a week.
func NextChange(ws []Window, t time.Time) time.Time {
	if len(ws) == 0 {
		return time.Time{}
	}
	now := Open(ws, t)
	at := t.Truncate(time.Minute)
	for range 8 * 24 * 60 {
		at = at.Add(time.Minute)
		if Open(ws, at) != now {
			return at
		}
	}
	return time.Time{}
}
