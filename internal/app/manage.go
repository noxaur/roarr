package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/noxaur/roarr/internal/arr"
)

// Managing what is already in the library: deleting files, (un)monitoring,
// changing quality profiles, asking the *arr to rescan or search, and
// removing titles. Every change drops the cached detail and nudges the
// library so the next page load is current.

// EpisodeAction changes a set of episodes of one show.
type EpisodeAction struct {
	Action     string `json:"action"` // delete | monitor | unmonitor | search
	EpisodeIDs []int  `json:"episodeIds"`
}

// ActionResult says what an action did, for the toast.
type ActionResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Count   int    `json:"count,omitempty"`
}

// SeriesEpisodes applies an action to episodes of a library show.
func (a *App) SeriesEpisodes(ctx context.Context, seriesID int, act EpisodeAction) (ActionResult, error) {
	c := a.C()
	if c.Sonarr == nil {
		return ActionResult{}, fmt.Errorf("sonarr %w", ErrNotConfigured)
	}
	ids := positive(act.EpisodeIDs)
	if len(ids) == 0 {
		return ActionResult{}, errors.New("no episodes given")
	}
	defer a.touched(ctx, "tv", seriesID)
	switch act.Action {
	case "delete":
		eps, err := c.Sonarr.Episodes(ctx, seriesID)
		if err != nil {
			return ActionResult{}, err
		}
		want := set(ids)
		files := map[int]bool{}
		for _, e := range eps {
			if want[e.ID] && e.HasFile && e.EpisodeFileID != 0 {
				files[e.EpisodeFileID] = true
			}
		}
		if len(files) == 0 {
			return ActionResult{OK: true, Message: "Nothing to delete; none of those are on disk."}, nil
		}
		if err := c.Sonarr.DeleteEpisodeFiles(ctx, keys(files)); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{OK: true, Count: len(files), Message: plural(len(files), "file") + " deleted"}, nil
	case "monitor", "unmonitor":
		on := act.Action == "monitor"
		if err := c.Sonarr.MonitorEpisodes(ctx, ids, on); err != nil {
			return ActionResult{}, err
		}
		if on {
			d, err := a.Series(ctx, seriesID, true)
			if err == nil {
				want := set(ids)
				seasons := map[int]bool{}
				for _, s := range d.Seasons {
					for _, e := range s.Episodes {
						if want[e.ID] {
							seasons[s.Number] = true
						}
					}
				}
				_ = c.Sonarr.EnsureSeasonsMonitored(ctx, seriesID, keys(seasons))
			}
		}
		verb := "Monitoring"
		if !on {
			verb = "Stopped monitoring"
		}
		return ActionResult{OK: true, Count: len(ids), Message: fmt.Sprintf("%s %s", verb, plural(len(ids), "episode"))}, nil
	case "search":
		if err := c.Sonarr.SearchEpisodes(ctx, ids); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{OK: true, Count: len(ids), Message: "Sonarr is searching for " + plural(len(ids), "episode")}, nil
	}
	return ActionResult{}, fmt.Errorf("unknown action %q", act.Action)
}

// TitlePatch changes title-level settings. Nil fields stay as they are.
type TitlePatch struct {
	Monitored *bool `json:"monitored"`
	ProfileID *int  `json:"qualityProfileId"`
	// Season, with Monitored, flips one season's flag instead of the show's.
	Season *int `json:"season"`
}

// PatchSeries changes a library show.
func (a *App) PatchSeries(ctx context.Context, id int, p TitlePatch) error {
	c := a.C()
	if c.Sonarr == nil {
		return fmt.Errorf("sonarr %w", ErrNotConfigured)
	}
	defer a.touched(ctx, "tv", id)
	if p.Monitored != nil {
		var err error
		if p.Season != nil {
			err = c.Sonarr.SetSeasonMonitored(ctx, id, *p.Season, *p.Monitored)
		} else {
			err = c.Sonarr.SetMonitored(ctx, id, *p.Monitored)
		}
		if err != nil {
			return err
		}
	}
	if p.ProfileID != nil {
		return c.Sonarr.SetQualityProfile(ctx, id, *p.ProfileID)
	}
	return nil
}

// PatchMovie changes a library movie.
func (a *App) PatchMovie(ctx context.Context, id int, p TitlePatch) error {
	c := a.C()
	if c.Radarr == nil {
		return fmt.Errorf("radarr %w", ErrNotConfigured)
	}
	defer a.touched(ctx, "movie", id)
	if p.Monitored != nil {
		if err := c.Radarr.SetMonitored(ctx, id, *p.Monitored); err != nil {
			return err
		}
	}
	if p.ProfileID != nil {
		return c.Radarr.SetQualityProfile(ctx, id, *p.ProfileID)
	}
	return nil
}

// TitleCommand asks Sonarr/Radarr to do something with a title:
// "refresh" (metadata + rescan disk) or "search" (its own search for what
// is missing).
func (a *App) TitleCommand(ctx context.Context, kind string, id int, name string) (ActionResult, error) {
	c := a.C()
	defer a.touched(ctx, kind, id)
	if kind == "movie" {
		if c.Radarr == nil {
			return ActionResult{}, fmt.Errorf("radarr %w", ErrNotConfigured)
		}
		switch name {
		case "refresh":
			return ActionResult{OK: true, Message: "Radarr is rescanning the movie"}, c.Radarr.RefreshMovie(ctx, id)
		case "search":
			return ActionResult{OK: true, Message: "Radarr is searching"}, c.Radarr.SearchMovie(ctx, id)
		}
		return ActionResult{}, fmt.Errorf("unknown command %q", name)
	}
	if c.Sonarr == nil {
		return ActionResult{}, fmt.Errorf("sonarr %w", ErrNotConfigured)
	}
	switch name {
	case "refresh":
		return ActionResult{OK: true, Message: "Sonarr is rescanning the show"}, c.Sonarr.RefreshSeries(ctx, id)
	case "search":
		_, err := c.Sonarr.RunCommand(ctx, map[string]any{"name": "SeriesSearch", "seriesId": id})
		return ActionResult{OK: true, Message: "Sonarr is searching for missing episodes"}, err
	}
	return ActionResult{}, fmt.Errorf("unknown command %q", name)
}

// DeleteMovieFile removes a movie's file from disk.
func (a *App) DeleteMovieFile(ctx context.Context, id int) (ActionResult, error) {
	c := a.C()
	if c.Radarr == nil {
		return ActionResult{}, fmt.Errorf("radarr %w", ErrNotConfigured)
	}
	defer a.touched(ctx, "movie", id)
	m, err := c.Radarr.Movie(ctx, id)
	if err != nil {
		return ActionResult{}, err
	}
	if m.MovieFile == nil || m.MovieFile.ID == 0 {
		return ActionResult{OK: true, Message: "Nothing on disk to delete."}, nil
	}
	if err := c.Radarr.DeleteMovieFile(ctx, m.MovieFile.ID); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{OK: true, Count: 1, Message: "File deleted"}, nil
}

// RemoveTitle deletes a show or movie from Sonarr/Radarr, optionally with
// everything on disk.
func (a *App) RemoveTitle(ctx context.Context, kind string, id int, deleteFiles bool) error {
	c := a.C()
	var err error
	if kind == "movie" {
		if c.Radarr == nil {
			return fmt.Errorf("radarr %w", ErrNotConfigured)
		}
		err = c.Radarr.DeleteMovie(ctx, id, deleteFiles)
	} else {
		if c.Sonarr == nil {
			return fmt.Errorf("sonarr %w", ErrNotConfigured)
		}
		err = c.Sonarr.DeleteSeries(ctx, id, deleteFiles)
	}
	if err != nil {
		return err
	}
	a.touched(ctx, kind, id)
	a.Library.Load(ctx)
	return nil
}

// Profile is a quality profile users can move a title to.
type Profile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Profiles lists Sonarr's (tv) or Radarr's (movie) quality profiles,
// without roarr's internal bypass profile.
func (a *App) Profiles(ctx context.Context, kind string) ([]Profile, error) {
	c := a.C()
	var cl *arr.Client
	if kind == "movie" && c.Radarr != nil {
		cl = c.Radarr.Client
	} else if kind != "movie" && c.Sonarr != nil {
		cl = c.Sonarr.Client
	}
	if cl == nil {
		return nil, fmt.Errorf("%s %w", kind, ErrNotConfigured)
	}
	ps, err := cl.QualityProfiles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(ps))
	for _, p := range ps {
		if p.Name != arr.BypassProfileName {
			out = append(out, Profile{ID: p.ID, Name: p.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// touched forgets cached details of a title and reloads the library soon.
func (a *App) touched(ctx context.Context, kind string, id int) {
	a.detailMu.Lock()
	delete(a.details, fmt.Sprintf("%s:%d", kind, id))
	a.detailMu.Unlock()
	a.Library.Refresh(ctx)
}

func positive(ids []int) []int {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func set(ids []int) map[int]bool {
	m := make(map[int]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func keys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
