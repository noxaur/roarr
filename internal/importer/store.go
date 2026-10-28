package importer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// FileRecord is one video roarr placed in the library.
type FileRecord struct {
	TitleKey   string    `json:"titleKey"`
	Season     int       `json:"season,omitempty"`
	Episodes   []int     `json:"episodes,omitempty"`
	Path       string    `json:"path"`
	Subtitles  []string  `json:"subtitles,omitempty"`
	Size       int64     `json:"size"`
	Release    string    `json:"release,omitempty"`
	Hash       string    `json:"hash,omitempty"`
	Source     string    `json:"source,omitempty"`
	Rank       int       `json:"rank"`
	Method     string    `json:"method,omitempty"`
	ImportedAt time.Time `json:"importedAt"`
}

type storeData struct {
	Version int                    `json:"version"`
	Titles  map[string]Title       `json:"titles"`
	Files   map[string]*FileRecord `json:"files"` // by library path
}

// Store is roarr's small library database: the titles it tracks and every
// file it imported, as one JSON file (/config/library.json). It is the
// importer's memory for duplicates and upgrades; the library folders stay
// the source of truth and are checked before anything is replaced.
type Store struct {
	path string
	mu   sync.RWMutex
	d    storeData
}

// OpenStore loads (or starts) the store at path.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, d: storeData{Version: 1, Titles: map[string]Title{}, Files: map[string]*FileRecord{}}}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &s.d); err != nil {
			return nil, errors.New(filepath.Base(path) + " is not valid JSON: " + err.Error())
		}
		if s.d.Titles == nil {
			s.d.Titles = map[string]Title{}
		}
		if s.d.Files == nil {
			s.d.Files = map[string]*FileRecord{}
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.d, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// PutTitle remembers (or refreshes) a tracked title.
func (s *Store) PutTitle(t Title) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Titles[t.Key()] = t
	return s.saveLocked()
}

// Title returns a tracked title by key.
func (s *Store) Title(key string) (Title, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.d.Titles[key]
	return t, ok
}

// Titles lists tracked titles.
func (s *Store) Titles() []Title {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Title, 0, len(s.d.Titles))
	for _, t := range s.d.Titles {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Files lists the imported files of a title (all titles for "").
func (s *Store) Files(titleKey string) []FileRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []FileRecord
	for _, f := range s.d.Files {
		if titleKey == "" || f.TitleKey == titleKey {
			out = append(out, *f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ForEpisode returns the records that hold season/episode of a title.
func (s *Store) ForEpisode(titleKey string, season, episode int) []FileRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []FileRecord
	for _, f := range s.d.Files {
		if f.TitleKey != titleKey || f.Season != season {
			continue
		}
		for _, e := range f.Episodes {
			if e == episode {
				out = append(out, *f)
				break
			}
		}
	}
	return out
}

// HasEpisode reports whether roarr imported a file for the episode that is
// still on disk.
func (s *Store) HasEpisode(titleKey string, season, episode int) bool {
	for _, f := range s.ForEpisode(titleKey, season, episode) {
		if _, err := os.Stat(f.Path); err == nil {
			return true
		}
	}
	return false
}

// Put records an imported file.
func (s *Store) Put(r FileRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := r
	s.d.Files[r.Path] = &cp
	return s.saveLocked()
}

// Forget drops the records of library paths that were removed.
func (s *Store) Forget(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range paths {
		delete(s.d.Files, p)
	}
	return s.saveLocked()
}
