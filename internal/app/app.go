// Package app is roarr's domain layer: it ties the upstream clients together
// into library browsing, streaming search, grabbing, watch-first downloads
// and the activity feed. HTTP handlers live in internal/server.
package app

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/jellyfin"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/search"
	"github.com/noxaur/roarr/internal/seerr"
)

// Clients is one consistent set of upstream clients built from settings.
type Clients struct {
	Sonarr   *arr.Sonarr
	Radarr   *arr.Radarr
	Prowlarr *arr.Prowlarr
	Seerr    *seerr.Client
	Jellyfin *jellyfin.Client
	QBit     *qbit.Client
	Search   *search.Engine
}

// App is the running application.
type App struct {
	Settings *config.Store
	DataDir  string

	mu      sync.RWMutex
	clients *Clients

	Library *Library
	Watch   *WatchFirst
	// Native is roarr's own download + import backend (IMPORT_MODE=native).
	Native *Native
	// Hooks run around every native import. Register at startup:
	// a.Hooks.BeforeImport(...) to verify or hold back a file,
	// a.Hooks.AfterImport(...) to post-process it (e.g. transcode).
	Hooks *importer.Hooks

	bypassMu sync.Mutex // serializes temporary quality profile swaps
	detailMu sync.Mutex
	details  map[string]cachedDetail
}

// New builds the app and starts background loops.
func New(ctx context.Context, store *config.Store, dataDir string) *App {
	a := &App{Settings: store, DataDir: dataDir, details: map[string]cachedDetail{}, Hooks: &importer.Hooks{}}
	a.rebuild(store.Get())
	store.Subscribe(func(s config.Settings) {
		a.rebuild(s)
		a.Library.Refresh(context.Background())
	})
	a.Library = newLibrary(a)
	a.Watch = newWatchFirst(a, dataDir)
	a.Native = newNative(a, dataDir)
	go a.Library.loop(ctx)
	go a.Watch.loop(ctx)
	go a.Native.loop(ctx)
	go a.sweepLoop(ctx)
	go a.seerrLoop(ctx)
	return a
}

func (a *App) rebuild(s config.Settings) {
	c := &Clients{}
	if s.Sonarr.URL != "" {
		c.Sonarr = arr.NewSonarr(s.Sonarr.URL, s.Sonarr.APIKey)
	}
	if s.Radarr.URL != "" {
		c.Radarr = arr.NewRadarr(s.Radarr.URL, s.Radarr.APIKey)
	}
	if s.Prowlarr.URL != "" {
		c.Prowlarr = arr.NewProwlarr(s.Prowlarr.URL, s.Prowlarr.APIKey)
		c.Search = search.NewEngine(c.Prowlarr)
		c.Search.Timeout = time.Duration(s.Behaviour.IndexerTimeout) * time.Second
		c.Search.TTL = time.Duration(s.Behaviour.CacheMinutes) * time.Minute
	}
	if s.Seerr.URL != "" {
		c.Seerr = seerr.New(s.Seerr.URL, s.Seerr.APIKey)
	}
	if s.Jellyfin.URL != "" {
		c.Jellyfin = jellyfin.New(s.Jellyfin.URL, s.Jellyfin.APIKey)
	}
	if s.QBit.URL != "" {
		c.QBit = qbit.New(s.QBit.URL, s.QBit.Username, s.QBit.Password)
	}
	a.mu.Lock()
	old := a.clients
	a.clients = c
	a.mu.Unlock()
	if old != nil && old.Search != nil {
		old.Search.Close()
	}
	go a.discover(c, s)
}

// C returns the current clients.
func (a *App) C() *Clients {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.clients
}

// discover fills gaps from what other services already know: Sonarr/Radarr
// URLs from Prowlarr's app links, and the qBittorrent host from Sonarr's
// download client settings (the password must still come from config).
// In a compose stack roarr often starts before the others are listening, so
// failed lookups are retried for a few minutes.
func (a *App) discover(orig *Clients, s config.Settings) {
	cur := orig
	for attempt, wait := 0, 3*time.Second; ; attempt++ {
		next, retry := discoverOnce(cur, s)
		if next != cur {
			a.mu.Lock()
			swapped := a.clients == cur
			if swapped {
				a.clients = next
			}
			a.mu.Unlock()
			if !swapped {
				return // settings changed meanwhile; a newer discover runs
			}
			cur = next
			if a.Library != nil {
				a.Library.Refresh(context.Background())
			}
		}
		if !retry || attempt >= 8 {
			return
		}
		time.Sleep(wait)
		if a.C() != cur {
			return
		}
		wait = min(wait*2, time.Minute)
	}
}

// discoverOnce returns c, or a copy with discovered clients filled in, and
// whether a lookup failed in a way worth retrying.
func discoverOnce(orig *Clients, s config.Settings) (*Clients, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cp := *orig
	c := &cp
	changed, retry := false, false
	if c.Prowlarr != nil && (c.Sonarr == nil || c.Radarr == nil) {
		apps, err := c.Prowlarr.Applications(ctx)
		if err != nil {
			retry = true
			slog.Debug("discovery: prowlarr applications", "err", err)
		}
		for _, p := range apps {
			base, key := p.FieldString("baseUrl"), p.FieldString("apiKey")
			if base == "" || key == "" || strings.Trim(key, "*") == "" {
				continue // Prowlarr masks API keys in newer versions
			}
			switch p.Implementation {
			case "Sonarr":
				if c.Sonarr == nil {
					c.Sonarr = arr.NewSonarr(config.NormalizeURL(base), key)
					changed = true
					slog.Info("discovered Sonarr via Prowlarr", "url", base)
				}
			case "Radarr":
				if c.Radarr == nil {
					c.Radarr = arr.NewRadarr(config.NormalizeURL(base), key)
					changed = true
					slog.Info("discovered Radarr via Prowlarr", "url", base)
				}
			}
		}
	}
	if c.QBit == nil && s.QBit.Password != "" {
		var srcs []*arr.Client
		if c.Sonarr != nil {
			srcs = append(srcs, c.Sonarr.Client)
		}
		if c.Radarr != nil {
			srcs = append(srcs, c.Radarr.Client)
		}
		for _, src := range srcs {
			dcs, err := src.DownloadClients(ctx)
			if err != nil {
				retry = true
				slog.Debug("discovery: download clients", "service", src.Service, "err", err)
				continue
			}
			for _, d := range dcs {
				if d.Implementation != "QBittorrent" || !d.Enable {
					continue
				}
				scheme := "http"
				if d.FieldBool("useSsl") {
					scheme = "https"
				}
				base := scheme + "://" + d.FieldString("host") + ":" + d.FieldString("port") + "/" + strings.Trim(d.FieldString("urlBase"), "/")
				base = strings.TrimRight(base, "/")
				user := s.QBit.Username
				if user == "" {
					user = d.FieldString("username")
				}
				c.QBit = qbit.New(base, user, s.QBit.Password)
				changed = true
				slog.Info("discovered qBittorrent via "+src.Service, "url", base)
				break
			}
			if c.QBit != nil {
				break
			}
		}
	}
	if !changed {
		return orig, retry
	}
	return c, retry
}

func (a *App) sweepLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if c := a.C(); c.Search != nil {
				c.Search.Sweep()
			}
			a.detailMu.Lock()
			for k, d := range a.details {
				if time.Since(d.at) > 10*time.Minute {
					delete(a.details, k)
				}
			}
			a.detailMu.Unlock()
		}
	}
}
