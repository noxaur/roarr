// Package config loads roarr's settings. Environment variables (set in
// docker-compose) always win; anything not set there can be edited in the UI
// and is persisted to $ROARR_DATA/settings.json.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/noxaur/roarr/internal/release"
)

// Service is a URL + credential pair.
type Service struct {
	URL      string `json:"url"`
	APIKey   string `json:"apiKey,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Behaviour holds workflow switches.
type Behaviour struct {
	// WatchFirst downloads packs episode-by-episode in order and imports
	// each episode the moment it completes (qBittorrent only).
	WatchFirst bool `json:"watchFirst"`
	// Prefetch starts searching as soon as a title page opens.
	Prefetch bool `json:"prefetch"`
	// IndexerTimeout is the hard per-query budget, in seconds.
	IndexerTimeout int `json:"indexerTimeout"`
	// CacheMinutes is how long indexer answers are reused.
	CacheMinutes int `json:"cacheMinutes"`
	// SeerrAutoFetch grabs approved Seerr requests automatically.
	SeerrAutoFetch bool `json:"seerrAutoFetch"`
}

// Settings is the full configuration.
type Settings struct {
	Sonarr   Service `json:"sonarr"`
	Radarr   Service `json:"radarr"`
	Prowlarr Service `json:"prowlarr"`
	Seerr    Service `json:"seerr"`
	Jellyfin Service `json:"jellyfin"`
	QBit     Service `json:"qbittorrent"`

	// Auth: "none", "password", or "jellyfin".
	AuthMode          string `json:"authMode"`
	AuthPassword      string `json:"authPassword,omitempty"`
	JellyfinAdminOnly bool   `json:"jellyfinAdminOnly"`

	Prefs     release.Prefs `json:"prefs"`
	Behaviour Behaviour     `json:"behaviour"`
}

// Defaults returns the out-of-the-box settings.
func Defaults() Settings {
	return Settings{
		AuthMode:          "",
		JellyfinAdminOnly: true,
		Prefs:             release.DefaultPrefs(),
		Behaviour: Behaviour{
			WatchFirst:     true,
			Prefetch:       true,
			IndexerTimeout: 30,
			CacheMinutes:   20,
		},
	}
}

// envSpec maps environment variables onto settings fields.
var envSpec = []struct {
	key string
	set func(*Settings, string)
}{
	{"SONARR_URL", func(s *Settings, v string) { s.Sonarr.URL = v }},
	{"SONARR_API_KEY", func(s *Settings, v string) { s.Sonarr.APIKey = v }},
	{"RADARR_URL", func(s *Settings, v string) { s.Radarr.URL = v }},
	{"RADARR_API_KEY", func(s *Settings, v string) { s.Radarr.APIKey = v }},
	{"PROWLARR_URL", func(s *Settings, v string) { s.Prowlarr.URL = v }},
	{"PROWLARR_API_KEY", func(s *Settings, v string) { s.Prowlarr.APIKey = v }},
	{"SEERR_URL", func(s *Settings, v string) { s.Seerr.URL = v }},
	{"SEERR_API_KEY", func(s *Settings, v string) { s.Seerr.APIKey = v }},
	{"JELLYFIN_URL", func(s *Settings, v string) { s.Jellyfin.URL = v }},
	{"JELLYFIN_API_KEY", func(s *Settings, v string) { s.Jellyfin.APIKey = v }},
	{"QBIT_URL", func(s *Settings, v string) { s.QBit.URL = v }},
	{"QBIT_USERNAME", func(s *Settings, v string) { s.QBit.Username = v }},
	{"QBIT_PASSWORD", func(s *Settings, v string) { s.QBit.Password = v }},
	{"AUTH_MODE", func(s *Settings, v string) { s.AuthMode = strings.ToLower(v) }},
	{"AUTH_PASSWORD", func(s *Settings, v string) { s.AuthPassword = v }},
	{"JELLYFIN_ADMIN_ONLY", func(s *Settings, v string) { s.JellyfinAdminOnly = truthy(v) }},
	{"WATCH_FIRST", func(s *Settings, v string) { s.Behaviour.WatchFirst = truthy(v) }},
	{"PREFETCH", func(s *Settings, v string) { s.Behaviour.Prefetch = truthy(v) }},
	{"INDEXER_TIMEOUT", func(s *Settings, v string) { s.Behaviour.IndexerTimeout = atoi(v, 30) }},
	{"CACHE_MINUTES", func(s *Settings, v string) { s.Behaviour.CacheMinutes = atoi(v, 20) }},
	{"SEERR_AUTO_FETCH", func(s *Settings, v string) { s.Behaviour.SeerrAutoFetch = truthy(v) }},
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func atoi(v string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// Store holds settings and persists UI edits.
type Store struct {
	path   string
	mu     sync.RWMutex
	file   Settings // what the UI saved
	cur    Settings // file overlaid with env
	locked map[string]bool
	subs   []func(Settings)
}

// Open loads settings from dir/settings.json and the environment.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "settings.json"), file: Defaults()}
	b, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &s.file); err != nil {
			return nil, errors.New("settings.json is not valid JSON: " + err.Error())
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	s.recompute()
	return s, nil
}

func (s *Store) recompute() {
	cur := s.file
	locked := map[string]bool{}
	for _, e := range envSpec {
		if v, ok := os.LookupEnv(e.key); ok && strings.TrimSpace(v) != "" {
			e.set(&cur, strings.TrimSpace(v))
			locked[e.key] = true
		}
	}
	for _, svc := range []*Service{&cur.Sonarr, &cur.Radarr, &cur.Prowlarr, &cur.Seerr, &cur.Jellyfin, &cur.QBit} {
		svc.URL = strings.TrimRight(strings.TrimSpace(svc.URL), "/")
	}
	if cur.AuthMode == "" {
		switch {
		case cur.AuthPassword != "":
			cur.AuthMode = "password"
		case cur.Jellyfin.URL != "":
			cur.AuthMode = "jellyfin"
		default:
			cur.AuthMode = "none"
		}
	}
	if cur.Behaviour.IndexerTimeout <= 0 {
		cur.Behaviour.IndexerTimeout = 30
	}
	if cur.Behaviour.CacheMinutes <= 0 {
		cur.Behaviour.CacheMinutes = 20
	}
	if len(cur.Prefs.Resolutions) == 0 {
		cur.Prefs.Resolutions = release.DefaultPrefs().Resolutions
	}
	s.cur, s.locked = cur, locked
}

// Get returns the effective settings.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Locked reports which env keys are set (and therefore not editable).
func (s *Store) Locked() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.locked))
	for k, v := range s.locked {
		out[k] = v
	}
	return out
}

// Update applies a mutation to the persisted settings and notifies subscribers.
func (s *Store) Update(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	next := s.file
	fn(&next)
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return Settings{}, err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		s.mu.Unlock()
		return Settings{}, err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		s.mu.Unlock()
		return Settings{}, err
	}
	s.file = next
	s.recompute()
	cur := s.cur
	subs := append([]func(Settings){}, s.subs...)
	s.mu.Unlock()
	for _, f := range subs {
		f(cur)
	}
	return cur, nil
}

// Subscribe registers a callback for settings changes.
func (s *Store) Subscribe(fn func(Settings)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}
