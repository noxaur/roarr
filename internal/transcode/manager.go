package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job states.
const (
	Queued  = "queued"
	Running = "running"
	Done    = "done"
	Skipped = "skipped"
	Failed  = "failed"
)

const maxFinished = 200

// Item is a file to convert.
type Item struct {
	Path    string // path roarr can read
	Title   string
	Kind    string // "tv" | "movie"
	TitleID int    // Sonarr series / Radarr movie id
}

// Job is one file's trip through the queue.
type Job struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	Title      string    `json:"title,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	TitleID    int       `json:"titleId,omitempty"`
	State      string    `json:"state"`
	Percent    float64   `json:"percent"`
	Speed      string    `json:"speed,omitempty"`
	SrcBytes   int64     `json:"srcBytes,omitempty"`
	OutBytes   int64     `json:"outBytes,omitempty"`
	SavedBytes int64     `json:"savedBytes,omitempty"`
	CRF        int       `json:"crf,omitempty"`
	Encoder    string    `json:"encoder,omitempty"`
	Message    string    `json:"message,omitempty"`
	Paused     bool      `json:"paused,omitempty"` // running, but held in place
	QueuedAt   time.Time `json:"queuedAt"`
	StartedAt  time.Time `json:"startedAt,omitzero"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// Status is what the API shows.
type Status struct {
	Enabled     bool   `json:"enabled"`
	Available   bool   `json:"available"` // ffmpeg with an AV1 encoder found
	Encoder     string `json:"encoder,omitempty"`
	Error       string `json:"error,omitempty"`
	Concurrency int    `json:"concurrency"`
	// Paused is a hold set by hand. Held is true while nothing may run: paused,
	// or outside the schedule. NextChange is when the schedule next opens or
	// closes.
	Paused     bool      `json:"paused"`
	Held       bool      `json:"held"`
	Scheduled  bool      `json:"scheduled"`
	NextChange time.Time `json:"nextChange,omitzero"`
	// Zone and ZoneOffset (seconds east of UTC) are the server's time zone,
	// in which the schedule is read.
	Zone       string `json:"zone"`
	ZoneOffset int    `json:"zoneOffset"`
	Counts     struct {
		Queued  int `json:"queued"`
		Running int `json:"running"`
		Done    int `json:"done"`
		Skipped int `json:"skipped"`
		Failed  int `json:"failed"`
	} `json:"counts"`
	BytesSaved int64 `json:"bytesSaved"`
	Jobs       []Job `json:"jobs"`
}

// Manager owns the queue.
type Manager struct {
	run     Runner
	path    string // state file
	ffmpeg  string
	ffprobe string
	onDone  func(Job)

	mu       sync.Mutex
	opts     Options
	enc      string
	encErr   string
	detected bool
	jobs     []*Job
	saved    int64
	seq      int
	kick     chan struct{}
	paused   bool
	active   map[*Job]*active

	now func() time.Time // test hook

	saveMu                  sync.Mutex              // serializes save()
	beforeSwap, afterBackup func()                  // test hooks
	renameFn, linkFn        func(a, b string) error // test hooks (nil = os.Rename / os.Link)
	noReplaceFn             func(a, b string) error // test hook (nil = renameNoReplace)
}

// active is a job whose ffmpeg may be running.
type active struct {
	proc     Proc
	cancel   context.CancelFunc
	stopping bool // cancelled because it could not be suspended
}

// New builds a manager; state is kept in dir/transcode.json.
func New(r Runner, dir string) *Manager {
	if r == nil {
		r = ExecRunner{}
	}
	m := &Manager{run: r, path: filepath.Join(dir, "transcode.json"), ffmpeg: "ffmpeg", ffprobe: "ffprobe", kick: make(chan struct{}, 1), active: map[*Job]*active{}, now: time.Now}
	m.load()
	return m
}

// OnDone registers a callback for files that were replaced by a smaller one.
func (m *Manager) OnDone(f func(Job)) { m.onDone = f }

// SetOptions applies new settings.
func (m *Manager) SetOptions(o Options) {
	m.mu.Lock()
	m.opts = o
	m.applyHoldLocked()
	m.mu.Unlock()
	m.wake()
}

// SetPaused holds or releases the queue by hand. Running jobs are suspended
// where they are and carry on when released. The hold survives a restart.
func (m *Manager) SetPaused(p bool) {
	m.mu.Lock()
	m.paused = p
	m.applyHoldLocked()
	m.mu.Unlock()
	m.save()
	m.wake()
}

// heldLocked reports whether jobs must not run now.
func (m *Manager) heldLocked() bool {
	return m.paused || !Open(m.opts.Schedule, m.now())
}

// applyHoldLocked suspends or resumes running ffmpeg processes to match the
// hold. Where a process cannot be suspended, it is stopped and its job is
// queued again.
func (m *Manager) applyHoldLocked() {
	held := m.heldLocked()
	for j, a := range m.active {
		if a.proc == nil || a.stopping || j.Paused == held {
			continue
		}
		if held {
			if err := a.proc.Suspend(); err != nil {
				a.stopping = true
				a.cancel()
				continue
			}
		} else if err := a.proc.Resume(); err != nil {
			slog.Warn("transcode: cannot resume ffmpeg", "file", filepath.Base(j.Path), "err", err)
			continue
		}
		j.Paused = held
	}
}

func (m *Manager) wake() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Detect finds ffmpeg, ffprobe and an AV1 encoder: SVT-AV1 first, libaom
// only when SVT-AV1 is missing.
func (m *Manager) Detect(ctx context.Context) {
	enc, err := detectEncoder(ctx, m.run, m.ffmpeg, m.ffprobe)
	m.mu.Lock()
	m.detected, m.enc = true, enc
	m.encErr = ""
	if err != nil {
		m.encErr = err.Error()
	}
	m.mu.Unlock()
	if err != nil {
		slog.Warn("transcode: AV1 conversion unavailable", "err", err)
	} else {
		slog.Info("transcode: encoder found", "encoder", enc)
	}
	m.wake()
}

func detectEncoder(ctx context.Context, r Runner, ffmpeg, ffprobe string) (string, error) {
	out, err := r.Output(ctx, ffmpeg, "-hide_banner", "-encoders")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found: %w", err)
	}
	if _, err := r.Output(ctx, ffprobe, "-version"); err != nil {
		return "", fmt.Errorf("ffprobe not found: %w", err)
	}
	return pickEncoder(string(out))
}

func pickEncoder(encoders string) (string, error) {
	has := func(name string) bool {
		for _, l := range strings.Split(encoders, "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 && f[1] == name {
				return true
			}
		}
		return false
	}
	switch {
	case has(EncSVT):
		return EncSVT, nil
	case has(EncAOM):
		return EncAOM, nil
	}
	return "", errors.New("this ffmpeg has no AV1 encoder (libsvtav1 or libaom-av1)")
}

// Submit queues a file. It returns false when conversion is off or the file
// is already queued.
func (m *Manager) Submit(it Item) bool {
	m.mu.Lock()
	if !m.opts.Enabled {
		m.mu.Unlock()
		return false
	}
	for _, j := range m.jobs {
		if j.Path == it.Path && (j.State == Queued || j.State == Running) {
			m.mu.Unlock()
			return false
		}
	}
	m.seq++
	m.jobs = append(m.jobs, &Job{
		ID: strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(m.seq), Path: it.Path, Title: it.Title,
		Kind: it.Kind, TitleID: it.TitleID, State: Queued, QueuedAt: time.Now(),
	})
	m.mu.Unlock()
	m.save()
	m.wake()
	return true
}

// Status returns a snapshot: running jobs, then queued, then finished
// (newest first).
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Enabled: m.opts.Enabled, Available: m.enc != "", Encoder: m.enc, Error: m.encErr, Concurrency: m.opts.Concurrency, BytesSaved: m.saved, Jobs: []Job{}}
	st.Paused, st.Held, st.Scheduled = m.paused, m.heldLocked(), len(m.opts.Schedule) > 0
	now := m.now()
	st.NextChange = NextChange(m.opts.Schedule, now)
	st.Zone, st.ZoneOffset = now.Zone()
	rank := func(s string) int {
		switch s {
		case Running:
			return 0
		case Queued:
			return 1
		}
		return 2
	}
	for _, j := range m.jobs {
		switch j.State {
		case Queued:
			st.Counts.Queued++
		case Running:
			st.Counts.Running++
		case Done:
			st.Counts.Done++
		case Skipped:
			st.Counts.Skipped++
		case Failed:
			st.Counts.Failed++
		}
		st.Jobs = append(st.Jobs, *j)
	}
	sort.SliceStable(st.Jobs, func(a, b int) bool {
		ja, jb := st.Jobs[a], st.Jobs[b]
		if ra, rb := rank(ja.State), rank(jb.State); ra != rb {
			return ra < rb
		}
		if ja.State == Queued {
			return ja.QueuedAt.Before(jb.QueuedAt)
		}
		return ja.FinishedAt.After(jb.FinishedAt)
	})
	return st
}

// Loop starts queued jobs while there is capacity. It returns when ctx ends.
func (m *Manager) Loop(ctx context.Context) {
	m.Detect(ctx)
	t := time.NewTicker(15 * time.Second) // the schedule is followed to the minute
	defer t.Stop()
	for {
		m.mu.Lock()
		m.applyHoldLocked()
		m.mu.Unlock()
		m.startReady(ctx)
		select {
		case <-ctx.Done():
			return
		case <-m.kick:
		case <-t.C:
		}
	}
}

func (m *Manager) startReady(ctx context.Context) {
	for {
		m.mu.Lock()
		if !m.opts.Enabled || m.enc == "" || m.heldLocked() {
			m.mu.Unlock()
			return
		}
		running := 0
		var next *Job
		for _, j := range m.jobs {
			switch {
			case j.State == Running:
				running++
			case j.State == Queued && next == nil:
				next = j
			}
		}
		if next == nil || running >= max(m.opts.Concurrency, 1) {
			m.mu.Unlock()
			return
		}
		next.State, next.StartedAt, next.Percent, next.Message = Running, time.Now(), 0, ""
		opts, enc := m.opts, m.enc
		m.mu.Unlock()
		go func() {
			m.process(ctx, next, opts, enc)
			m.save()
			m.wake()
		}()
	}
}

// update changes a job under the lock.
func (m *Manager) update(j *Job, f func(*Job)) {
	m.mu.Lock()
	f(j)
	m.mu.Unlock()
}

func (m *Manager) finish(j *Job, state, msg string) {
	m.update(j, func(j *Job) {
		j.State, j.Message, j.FinishedAt = state, msg, time.Now()
		if state == Done {
			j.Percent = 100
		}
	})
}

// process converts one file. The original stays untouched until the very
// last step, an atomic rename of the verified result.
func (m *Manager) process(ctx context.Context, j *Job, opts Options, enc string) {
	path := j.Path
	st, err := os.Stat(path)
	if err != nil {
		m.finish(j, Failed, "cannot read file (is the media folder mounted in roarr at the same path as in Sonarr/Radarr?): "+err.Error())
		return
	}
	src, err := Probe(ctx, m.run, m.ffprobe, path)
	if err != nil {
		m.finish(j, Failed, err.Error())
		return
	}
	src.Size = st.Size()
	if reason := Skip(src, path); reason != "" {
		m.update(j, func(j *Job) { j.SrcBytes = src.Size })
		m.finish(j, Skipped, reason)
		return
	}
	tmp := tempPath(path)
	_ = os.Remove(tmp)
	defer os.Remove(tmp) // no-op after a successful rename
	m.update(j, func(j *Job) { j.SrcBytes, j.CRF, j.Encoder = src.Size, CRFFor(opts, src), enc })

	args := BuildArgs(opts, enc, src, path, tmp)
	jctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.mu.Lock()
	m.active[j] = &active{cancel: cancel}
	m.mu.Unlock()
	started := func(p Proc) {
		m.mu.Lock()
		if a := m.active[j]; a != nil {
			a.proc = p
		}
		m.applyHoldLocked() // a hold set while the file was probed
		m.mu.Unlock()
	}
	err = m.run.Run(jctx, opts.Nice, m.ffmpeg, args, func(line string) {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return
		}
		switch k {
		case "out_time_us":
			if us, err := strconv.ParseInt(v, 10, 64); err == nil && src.Duration > 0 {
				p := min(float64(us)/1e6/src.Duration*100, 99.9)
				m.update(j, func(j *Job) { j.Percent = p })
			}
		case "speed":
			m.update(j, func(j *Job) { j.Speed = strings.TrimSpace(v) })
		}
	}, started)
	m.mu.Lock()
	delete(m.active, j)
	j.Paused = false
	m.mu.Unlock()
	if err != nil {
		if jctx.Err() != nil {
			// Shutting down, or held where ffmpeg cannot be suspended: put the
			// file back in the queue.
			m.update(j, func(j *Job) { j.State, j.Percent, j.Speed = Queued, 0, "" })
			return
		}
		m.finish(j, Failed, "ffmpeg failed: "+err.Error())
		return
	}

	ost, err := os.Stat(tmp)
	if err != nil {
		m.finish(j, Failed, "ffmpeg produced no output")
		return
	}
	out, err := Probe(ctx, m.run, m.ffprobe, tmp)
	if err != nil {
		m.finish(j, Failed, "cannot read the result: "+err.Error())
		return
	}
	out.Size = ost.Size()
	if err := Verify(src, out); err != nil {
		m.finish(j, Failed, "result rejected, original kept: "+err.Error())
		return
	}
	m.update(j, func(j *Job) { j.OutBytes = out.Size })
	if !WorthKeeping(src.Size, out.Size, opts.MinSavings) {
		m.finish(j, Skipped, fmt.Sprintf("only %.0f%% smaller (need %d%%), original kept", savedPercent(src.Size, out.Size), opts.MinSavings))
		return
	}
	_ = os.Chmod(tmp, st.Mode().Perm())
	if err := m.swap(path, tmp, st); err != nil {
		m.finish(j, Failed, err.Error())
		return
	}
	saved := src.Size - out.Size
	m.mu.Lock()
	m.saved += saved
	j.SavedBytes = saved
	m.mu.Unlock()
	m.finish(j, Done, "")
	slog.Info("transcode: converted", "file", filepath.Base(path), "saved", saved, "encoder", enc)
	if m.onDone != nil {
		m.onDone(*j)
	}
}

func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// swap puts the verified file tmp in place of path. Sonarr/Radarr may
// replace path at any time, so the original is first moved aside and checked
// to be the very file that was encoded (same inode, size and mtime as orig).
// A newer file is put back untouched. The new file is linked in without
// overwriting (moved in with an atomic no-replace rename when the filesystem
// has no hard links), so a file that appears in the gap is never clobbered.
// Where no atomic no-replace move exists the job fails and the backup is
// kept. The backup is deleted only after the new file is in place.
// An existing backup from an earlier job is never deleted or overwritten.
func (m *Manager) swap(path, tmp string, orig os.FileInfo) error {
	if now, err := os.Stat(path); err != nil || !sameFile(orig, now) {
		return errors.New("the original changed during conversion, result discarded")
	}
	bak := backupPath(path)
	if _, err := os.Lstat(bak); err == nil {
		return fmt.Errorf("an earlier backup is kept at %s; remove it to allow this file to be converted", bak)
	}
	if m.beforeSwap != nil {
		m.beforeSwap()
	}
	rename, link := m.renameFn, m.linkFn
	if rename == nil {
		rename = os.Rename
	}
	if link == nil {
		link = os.Link
	}
	noReplace := m.noReplaceFn
	if noReplace == nil {
		noReplace = renameNoReplace
	}
	if err := rename(path, bak); err != nil {
		return fmt.Errorf("cannot replace the original: %w", err)
	}
	if m.afterBackup != nil {
		m.afterBackup()
	}
	moved, err := os.Stat(bak)
	if err != nil || !sameFile(orig, moved) {
		return restore(noReplace, link, bak, path, "the original changed during conversion, result discarded")
	}
	err = link(tmp, path)
	switch {
	case err == nil:
		_ = os.Remove(tmp)
	case errors.Is(err, fs.ErrExist):
		return restore(noReplace, link, bak, path, "a new file appeared during conversion, result discarded")
	default:
		// No hard links here (SMB, exFAT, some FUSE): move the result in
		// atomically, and only if nothing is at path.
		switch rerr := noReplace(tmp, path); {
		case rerr == nil:
		case errors.Is(rerr, fs.ErrExist):
			return restore(noReplace, link, bak, path, "a new file appeared during conversion, result discarded")
		case errors.Is(rerr, errNoReplaceUnsupported):
			return restore(noReplace, link, bak, path, "cannot put the result in place without risking a newer file: "+rerr.Error())
		default:
			return restore(noReplace, link, bak, path, "cannot replace the original: "+rerr.Error())
		}
	}
	_ = os.Remove(bak)
	return nil
}

// restore moves the backup back to path without overwriting. If another
// writer has put a file there, or no safe no-replace move exists, the backup
// is kept and named in the error.
func restore(noReplace, link func(a, b string) error, bak, path, reason string) error {
	err := moveNoReplace(noReplace, link, bak, path)
	switch {
	case err == nil:
		return errors.New(reason)
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s; a newer file is in place, the previous one is kept as %s", reason, bak)
	}
	return fmt.Errorf("%s; restoring failed (%v), the original is kept as %s", reason, err, bak)
}

func backupPath(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".roarr-orig.bak")
}

func tempPath(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".roarr-av1.tmp")
}

type state struct {
	Jobs       []*Job `json:"jobs"`
	BytesSaved int64  `json:"bytesSaved"`
	Paused     bool   `json:"paused,omitempty"`
}

func (m *Manager) load() {
	b, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var s state
	if json.Unmarshal(b, &s) != nil {
		return
	}
	m.saved, m.paused = s.BytesSaved, s.Paused
	for _, j := range s.Jobs {
		j.Paused = false
		if j.State == Running {
			j.State, j.Percent = Queued, 0 // interrupted by a restart
		}
		m.jobs = append(m.jobs, j)
	}
}

func (m *Manager) save() {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	finished := 0
	for i := len(m.jobs) - 1; i >= 0; i-- {
		if s := m.jobs[i].State; s != Queued && s != Running {
			if finished++; finished > maxFinished {
				m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			}
		}
	}
	b, err := json.Marshal(state{Jobs: m.jobs, BytesSaved: m.saved, Paused: m.paused})
	m.mu.Unlock()
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, m.path)
	}
}
