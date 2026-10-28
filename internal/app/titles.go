package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/seerr"
)

// ErrNotConfigured means a needed upstream is missing.
var ErrNotConfigured = errors.New("not configured")

// EpisodeDetail is an episode row on a show page.
type EpisodeDetail struct {
	ID        int       `json:"id"`
	Season    int       `json:"season"`
	Number    int       `json:"number"`
	Absolute  int       `json:"absolute,omitempty"`
	Title     string    `json:"title"`
	AirDate   string    `json:"airDate,omitempty"`
	Aired     bool      `json:"aired"`
	HasFile   bool      `json:"hasFile"`
	Monitored bool      `json:"monitored"`
	Queued    bool      `json:"queued,omitempty"`
	Runtime   int       `json:"runtime,omitempty"`
	File      *FileInfo `json:"file,omitempty"`
}

// FileInfo describes a file on disk.
type FileInfo struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Quality    string `json:"quality,omitempty"`
	Resolution int    `json:"resolution,omitempty"`
	Group      string `json:"group,omitempty"`
	Codec      string `json:"codec,omitempty"`
	HDR        string `json:"hdr,omitempty"`
	Audio      string `json:"audio,omitempty"`
	Languages  string `json:"languages,omitempty"`
	Subtitles  string `json:"subtitles,omitempty"`
	Added      string `json:"added,omitempty"`
}

func fileInfo(f *arr.MediaFile) *FileInfo {
	if f == nil || f.ID == 0 {
		return nil
	}
	fi := &FileInfo{ID: f.ID, Path: f.RelativePath, Size: f.Size, Quality: f.Quality.Quality.Name, Resolution: f.Quality.Quality.Resolution, Group: f.ReleaseGroup, Added: f.DateAdded}
	if fi.Path == "" {
		fi.Path = f.Path
	}
	if m := f.MediaInfo; m != nil {
		fi.Codec, fi.Audio, fi.Languages, fi.Subtitles = m.VideoCodec, m.AudioCodec, m.AudioLanguages, m.Subtitles
		if m.VideoDynamicRange != "" && m.VideoDynamicRange != "SDR" {
			fi.HDR = m.VideoDynamicRange
		}
		if m.AudioChannels > 0 {
			fi.Audio = strings.TrimSpace(fmt.Sprintf("%s %.1f", fi.Audio, m.AudioChannels))
		}
	}
	return fi
}

// SeasonDetail groups episodes.
type SeasonDetail struct {
	Number    int             `json:"number"`
	Monitored bool            `json:"monitored"`
	Episodes  []EpisodeDetail `json:"episodes"`
}

// SeriesDetail is everything a show page needs.
type SeriesDetail struct {
	LibraryItem
	InLibrary bool           `json:"inLibrary"`
	Overview  string         `json:"overview,omitempty"`
	Fanart    string         `json:"fanart,omitempty"`
	Genres    []string       `json:"genres,omitempty"`
	Runtime   int            `json:"runtime,omitempty"`
	Seasons   []SeasonDetail `json:"seasons"`
	Path      string         `json:"path,omitempty"`
	ProfileID int            `json:"qualityProfileId,omitempty"`
	// Synthetic is true when episodes come from TMDB counts rather than
	// Sonarr (title not in the library yet). Episode IDs are then negative.
	Synthetic bool `json:"synthetic,omitempty"`
}

// MovieDetail is everything a movie page needs.
type MovieDetail struct {
	LibraryItem
	InLibrary  bool            `json:"inLibrary"`
	HasFile    bool            `json:"hasFile"`
	Queued     bool            `json:"queued,omitempty"`
	Overview   string          `json:"overview,omitempty"`
	Fanart     string          `json:"fanart,omitempty"`
	Genres     []string        `json:"genres,omitempty"`
	Runtime    int             `json:"runtime,omitempty"`
	Collection *CollectionInfo `json:"collection,omitempty"`
	File       *FileInfo       `json:"file,omitempty"`
	Path       string          `json:"path,omitempty"`
	ProfileID  int             `json:"qualityProfileId,omitempty"`
}

// CollectionInfo lists the movies of a collection.
type CollectionInfo struct {
	TmdbID int               `json:"tmdbId"`
	Title  string            `json:"title"`
	Movies []CollectionMovie `json:"movies"`
}

// CollectionMovie is one part of a collection.
type CollectionMovie struct {
	TmdbID    int    `json:"tmdbId"`
	ImdbID    string `json:"imdbId,omitempty"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	Poster    string `json:"poster,omitempty"`
	Runtime   int    `json:"runtime,omitempty"`
	InLibrary bool   `json:"inLibrary"`
	LibraryID int    `json:"libraryId,omitempty"`
	HasFile   bool   `json:"hasFile"`
}

type cachedDetail struct {
	at     time.Time
	series *SeriesDetail
	movie  *MovieDetail
}

// SyntheticID encodes season/episode for shows not yet in Sonarr.
func SyntheticID(season, ep int) int { return -(season*10000 + ep) }

// Series loads a library show with episodes and queue state.
func (a *App) Series(ctx context.Context, id int, fresh bool) (*SeriesDetail, error) {
	key := "tv:" + strconv.Itoa(id)
	if !fresh {
		a.detailMu.Lock()
		d, ok := a.details[key]
		a.detailMu.Unlock()
		if ok && d.series != nil && time.Since(d.at) < 20*time.Second {
			return d.series, nil
		}
	}
	c := a.C()
	if c.Sonarr == nil {
		return nil, fmt.Errorf("sonarr %w", ErrNotConfigured)
	}
	var (
		s     arr.Series
		eps   []arr.Episode
		queue []arr.QueueRecord
		files []arr.MediaFile
		errS  error
		errE  error
		wg    sync.WaitGroup
	)
	wg.Add(4)
	go func() { defer wg.Done(); files, _ = c.Sonarr.EpisodeFiles(ctx, id) }()
	go func() { defer wg.Done(); s, errS = c.Sonarr.Series(ctx, id) }()
	go func() { defer wg.Done(); eps, errE = c.Sonarr.Episodes(ctx, id) }()
	go func() { defer wg.Done(); queue, _ = c.Sonarr.Queue(ctx, "") }()
	wg.Wait()
	if errS != nil {
		return nil, errS
	}
	if errE != nil {
		return nil, errE
	}
	queued := map[int]bool{}
	for _, q := range queue {
		if q.SeriesID == id && q.EpisodeID != 0 {
			queued[q.EpisodeID] = true
		}
	}
	byFile := map[int]*arr.MediaFile{}
	for i := range files {
		byFile[files[i].ID] = &files[i]
	}
	d := &SeriesDetail{LibraryItem: seriesItem(s), InLibrary: true, Overview: s.Overview, Fanart: arr.Fanart(s.Images), Genres: s.Genres, Runtime: s.Runtime, Path: s.Path, ProfileID: s.QualityProfile}
	seasonMon := map[int]bool{}
	for _, sn := range s.Seasons {
		seasonMon[sn.SeasonNumber] = sn.Monitored
	}
	bySeason := map[int][]EpisodeDetail{}
	for _, e := range eps {
		ed := EpisodeDetail{ID: e.ID, Season: e.SeasonNumber, Number: e.EpisodeNumber, Absolute: e.AbsoluteEpisodeNumber, Title: e.Title,
			Aired: e.Aired(), HasFile: e.HasFile, Monitored: e.Monitored, Queued: queued[e.ID], Runtime: e.Runtime}
		if e.HasFile {
			ed.File = fileInfo(byFile[e.EpisodeFileID])
		}
		if !e.AirDateUTC.IsZero() {
			ed.AirDate = e.AirDateUTC.Format("2006-01-02")
		}
		bySeason[e.SeasonNumber] = append(bySeason[e.SeasonNumber], ed)
	}
	for n, list := range bySeason {
		sort.Slice(list, func(i, j int) bool { return list[i].Number < list[j].Number })
		d.Seasons = append(d.Seasons, SeasonDetail{Number: n, Monitored: seasonMon[n], Episodes: list})
	}
	sortSeasons(d.Seasons)
	a.detailMu.Lock()
	a.details[key] = cachedDetail{at: time.Now(), series: d}
	a.detailMu.Unlock()
	return d, nil
}

func sortSeasons(ss []SeasonDetail) {
	// Specials last, everything else ascending.
	sort.Slice(ss, func(i, j int) bool {
		a, b := ss[i].Number, ss[j].Number
		if a == 0 || b == 0 {
			return b == 0 && a != 0
		}
		return a < b
	})
}

// SeriesByTvdb returns the library show if present, else a preview built
// from Sonarr's lookup plus TMDB episode counts from Seerr when available.
func (a *App) SeriesByTvdb(ctx context.Context, tvdbID, tmdbID int) (*SeriesDetail, error) {
	if it, ok := a.Library.FindTvdb(tvdbID); ok && tvdbID != 0 {
		return a.Series(ctx, it.ID, false)
	}
	if it, ok := a.Library.FindTmdbShow(tmdbID); ok && tmdbID != 0 {
		return a.Series(ctx, it.ID, false)
	}
	key := fmt.Sprintf("tvdb:%d:%d", tvdbID, tmdbID)
	a.detailMu.Lock()
	if d, ok := a.details[key]; ok && d.series != nil && time.Since(d.at) < 10*time.Minute {
		a.detailMu.Unlock()
		return d.series, nil
	}
	a.detailMu.Unlock()

	c := a.C()
	var tv *seerr.TV
	if c.Seerr != nil && tmdbID != 0 {
		if t, err := c.Seerr.TV(ctx, tmdbID); err == nil {
			tv = &t
			if tvdbID == 0 {
				tvdbID = t.ExternalIDs.TvdbID
			}
		}
	}
	if tvdbID != 0 {
		if it, ok := a.Library.FindTvdb(tvdbID); ok {
			return a.Series(ctx, it.ID, false)
		}
	}
	if c.Sonarr == nil {
		if tv == nil {
			return nil, fmt.Errorf("sonarr %w (or Seerr, for TMDB metadata)", ErrNotConfigured)
		}
		d := a.seriesFromSeerr(tv, tvdbID)
		a.detailMu.Lock()
		a.details[key] = cachedDetail{at: time.Now(), series: d}
		a.detailMu.Unlock()
		return d, nil
	}
	if tvdbID == 0 {
		return nil, errors.New("this show has no TVDB id yet, so Sonarr cannot track it")
	}
	hits, err := c.Sonarr.Lookup(ctx, fmt.Sprintf("tvdb:%d", tvdbID))
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("sonarr found nothing for tvdb:%d", tvdbID)
	}
	s := hits[0]
	d := &SeriesDetail{LibraryItem: seriesItem(s), InLibrary: false, Overview: s.Overview, Fanart: arr.Fanart(s.Images), Genres: s.Genres, Runtime: s.Runtime, Synthetic: true}
	d.Key = fmt.Sprintf("tvdb:%d", tvdbID)
	if tv != nil && tv.IsAnime() {
		d.Anime = true
	}
	counts := map[int]int{}
	if tv != nil {
		for _, sn := range tv.Seasons {
			counts[sn.SeasonNumber] = sn.EpisodeCount
		}
	}
	for _, sn := range s.Seasons {
		sd := SeasonDetail{Number: sn.SeasonNumber, Episodes: []EpisodeDetail{}}
		n := counts[sn.SeasonNumber]
		if n == 0 && sn.Statistics != nil {
			n = sn.Statistics.TotalEpisodeCount
		}
		for e := 1; e <= n; e++ {
			sd.Episodes = append(sd.Episodes, EpisodeDetail{ID: SyntheticID(sn.SeasonNumber, e), Season: sn.SeasonNumber, Number: e, Title: fmt.Sprintf("Episode %d", e), Aired: true})
		}
		d.Seasons = append(d.Seasons, sd)
	}
	sortSeasons(d.Seasons)
	if tv != nil && d.TmdbID == 0 {
		d.TmdbID = tv.ID
	}
	a.Native.markOnDisk(d)
	a.detailMu.Lock()
	a.details[key] = cachedDetail{at: time.Now(), series: d}
	a.detailMu.Unlock()
	return d, nil
}

// Movie loads a library movie.
func (a *App) Movie(ctx context.Context, id int) (*MovieDetail, error) {
	c := a.C()
	if c.Radarr == nil {
		return nil, fmt.Errorf("radarr %w", ErrNotConfigured)
	}
	var (
		m     arr.Movie
		err   error
		queue []arr.QueueRecord
		wg    sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); m, err = c.Radarr.Movie(ctx, id) }()
	go func() { defer wg.Done(); queue, _ = c.Radarr.Queue(ctx, "") }()
	wg.Wait()
	if err != nil {
		return nil, err
	}
	d := movieDetail(m, true)
	for _, q := range queue {
		if q.MovieID == id {
			d.Queued = true
		}
	}
	if m.Collection != nil && m.Collection.TmdbID != 0 {
		d.Collection = a.collection(ctx, m.Collection.TmdbID, m.Collection.Title)
		// The library snapshot lags right after a change; this movie is fresh.
		if d.Collection != nil {
			for i := range d.Collection.Movies {
				if d.Collection.Movies[i].TmdbID == m.TmdbID {
					d.Collection.Movies[i].HasFile = m.HasFile
				}
			}
		}
	}
	return d, nil
}

func movieDetail(m arr.Movie, inLib bool) *MovieDetail {
	it := movieItem(m)
	if !inLib {
		it.Key = fmt.Sprintf("tmdb:%d", m.TmdbID)
		it.ID = 0
	}
	d := &MovieDetail{LibraryItem: it, InLibrary: inLib, HasFile: m.HasFile, Overview: m.Overview, Fanart: arr.Fanart(m.Images), Genres: m.Genres, Runtime: m.Runtime}
	if inLib {
		d.File, d.Path, d.ProfileID = fileInfo(m.MovieFile), m.Path, m.QualityProfileID
	}
	return d
}

// MovieByTmdb returns the library movie or a lookup preview.
func (a *App) MovieByTmdb(ctx context.Context, tmdbID int) (*MovieDetail, error) {
	if it, ok := a.Library.FindTmdbMovie(tmdbID); ok {
		return a.Movie(ctx, it.ID)
	}
	c := a.C()
	if c.Radarr == nil {
		if c.Seerr == nil {
			return nil, fmt.Errorf("radarr %w (or Seerr, for TMDB metadata)", ErrNotConfigured)
		}
		m, err := c.Seerr.Movie(ctx, tmdbID)
		if err != nil {
			return nil, err
		}
		return a.movieFromSeerr(ctx, m), nil
	}
	raw, err := c.Radarr.Lookup(ctx, fmt.Sprintf("tmdb:%d", tmdbID))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("radarr found nothing for tmdb:%d", tmdbID)
	}
	d := movieDetail(raw[0], false)
	if raw[0].Collection != nil && raw[0].Collection.TmdbID != 0 {
		d.Collection = a.collection(ctx, raw[0].Collection.TmdbID, raw[0].Collection.Title)
	}
	return d, nil
}

// collection resolves a collection's parts from Radarr, falling back to Seerr.
func (a *App) collection(ctx context.Context, tmdbID int, title string) *CollectionInfo {
	c := a.C()
	ci := &CollectionInfo{TmdbID: tmdbID, Title: title}
	if c.Radarr != nil {
		if col, err := c.Radarr.Collection(ctx, tmdbID); err == nil && col != nil {
			ci.Title = col.Title
			for _, m := range col.Movies {
				ci.Movies = append(ci.Movies, CollectionMovie{TmdbID: m.TmdbID, ImdbID: m.ImdbID, Title: m.Title, Year: m.Year, Poster: arr.Poster(m.Images), Runtime: m.Runtime})
			}
		}
	}
	if len(ci.Movies) == 0 && c.Seerr != nil {
		if col, err := c.Seerr.Collection(ctx, tmdbID); err == nil {
			ci.Title = col.Name
			for _, p := range col.Parts {
				ci.Movies = append(ci.Movies, CollectionMovie{TmdbID: p.ID, Title: p.DisplayTitle(), Year: p.Year(), Poster: seerr.Poster(p.PosterPath, "w342")})
			}
		}
	}
	if len(ci.Movies) == 0 {
		return nil
	}
	for i := range ci.Movies {
		if it, ok := a.Library.FindTmdbMovie(ci.Movies[i].TmdbID); ok {
			ci.Movies[i].InLibrary = true
			ci.Movies[i].LibraryID = it.ID
			ci.Movies[i].HasFile = it.Have > 0
			if ci.Movies[i].ImdbID == "" {
				ci.Movies[i].ImdbID = it.ImdbID
			}
		} else if a.Native.hasMovie(ci.Movies[i].TmdbID) {
			ci.Movies[i].HasFile = true
		}
	}
	sort.Slice(ci.Movies, func(i, j int) bool {
		yi, yj := ci.Movies[i].Year, ci.Movies[j].Year
		if yi == 0 {
			yi = 9999
		}
		if yj == 0 {
			yj = 9999
		}
		return yi < yj
	})
	return ci
}

// DiscoverItem is a search hit from outside the library.
type DiscoverItem struct {
	Kind      string  `json:"kind"` // tv | movie
	Title     string  `json:"title"`
	Year      int     `json:"year,omitempty"`
	Poster    string  `json:"poster,omitempty"`
	Overview  string  `json:"overview,omitempty"`
	TmdbID    int     `json:"tmdbId,omitempty"`
	TvdbID    int     `json:"tvdbId,omitempty"`
	ImdbID    string  `json:"imdbId,omitempty"`
	Rating    float64 `json:"rating,omitempty"`
	InLibrary bool    `json:"inLibrary"`
	LibraryID int     `json:"libraryId,omitempty"`
	Requested bool    `json:"requested,omitempty"`
}

// Discover searches for any title that exists. Seerr (TMDB) answers in a
// few hundred ms with posters for both movies and shows; without Seerr,
// Sonarr and Radarr lookups run in parallel.
func (a *App) Discover(ctx context.Context, q string) ([]DiscoverItem, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	c := a.C()
	if c.Seerr != nil {
		res, err := c.Seerr.Search(ctx, q)
		if err == nil {
			out := make([]DiscoverItem, 0, len(res))
			for _, r := range res {
				if r.MediaType != "tv" && r.MediaType != "movie" {
					continue
				}
				it := DiscoverItem{Kind: r.MediaType, Title: r.DisplayTitle(), Year: r.Year(), Poster: seerr.Poster(r.PosterPath, "w342"), Overview: r.Overview, TmdbID: r.ID, Rating: r.VoteAverage}
				if r.MediaInfo != nil {
					it.TvdbID = r.MediaInfo.TvdbID
					it.Requested = r.MediaInfo.Status == 2 || r.MediaInfo.Status == 3
				}
				a.markLibrary(&it)
				out = append(out, it)
			}
			return out, nil
		}
		if c.Sonarr == nil && c.Radarr == nil {
			return nil, err
		}
	}
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		out    []DiscoverItem
		errs   []error
		series []arr.Series
		movies []arr.Movie
	)
	if c.Sonarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := c.Sonarr.Lookup(ctx, q)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			}
			series = s
		}()
	}
	if c.Radarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := c.Radarr.Lookup(ctx, q)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			}
			movies = m
		}()
	}
	wg.Wait()
	// Interleave so the best show and best movie both appear near the top.
	for i := 0; i < len(series) || i < len(movies); i++ {
		if i < len(series) && i < 20 {
			s := series[i]
			it := DiscoverItem{Kind: "tv", Title: s.Title, Year: s.Year, Poster: arr.Poster(s.Images), Overview: s.Overview, TvdbID: s.TvdbID, TmdbID: s.TmdbID, ImdbID: s.ImdbID}
			a.markLibrary(&it)
			out = append(out, it)
		}
		if i < len(movies) && i < 20 {
			m := movies[i]
			it := DiscoverItem{Kind: "movie", Title: m.Title, Year: m.Year, Poster: arr.Poster(m.Images), Overview: m.Overview, TmdbID: m.TmdbID, ImdbID: m.ImdbID}
			a.markLibrary(&it)
			out = append(out, it)
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return out, nil
}

func (a *App) markLibrary(it *DiscoverItem) {
	var li LibraryItem
	var ok bool
	if it.Kind == "movie" {
		li, ok = a.Library.FindTmdbMovie(it.TmdbID)
	} else {
		if it.TvdbID != 0 {
			li, ok = a.Library.FindTvdb(it.TvdbID)
		}
		if !ok && it.TmdbID != 0 {
			li, ok = a.Library.FindTmdbShow(it.TmdbID)
		}
	}
	if ok {
		it.InLibrary, it.LibraryID = true, li.ID
		if it.TvdbID == 0 {
			it.TvdbID = li.TvdbID
		}
	}
}

// showForMatching converts a detail into the release package's Show.
func showForMatching(d *SeriesDetail) release.Show {
	s := release.Show{Titles: append([]string{d.Title}, d.Aliases...), Year: d.Year, Anime: d.Anime}
	for _, sn := range d.Seasons {
		for _, e := range sn.Episodes {
			s.Episodes = append(s.Episodes, release.Episode{ID: e.ID, Season: e.Season, Number: e.Number, Absolute: e.Absolute, HasFile: e.HasFile, Aired: e.Aired})
		}
	}
	return s
}
