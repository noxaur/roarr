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
	if b, err := os.ReadFile(cursorPath); err == nil {
		_ = json.Unmarshal(b, &cursor)
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
			last, seen := cursor[src.name]
			cl := src.cl
			fresh, next, err := newImports(func(page int) ([]arr.Import, error) {
				return cl.RecentImports(ctx, page, importPageSize)
			}, last, seen)
			if err != nil {
				slog.Debug("transcode: history", "service", src.name, "err", err)
				continue // cursor stays; the next poll retries everything
			}
			on := a.Settings.Get().Transcode
			for _, im := range fresh {
				if on.Enabled {
					a.queueTranscode(src.name, im, on.PathMap)
				}
			}
			if next != last || !seen {
				cursor[src.name] = next
				changed = true
			}
		}
		if changed {
			if b, err := json.Marshal(cursor); err == nil {
				_ = os.WriteFile(cursorPath, b, 0o600)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

const (
	importPageSize = 200
	importMaxPages = 50 // 10 000 imports between two polls is the most we follow
)

// newImports returns the imports newer than the cursor, oldest first, and
// the cursor to store. Without a cursor (first run) nothing is returned: the
// newest id, or 0 for an empty history, becomes the cursor so only later
// imports are converted. With one, history is read page by page until it
// reaches records at or below the cursor, so a backlog is never skipped. An
// error anywhere returns no imports and leaves the cursor alone.
func newImports(fetch func(page int) ([]arr.Import, error), last int, seen bool) ([]arr.Import, int, error) {
	next := last
	var fresh []arr.Import
	got := map[int]bool{}
	for page := 1; page <= importMaxPages; page++ {
		recs, err := fetch(page)
		if err != nil {
			return nil, last, err
		}
		reached := false
		for _, im := range recs {
			next = max(next, im.ID)
			if im.ID <= last {
				reached = true
			} else if seen && im.Path != "" && !got[im.ID] {
				got[im.ID] = true
				fresh = append(fresh, im)
			}
		}
		if !seen || reached || len(recs) < importPageSize {
			break
		}
	}
	// Oldest first, so episodes convert in the order they arrived.
	sort.Slice(fresh, func(i, k int) bool { return fresh[i].ID < fresh[k].ID })
	return fresh, next, nil
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
