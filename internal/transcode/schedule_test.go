package transcode

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 2026-11-02 is a Monday.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, 2+day, hour, minute, 0, 0, time.Local)
}

func TestScheduleOpen(t *testing.T) {
	weeknights := []Window{{Days: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, From: 22 * 60, To: 7 * 60}}
	for _, c := range []struct {
		name string
		t    time.Time
		want bool
	}{
		{"monday noon", at(0, 12, 0), false},
		{"monday 22:00", at(0, 22, 0), true},
		{"tuesday 06:59", at(1, 6, 59), true},
		{"tuesday 07:00", at(1, 7, 0), false},
		{"saturday 02:00, from friday", at(5, 2, 0), true},
		{"sunday 02:00", at(6, 2, 0), false},
		{"monday 02:00, sunday not listed", at(0, 2, 0), false},
	} {
		if got := Open(weeknights, c.t); got != c.want {
			t.Errorf("%s: open = %v, want %v", c.name, got, c.want)
		}
	}
	if !Open(nil, at(0, 12, 0)) {
		t.Error("no schedule must mean any time")
	}
	allDay := []Window{{Days: []time.Weekday{time.Sunday}, From: 0, To: 24 * 60}}
	if !Open(allDay, at(6, 23, 59)) || Open(allDay, at(0, 0, 0)) {
		t.Error("00:00-24:00 must cover Sunday only")
	}
	same := []Window{{From: 9 * 60, To: 9 * 60}}
	if !Open(same, at(0, 8, 59)) || !Open(same, at(0, 9, 0)) {
		t.Error("From == To must cover a full day")
	}
}

func TestScheduleNextChange(t *testing.T) {
	ws := []Window{{From: 22 * 60, To: 7 * 60}}
	if got := NextChange(ws, at(0, 12, 30)); !got.Equal(at(0, 22, 0)) {
		t.Errorf("next change = %v, want monday 22:00", got)
	}
	if got := NextChange(ws, at(0, 23, 15)); !got.Equal(at(1, 7, 0)) {
		t.Errorf("next change = %v, want tuesday 07:00", got)
	}
	if got := NextChange(nil, at(0, 12, 0)); !got.IsZero() {
		t.Errorf("no schedule: next change = %v", got)
	}
}

func TestPauseSuspendsAndResumes(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, ""), proc: &fakeProc{}}
	m, j, _ := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	f.onRun = func() {
		m.SetPaused(true)
		if f.proc.suspends != 1 || !m.Status().Jobs[0].Paused || !m.Status().Held {
			t.Errorf("pause: %d suspends, status %+v", f.proc.suspends, m.Status())
		}
		m.SetPaused(false)
		if f.proc.resumes != 1 || m.Status().Jobs[0].Paused {
			t.Errorf("resume: %d resumes", f.proc.resumes)
		}
	}
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Done {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
}

func TestPauseBeforeStartSuspendsAtOnce(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, ""), proc: &fakeProc{}}
	m, j, _ := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.SetPaused(true) // set while the file is probed
	f.onRun = func() {
		if f.proc.suspends != 1 {
			t.Errorf("%d suspends when ffmpeg started, want 1", f.proc.suspends)
		}
		m.SetPaused(false)
	}
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Done {
		t.Fatalf("state %s (%s)", j.State, j.Message)
	}
}

func TestPauseWithoutSuspendRequeues(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, ""), proc: &fakeProc{err: errors.ErrUnsupported}}
	m, j, path := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	f.onRun = func() { m.SetPaused(true) }
	m.process(context.Background(), j, m.opts, EncSVT)
	if j.State != Queued || j.Percent != 0 {
		t.Fatalf("state %s %.0f%% (%s), want queued", j.State, j.Percent, j.Message)
	}
	if size(t, path) != 1000 {
		t.Error("original touched")
	}
}

func TestHeldQueueStartsNothing(t *testing.T) {
	f := &fakeRunner{outSize: 300, outProbe: probeJSON("av1", 100, 2, 3, 4, "")}
	m, j, _ := setup(t, f, 1000, probeJSON("hevc", 100, 2, 3, 4, ""))
	m.enc = EncSVT
	m.now = func() time.Time { return at(0, 12, 0) }
	o := m.opts
	o.Schedule = []Window{{From: 22 * 60, To: 7 * 60}}
	m.SetOptions(o)
	m.startReady(context.Background())
	if st := m.Status(); j.State != Queued || !st.Held || st.Paused || !st.Scheduled || !st.NextChange.Equal(at(0, 22, 0)) {
		t.Fatalf("outside the schedule: job %s, status %+v", j.State, st)
	}
	m.now = func() time.Time { return at(0, 23, 0) }
	m.SetPaused(true)
	m.startReady(context.Background())
	if j.State != Queued {
		t.Fatalf("paused: job %s", j.State)
	}
}

func TestPauseSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m := New(&fakeRunner{}, dir)
	m.SetPaused(true)
	if !New(&fakeRunner{}, dir).Status().Paused {
		t.Error("pause lost on reload")
	}
	m.SetPaused(false)
	if New(&fakeRunner{}, dir).Status().Paused {
		t.Error("resume lost on reload")
	}
}
