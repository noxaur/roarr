package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/release"
)

// Blocked is a release roarr will not pick again: it failed (dead, fake,
// nothing usable in it) or a person threw it away.
type Blocked struct {
	ID       string    `json:"id"` // info-hash, or the normalized title when there is none
	Release  string    `json:"release"`
	Hash     string    `json:"hash,omitempty"`
	Indexer  string    `json:"indexer,omitempty"`
	Kind     string    `json:"kind,omitempty"`
	TitleKey string    `json:"titleKey,omitempty"`
	Title    string    `json:"title,omitempty"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
}

// Blocklist is roarr's own blocklist (blocklist.json). Sonarr and Radarr
// keep theirs for arr mode; this one is what native mode has instead.
// Entries never expire: a person removes them.
type Blocklist struct {
	path   string
	mu     sync.Mutex
	list   map[string]Blocked
	names  map[string]string // normalized release name -> id; search results are matched per result
	saveMu sync.Mutex        // snapshot, write and rename as one step
}

// Blocklist returns roarr's own blocklist; nil (it blocks nothing) when
// the native backend does not exist, as in some tests.
func (a *App) Blocklist() *Blocklist {
	if a.Native == nil {
		return nil
	}
	return a.Native.blocklist
}

func openBlocklist(dir string) *Blocklist {
	b := &Blocklist{list: map[string]Blocked{}, names: map[string]string{}}
	if dir == "" {
		return b
	}
	b.path = filepath.Join(dir, "blocklist.json")
	if raw, err := os.ReadFile(b.path); err == nil {
		var list []Blocked
		if json.Unmarshal(raw, &list) == nil {
			for _, e := range list {
				b.list[e.ID] = e
				b.names[release.Normalize(e.Release)] = e.ID
			}
		}
	}
	return b
}

// Add blocks a release. An entry for the same release is replaced.
func (b *Blocklist) Add(e Blocked) {
	e.Hash = strings.ToLower(e.Hash)
	e.ID = e.Hash
	if e.ID == "" {
		e.ID = release.Normalize(e.Release)
	}
	if e.ID == "" {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b.mu.Lock()
	b.list[e.ID] = e
	b.names[release.Normalize(e.Release)] = e.ID
	b.mu.Unlock()
	b.save()
}

// Remove unblocks a release.
func (b *Blocklist) Remove(id string) error {
	b.mu.Lock()
	e, ok := b.list[id]
	delete(b.list, id)
	if name := release.Normalize(e.Release); b.names[name] == id {
		delete(b.names, name)
	}
	b.mu.Unlock()
	if !ok {
		return errors.New("not in the blocklist")
	}
	b.save()
	return nil
}

// List returns every entry, newest first.
func (b *Blocklist) List() []Blocked {
	b.mu.Lock()
	out := make([]Blocked, 0, len(b.list))
	for _, e := range b.list {
		out = append(out, e)
	}
	b.mu.Unlock()
	sort.Slice(out, func(i, k int) bool { return out[i].At.After(out[k].At) })
	return out
}

// Match finds the entry for a release by info-hash or by name. The name
// catches the same release from another indexer, or re-hashed by a
// private tracker.
func (b *Blocklist) Match(title, hash string) (Blocked, bool) {
	if b == nil {
		return Blocked{}, false
	}
	hash, name := strings.ToLower(hash), release.Normalize(title)
	b.mu.Lock()
	defer b.mu.Unlock()
	if e, ok := b.list[hash]; ok && hash != "" {
		return e, true
	}
	if id, ok := b.names[name]; ok && name != "" {
		return b.list[id], true
	}
	return Blocked{}, false
}

// annotate rejects a search result that is on the blocklist, so the
// planner never picks it and the release list says why.
func (b *Blocklist) annotate(rel Release) Release {
	if e, ok := b.Match(rel.Title, rel.InfoHash); ok {
		rel.Rejected = true
		rel.Reasons = append(rel.Reasons, "blocklisted: "+e.Reason)
	}
	return rel
}

func (b *Blocklist) save() {
	if b.path == "" {
		return
	}
	b.saveMu.Lock()
	defer b.saveMu.Unlock()
	raw, err := json.MarshalIndent(b.List(), "", " ")
	if err != nil {
		return
	}
	tmp := b.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, b.path)
	}
}
