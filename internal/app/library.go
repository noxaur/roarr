package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/arr"
)

// LibraryItem is the compact, list-friendly view of a show or movie that is
// already in Sonarr/Radarr. The whole library is shipped to the browser once
// so typing in the search box filters it with zero network round-trips.
type LibraryItem struct {
	Key        string   `json:"key"` // "tv:12" | "movie:4"
	Kind       string   `json:"kind"`
	ID         int      `json:"id"`
	Title      string   `json:"title"`
	SortTitle  string   `json:"sortTitle,omitempty"`
	Year       int      `json:"year,omitempty"`
	Poster     string   `json:"poster,omitempty"`
	Status     string   `json:"status,omitempty"`
	Network    string   `json:"network,omitempty"`
	Monitored  bool     `json:"monitored"`
	Have       int      `json:"have"`
	Total      int      `json:"total"`
	SizeOnDisk int64    `json:"sizeOnDisk,omitempty"`
	TvdbID     int      `json:"tvdbId,omitempty"`
	TmdbID     int      `json:"tmdbId,omitempty"`
	ImdbID     string   `json:"imdbId,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
	Anime      bool     `json:"anime,omitempty"`
}

// Library caches the Sonarr + Radarr libraries in memory.
type Library struct {
	app *App

	mu     sync.RWMutex
	items  []LibraryItem
	byKey  map[string]int
	etag   string
	at     time.Time
	errors map[string]string
	kick   chan struct{}
}

func newLibrary(a *App) *Library {
	return &Library{app: a, byKey: map[string]int{}, errors: map[string]string{}, kick: make(chan struct{}, 1)}
}

// Snapshot returns all items with an ETag.
func (l *Library) Snapshot() ([]LibraryItem, string, map[string]string) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	errs := make(map[string]string, len(l.errors))
	for k, v := range l.errors {
		errs[k] = v
	}
	return l.items, l.etag, errs
}

// Find returns an item by key ("tv:12").
func (l *Library) Find(key string) (LibraryItem, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if i, ok := l.byKey[key]; ok {
		return l.items[i], true
	}
	return LibraryItem{}, false
}

// FindTvdb returns the library series with this TVDB id.
func (l *Library) FindTvdb(id int) (LibraryItem, bool) {
	return l.findBy(func(it LibraryItem) bool { return it.Kind == "tv" && it.TvdbID == id })
}

// FindTmdbMovie returns the library movie with this TMDB id.
func (l *Library) FindTmdbMovie(id int) (LibraryItem, bool) {
	return l.findBy(func(it LibraryItem) bool { return it.Kind == "movie" && it.TmdbID == id })
}

// FindTmdbShow returns the library series with this TMDB id.
func (l *Library) FindTmdbShow(id int) (LibraryItem, bool) {
	return l.findBy(func(it LibraryItem) bool { return it.Kind == "tv" && it.TmdbID == id })
}

func (l *Library) findBy(f func(LibraryItem) bool) (LibraryItem, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, it := range l.items {
		if f(it) {
			return it, true
		}
	}
	return LibraryItem{}, false
}

// Refresh asks the loop to reload soon (coalesced).
func (l *Library) Refresh(ctx context.Context) {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// Load reloads synchronously.
func (l *Library) Load(ctx context.Context) {
	c := l.app.C()
	var (
		wg     sync.WaitGroup
		series []arr.Series
		movies []arr.Movie
		errs   = map[string]string{}
		emu    sync.Mutex
	)
	if c.Sonarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := c.Sonarr.AllSeries(ctx)
			if err != nil {
				emu.Lock()
				errs["sonarr"] = err.Error()
				emu.Unlock()
				return
			}
			series = s
		}()
	}
	if c.Radarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := c.Radarr.Movies(ctx)
			if err != nil {
				emu.Lock()
				errs["radarr"] = err.Error()
				emu.Unlock()
				return
			}
			movies = m
		}()
	}
	wg.Wait()

	items := make([]LibraryItem, 0, len(series)+len(movies))
	for _, s := range series {
		items = append(items, seriesItem(s))
	}
	for _, m := range movies {
		items = append(items, movieItem(m))
	}
	// Native mode without Sonarr/Radarr: roarr's own library.json.
	for _, kind := range []string{"tv", "movie"} {
		if l.app.nativeLib(kind) {
			items = append(items, l.app.Native.libraryItems(kind)...)
		}
	}
	l.mu.Lock()
	// Keep the previous side of the library when one service failed.
	if errs["sonarr"] != "" || errs["radarr"] != "" {
		for _, old := range l.items {
			if (old.Kind == "tv" && errs["sonarr"] != "") || (old.Kind == "movie" && errs["radarr"] != "") {
				items = append(items, old)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return sortKey(items[i]) < sortKey(items[j]) })
	byKey := make(map[string]int, len(items))
	for i, it := range items {
		byKey[it.Key] = i
	}
	b, _ := json.Marshal(items)
	sum := sha1.Sum(b)
	l.items, l.byKey, l.etag, l.at, l.errors = items, byKey, `"`+hex.EncodeToString(sum[:8])+`"`, time.Now(), errs
	l.mu.Unlock()
	for k, v := range errs {
		slog.Warn("library refresh failed", "service", k, "err", v)
	}
}

func sortKey(it LibraryItem) string {
	t := it.SortTitle
	if t == "" {
		t = it.Title
	}
	return t + "\x00" + it.Kind
}

func (l *Library) loop(ctx context.Context) {
	l.Load(ctx)
	t := time.NewTicker(90 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-l.kick:
			// Coalesce bursts of refresh requests (e.g. several grabs).
			time.Sleep(300 * time.Millisecond)
		}
		lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		l.Load(lctx)
		cancel()
	}
}

func seriesItem(s arr.Series) LibraryItem {
	it := LibraryItem{
		Key: "tv:" + strconv.Itoa(s.ID), Kind: "tv", ID: s.ID, Title: s.Title, SortTitle: s.SortTitle, Year: s.Year,
		Poster: arr.Poster(s.Images), Status: s.Status, Network: s.Network, Monitored: s.Monitored,
		TvdbID: s.TvdbID, TmdbID: s.TmdbID, ImdbID: s.ImdbID, Anime: s.SeriesType == "anime",
	}
	if s.Statistics != nil {
		it.Have = s.Statistics.EpisodeFileCount
		it.Total = s.Statistics.EpisodeCount
		it.SizeOnDisk = s.Statistics.SizeOnDisk
	}
	seen := map[string]bool{s.Title: true}
	for _, a := range s.AlternateTitles {
		if !seen[a.Title] && len(it.Aliases) < 4 {
			it.Aliases = append(it.Aliases, a.Title)
			seen[a.Title] = true
		}
	}
	return it
}

func movieItem(m arr.Movie) LibraryItem {
	it := LibraryItem{
		Key: "movie:" + strconv.Itoa(m.ID), Kind: "movie", ID: m.ID, Title: m.Title, Year: m.Year,
		Poster: arr.Poster(m.Images), Status: m.Status, Network: m.Studio, Monitored: m.Monitored,
		TmdbID: m.TmdbID, ImdbID: m.ImdbID, Total: 1, SizeOnDisk: m.SizeOnDisk,
	}
	if m.HasFile {
		it.Have = 1
	}
	if m.OriginalTitle != "" && m.OriginalTitle != m.Title {
		it.Aliases = append(it.Aliases, m.OriginalTitle)
	}
	return it
}
