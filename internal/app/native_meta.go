package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/noxaur/roarr/internal/importer"
	"github.com/noxaur/roarr/internal/seerr"
)

// Metadata for native imports. roarr needs a title, a year, ids and (for
// shows) the episode list. The sources, in order:
//
//  1. Sonarr/Radarr, when still configured: their TVDB episode numbering
//     is what release names follow, so matching is most reliable.
//  2. TMDB through Seerr, which roarr already uses for discovery. TMDB
//     episode order can differ from scene numbering for some shows.
//
// Once a title is imported its name and year are kept in library.json so
// the library folder never changes under Jellyfin.

// nativeTarget is a resolved grab target.
type nativeTarget struct {
	Title  importer.Title
	Wanted [][2]int // season, episode the user asked for
	Href   string
	Poster string
	ArrID  int // Sonarr/Radarr id, for a rescan after imports
}

func (a *App) nativeTarget(ctx context.Context, g GrabRequest) (*nativeTarget, error) {
	if g.Kind == "movie" {
		return a.nativeMovie(ctx, g)
	}
	return a.nativeShow(ctx, g)
}

func (a *App) nativeShow(ctx context.Context, g GrabRequest) (*nativeTarget, error) {
	c := a.C()
	var d *SeriesDetail
	var err error
	if g.SeriesID > 0 && c.Sonarr != nil {
		d, err = a.Series(ctx, g.SeriesID, false)
	} else {
		d, err = a.SeriesByTvdb(ctx, g.TvdbID, g.TmdbID)
	}
	if err != nil {
		return nil, fmt.Errorf("looking up the show: %w", err)
	}
	t := importer.Title{Kind: "tv", Title: d.Title, Year: d.Year, TmdbID: d.TmdbID, TvdbID: d.TvdbID, ImdbID: d.ImdbID, Aliases: d.Aliases, Anime: d.Anime}
	if t.TmdbID == 0 {
		t.TmdbID = g.TmdbID
	}
	if t.TvdbID == 0 {
		t.TvdbID = g.TvdbID
	}
	if !d.Synthetic {
		t.Episodes = episodesOf(d)
	} else if c.Seerr != nil && t.TmdbID != 0 {
		var seasons []int
		for _, s := range d.Seasons {
			seasons = append(seasons, s.Number)
		}
		eps, err := seerrEpisodes(ctx, c.Seerr, t.TmdbID, seasons)
		if err != nil {
			slog.Warn("native: TMDB episode lists incomplete; using episode counts", "title", t.Title, "err", err)
		}
		t.Episodes = eps
	}
	if len(t.Episodes) == 0 {
		t.Episodes = episodesOf(d)
		// Episode counts can be missing for a season (Sonarr preview with
		// no TMDB data): then the list is not safe for absolute numbers.
		for _, s := range d.Seasons {
			if s.Number > 0 && len(s.Episodes) == 0 {
				t.Partial = true
			}
		}
	}
	a.Native.keepName(&t)

	tg := &nativeTarget{Title: t, Poster: d.Poster}
	if d.InLibrary {
		tg.ArrID, tg.Href = d.ID, fmt.Sprintf("/tv/%d", d.ID)
	} else {
		tg.Href = fmt.Sprintf("/tv/tvdb/%d?tmdb=%d", t.TvdbID, t.TmdbID)
	}
	byID := map[int][2]int{}
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			byID[e.ID] = [2]int{e.Season, e.Number}
		}
	}
	for _, id := range g.EpisodeIDs {
		if id < 0 {
			tg.Wanted = append(tg.Wanted, [2]int{-id / 10000, -id % 10000})
		} else if se, ok := byID[id]; ok {
			tg.Wanted = append(tg.Wanted, se)
		}
	}
	return tg, nil
}

func episodesOf(d *SeriesDetail) []importer.Episode {
	var out []importer.Episode
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			out = append(out, importer.Episode{Season: e.Season, Number: e.Number, Absolute: e.Absolute, Title: e.Title, AirDate: e.AirDate})
		}
	}
	return out
}

// seerrEpisodes fetches TMDB episode lists for the given seasons. It is
// all or nothing: absolute numbers are derived from the whole list, so one
// missing season would shift every later episode.
func seerrEpisodes(ctx context.Context, s *seerr.Client, tmdbID int, seasons []int) ([]importer.Episode, error) {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		out  []importer.Episode
		errs []error
	)
	sem := make(chan struct{}, 4)
	for _, n := range seasons {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			eps, err := s.Season(ctx, tmdbID, n)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			for _, e := range eps {
				out = append(out, importer.Episode{Season: n, Number: e.EpisodeNumber, Title: e.Name, AirDate: e.AirDate})
			}
		}(n)
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Number < out[j].Number
	})
	return out, nil
}

func (a *App) nativeMovie(ctx context.Context, g GrabRequest) (*nativeTarget, error) {
	c := a.C()
	var d *MovieDetail
	var err error
	if g.MovieID > 0 && c.Radarr != nil {
		d, err = a.Movie(ctx, g.MovieID)
	} else {
		d, err = a.MovieByTmdb(ctx, g.TmdbID)
	}
	if err != nil {
		return nil, fmt.Errorf("looking up the movie: %w", err)
	}
	t := importer.Title{Kind: "movie", Title: d.Title, Year: d.Year, TmdbID: d.TmdbID, ImdbID: d.ImdbID, Aliases: d.Aliases}
	if t.TmdbID == 0 {
		t.TmdbID = g.TmdbID
	}
	a.Native.keepName(&t)
	tg := &nativeTarget{Title: t, Poster: d.Poster}
	if d.InLibrary {
		tg.ArrID, tg.Href = d.ID, fmt.Sprintf("/movie/%d", d.ID)
	} else {
		tg.Href = fmt.Sprintf("/movie/tmdb/%d", t.TmdbID)
	}
	return tg, nil
}

// seriesFromSeerr builds a show page from TMDB when Sonarr is not
// configured (native mode). Episode ids are synthetic, as for any show
// that is not in Sonarr; episodes roarr imported count as on disk.
func (a *App) seriesFromSeerr(tv *seerr.TV, tvdbID int) *SeriesDetail {
	it := LibraryItem{Key: fmt.Sprintf("tvdb:%d", tvdbID), Kind: "tv", Title: tv.Name, Year: tv.Year(), Poster: seerr.Poster(tv.PosterPath, "w342"),
		Status: tv.Status, TvdbID: tvdbID, TmdbID: tv.ID, ImdbID: tv.ExternalIDs.ImdbID, Anime: tv.IsAnime()}
	if tv.OriginalName != "" && tv.OriginalName != tv.Name {
		it.Aliases = []string{tv.OriginalName}
	}
	d := &SeriesDetail{LibraryItem: it, Overview: tv.Overview, Fanart: seerr.Poster(tv.BackdropPath, "w1280"), Synthetic: true}
	for _, g := range tv.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	for _, sn := range tv.Seasons {
		sd := SeasonDetail{Number: sn.SeasonNumber, Episodes: []EpisodeDetail{}}
		for e := 1; e <= sn.EpisodeCount; e++ {
			sd.Episodes = append(sd.Episodes, EpisodeDetail{ID: SyntheticID(sn.SeasonNumber, e), Season: sn.SeasonNumber, Number: e, Title: fmt.Sprintf("Episode %d", e), Aired: true})
		}
		d.Seasons = append(d.Seasons, sd)
	}
	sortSeasons(d.Seasons)
	a.Native.markOnDisk(d)
	return d
}

// movieFromSeerr builds a movie page from TMDB when Radarr is not
// configured (native mode).
func (a *App) movieFromSeerr(ctx context.Context, m seerr.Movie) *MovieDetail {
	imdb := m.ImdbID
	if imdb == "" {
		imdb = m.ExternalIDs.ImdbID
	}
	it := LibraryItem{Key: fmt.Sprintf("tmdb:%d", m.ID), Kind: "movie", Title: m.Title, Year: m.Year(), Poster: seerr.Poster(m.PosterPath, "w342"),
		Status: m.Status, TmdbID: m.ID, ImdbID: imdb, Total: 1}
	if m.OriginalTitle != "" && m.OriginalTitle != m.Title {
		it.Aliases = []string{m.OriginalTitle}
	}
	d := &MovieDetail{LibraryItem: it, Overview: m.Overview, Fanart: seerr.Poster(m.BackdropPath, "w1280"), Runtime: m.Runtime}
	for _, g := range m.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	if a.Native.hasMovie(m.ID) {
		d.HasFile, d.Have = true, 1
	}
	if m.Collection != nil && m.Collection.ID != 0 {
		d.Collection = a.collection(ctx, m.Collection.ID, m.Collection.Name)
	}
	return d
}
