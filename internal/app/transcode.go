package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/transcode"
)

// Finished downloads are converted to AV1 in the background. Sonarr and
// Radarr record every import in their history, whoever did it (their own
// download handling or watch-first), so one poller of that history feeds the
// transcode queue.

func transcodeOptions(s config.Settings) transcode.Options {
	t := s.Transcode
	return transcode.Options{Enabled: t.Enabled, Concurrency: t.Concurrency, Nice: t.Nice, CRF: t.CRF, Preset: t.Preset, FilmGrain: t.FilmGrain, MinSavings: t.MinSavings}
}

func (a *App) startTranscode(ctx context.Context) {
	a.Transcode = transcode.New(nil, a.DataDir)
	a.Transcode.SetOptions(transcodeOptions(a.Settings.Get()))
	a.Settings.Subscribe(func(s config.Settings) { a.Transcode.SetOptions(transcodeOptions(s)) })
	a.Transcode.OnDone(a.afterTranscode)
	go a.Transcode.Loop(ctx)
	go a.transcodeImports(ctx)
}

// afterTranscode lets Sonarr/Radarr and Jellyfin see the new file.
func (a *App) afterTranscode(j transcode.Job) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := a.C()
	switch {
	case j.Kind == "tv" && c.Sonarr != nil && j.TitleID > 0:
		_ = c.Sonarr.RescanSeries(ctx, j.TitleID)
	case j.Kind == "movie" && c.Radarr != nil && j.TitleID > 0:
		_ = c.Radarr.RefreshMovie(ctx, j.TitleID)
	}
	if c.Jellyfin != nil {
		_ = c.Jellyfin.RefreshLibrary(ctx)
	}
	a.Library.Refresh(ctx)
}

// transcodeImports polls both *arr histories for new imports.
func (a *App) transcodeImports(ctx context.Context) {
	cursorPath := filepath.Join(a.DataDir, "transcode-cursor.json")
	cursor := map[string]int{}
	firstRun := true
	if b, err := os.ReadFile(cursorPath); err == nil && json.Unmarshal(b, &cursor) == nil {
		firstRun = false
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		changed := false
		c := a.C()
		for _, src := range []struct {
			name string
			cl   *arr.Client
		}{{"sonarr", sonarrClient(c)}, {"radarr", radarrClient(c)}} {
			if src.cl == nil {
				continue
			}
			imports, err := src.cl.RecentImports(ctx, 200)
			if err != nil {
				slog.Debug("transcode: history", "service", src.name, "err", err)
				continue
			}
			last, seen := cursor[src.name]
			var fresh []arr.Import
			maxID := last
			for _, im := range imports {
				maxID = max(maxID, im.ID)
				if seen && im.ID > last && im.Path != "" {
					fresh = append(fresh, im)
				}
			}
			if !seen && len(imports) == 0 {
				continue // nothing to anchor on yet
			}
			// Oldest first, so episodes convert in the order they arrived.
			sort.Slice(fresh, func(i, k int) bool { return fresh[i].ID < fresh[k].ID })
			on := a.Settings.Get().Transcode
			for _, im := range fresh {
				if on.Enabled {
					a.queueTranscode(src.name, im, on.PathMap)
				}
			}
			// First run (no cursor): start from now, never the whole backlog.
			if maxID != last || !seen {
				cursor[src.name] = maxID
				changed = true
			}
		}
		if changed || firstRun {
			if b, err := json.Marshal(cursor); err == nil {
				_ = os.WriteFile(cursorPath, b, 0o600)
			}
			firstRun = false
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func sonarrClient(c *Clients) *arr.Client {
	if c.Sonarr == nil {
		return nil
	}
	return c.Sonarr.Client
}

func radarrClient(c *Clients) *arr.Client {
	if c.Radarr == nil {
		return nil
	}
	return c.Radarr.Client
}

func (a *App) queueTranscode(service string, im arr.Import, pathMap string) {
	it := transcode.Item{Path: mapPath(pathMap, im.Path)}
	if service == "sonarr" {
		it.Kind, it.TitleID = "tv", im.SeriesID
		if l, ok := a.Library.Find("tv:" + strconv.Itoa(im.SeriesID)); ok {
			it.Title = l.Title
		}
	} else {
		it.Kind, it.TitleID = "movie", im.MovieID
		if l, ok := a.Library.Find("movie:" + strconv.Itoa(im.MovieID)); ok {
			it.Title = l.Title
		}
	}
	if it.Title == "" {
		it.Title = filepath.Base(it.Path)
	}
	a.Transcode.Submit(it)
}

// mapPath applies "from=to,from2=to2" prefix rewrites to a path.
func mapPath(spec, p string) string {
	for _, pair := range strings.Split(spec, ",") {
		from, to, ok := strings.Cut(strings.TrimSpace(pair), "=")
		from, to = strings.TrimRight(from, "/"), strings.TrimRight(to, "/")
		if ok && from != "" && (p == from || strings.HasPrefix(p, from+"/")) {
			return to + strings.TrimPrefix(p, from)
		}
	}
	return p
}
