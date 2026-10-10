package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Sonarr is a Sonarr v4 (API v3) client.
type Sonarr struct{ *Client }

// NewSonarr returns a Sonarr client.
func NewSonarr(base, key string) *Sonarr { return &Sonarr{newClient("sonarr", base, key, "/api/v3")} }

// Series is a Sonarr series.
type Series struct {
	ID              int      `json:"id"`
	Title           string   `json:"title"`
	SortTitle       string   `json:"sortTitle"`
	Year            int      `json:"year"`
	TvdbID          int      `json:"tvdbId"`
	TmdbID          int      `json:"tmdbId"`
	ImdbID          string   `json:"imdbId"`
	Overview        string   `json:"overview"`
	Network         string   `json:"network"`
	Status          string   `json:"status"`
	SeriesType      string   `json:"seriesType"`
	Monitored       bool     `json:"monitored"`
	Runtime         int      `json:"runtime"`
	Path            string   `json:"path"`
	QualityProfile  int      `json:"qualityProfileId"`
	Images          []Image  `json:"images"`
	Genres          []string `json:"genres"`
	AlternateTitles []struct {
		Title        string `json:"title"`
		SeasonNumber *int   `json:"seasonNumber"`
	} `json:"alternateTitles"`
	Seasons []struct {
		SeasonNumber int  `json:"seasonNumber"`
		Monitored    bool `json:"monitored"`
		Statistics   *struct {
			EpisodeFileCount  int   `json:"episodeFileCount"`
			EpisodeCount      int   `json:"episodeCount"`
			TotalEpisodeCount int   `json:"totalEpisodeCount"`
			SizeOnDisk        int64 `json:"sizeOnDisk"`
		} `json:"statistics"`
	} `json:"seasons"`
	Statistics *struct {
		EpisodeFileCount  int   `json:"episodeFileCount"`
		EpisodeCount      int   `json:"episodeCount"`
		TotalEpisodeCount int   `json:"totalEpisodeCount"`
		SizeOnDisk        int64 `json:"sizeOnDisk"`
	} `json:"statistics"`
	Ratings *struct {
		Value float64 `json:"value"`
	} `json:"ratings"`
}

// Episode is a Sonarr episode.
type Episode struct {
	ID                    int       `json:"id"`
	SeriesID              int       `json:"seriesId"`
	SeasonNumber          int       `json:"seasonNumber"`
	EpisodeNumber         int       `json:"episodeNumber"`
	AbsoluteEpisodeNumber int       `json:"absoluteEpisodeNumber"`
	Title                 string    `json:"title"`
	AirDateUTC            time.Time `json:"airDateUtc"`
	HasFile               bool      `json:"hasFile"`
	Monitored             bool      `json:"monitored"`
	Runtime               int       `json:"runtime"`
	EpisodeFileID         int       `json:"episodeFileId"`
}

// Aired reports whether the episode has aired.
func (e Episode) Aired() bool { return !e.AirDateUTC.IsZero() && e.AirDateUTC.Before(time.Now()) }

// AllSeries lists the library.
func (s *Sonarr) AllSeries(ctx context.Context) ([]Series, error) {
	var out []Series
	err := s.get(ctx, "/series", nil, &out)
	return out, err
}

// Series fetches one series.
func (s *Sonarr) Series(ctx context.Context, id int) (Series, error) {
	var out Series
	err := s.get(ctx, "/series/"+strconv.Itoa(id), nil, &out)
	return out, err
}

// Episodes lists every episode of a series.
func (s *Sonarr) Episodes(ctx context.Context, seriesID int) ([]Episode, error) {
	var out []Episode
	err := s.get(ctx, "/episode", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, &out)
	return out, err
}

// Lookup searches TVDB through Sonarr's metadata proxy.
func (s *Sonarr) Lookup(ctx context.Context, term string) ([]Series, error) {
	var out []Series
	err := s.get(ctx, "/series/lookup", url.Values{"term": {term}}, &out)
	return out, err
}

// LookupRaw returns the raw lookup JSON (needed verbatim to add a series).
func (s *Sonarr) LookupRaw(ctx context.Context, term string) ([]map[string]any, error) {
	var out []map[string]any
	err := s.get(ctx, "/series/lookup", url.Values{"term": {term}}, &out)
	return out, err
}

// AddOptions for a new series.
type AddSeries struct {
	TvdbID           int
	QualityProfileID int
	RootFolder       string
	Anime            bool
	// Monitor is Sonarr's monitor preset: "none", "all", "future", ...
	Monitor string
}

// Add adds a series to Sonarr without triggering a Sonarr search (roarr does
// the searching).
func (s *Sonarr) Add(ctx context.Context, a AddSeries) (Series, error) {
	hits, err := s.LookupRaw(ctx, fmt.Sprintf("tvdb:%d", a.TvdbID))
	if err != nil {
		return Series{}, err
	}
	if len(hits) == 0 {
		return Series{}, fmt.Errorf("sonarr: no series with tvdb id %d", a.TvdbID)
	}
	body := hits[0]
	if id, _ := body["id"].(float64); id > 0 {
		return s.Series(ctx, int(id))
	}
	body["qualityProfileId"] = a.QualityProfileID
	body["rootFolderPath"] = a.RootFolder
	body["monitored"] = true
	body["seasonFolder"] = true
	if a.Anime {
		body["seriesType"] = "anime"
	}
	monitor := a.Monitor
	if monitor == "" {
		monitor = "none"
	}
	body["addOptions"] = map[string]any{"monitor": monitor, "searchForMissingEpisodes": false, "searchForCutoffUnmetEpisodes": false}
	var out Series
	err = s.send(ctx, http.MethodPost, "/series", body, &out)
	return out, err
}

// MonitorEpisodes sets monitored state for episodes.
func (s *Sonarr) MonitorEpisodes(ctx context.Context, ids []int, monitored bool) error {
	if len(ids) == 0 {
		return nil
	}
	return s.send(ctx, http.MethodPut, "/episode/monitor", map[string]any{"episodeIds": ids, "monitored": monitored}, nil)
}

// EnsureSeasonsMonitored flips the season-level monitored flag on so that
// Sonarr's decision engine accepts season packs for them.
func (s *Sonarr) EnsureSeasonsMonitored(ctx context.Context, seriesID int, seasons []int) error {
	var raw map[string]any
	if err := s.get(ctx, "/series/"+strconv.Itoa(seriesID), nil, &raw); err != nil {
		return err
	}
	want := map[int]bool{}
	for _, n := range seasons {
		want[n] = true
	}
	changed := false
	if m, _ := raw["monitored"].(bool); !m {
		raw["monitored"] = true
		changed = true
	}
	list, _ := raw["seasons"].([]any)
	for _, it := range list {
		season, _ := it.(map[string]any)
		n, _ := season["seasonNumber"].(float64)
		if want[int(n)] {
			if m, _ := season["monitored"].(bool); !m {
				season["monitored"] = true
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	// Changing the season flag would monitor every episode in it; keep the
	// episode-level choices untouched by not cascading.
	return s.Do(ctx, http.MethodPut, s.prefix+"/series/"+strconv.Itoa(seriesID), url.Values{"moveFiles": {"false"}}, raw, nil)
}

// ManualImportItem is one file Sonarr can see in a download folder.
type ManualImportItem struct {
	ID           int             `json:"id"`
	Path         string          `json:"path"`
	RelativePath string          `json:"relativePath"`
	FolderName   string          `json:"folderName"`
	Name         string          `json:"name"`
	Size         int64           `json:"size"`
	Series       *Series         `json:"series"`
	SeasonNumber *int            `json:"seasonNumber"`
	Episodes     []Episode       `json:"episodes"`
	Quality      json.RawMessage `json:"quality"`
	Languages    json.RawMessage `json:"languages"`
	ReleaseGroup string          `json:"releaseGroup"`
	DownloadID   string          `json:"downloadId"`
	IndexerFlags int             `json:"indexerFlags"`
	ReleaseType  string          `json:"releaseType"`
	Rejections   []struct {
		Reason string `json:"reason"`
		Type   string `json:"type"`
	} `json:"rejections"`
}

// ManualImportCandidates lists files Sonarr could import right now, either
// from a download it tracks (downloadID) or from a folder. Do not pass a
// series id: with one, Sonarr scans the series' library folder instead.
func (s *Sonarr) ManualImportCandidates(ctx context.Context, downloadID, folder string) ([]ManualImportItem, error) {
	q := url.Values{"filterExistingFiles": {"true"}}
	if downloadID != "" {
		q.Set("downloadId", downloadID)
	}
	if folder != "" {
		q.Set("folder", folder)
	}
	var out []ManualImportItem
	err := s.get(ctx, "/manualimport", q, &out)
	return out, err
}

// ImportFiles runs a ManualImport command in copy mode (hardlink when the
// filesystem allows) so a torrent that is still downloading keeps seeding.
func (s *Sonarr) ImportFiles(ctx context.Context, items []ManualImportItem, seriesID int) (Command, error) {
	files := make([]map[string]any, 0, len(items))
	for _, it := range items {
		ids := make([]int, 0, len(it.Episodes))
		for _, e := range it.Episodes {
			ids = append(ids, e.ID)
		}
		f := map[string]any{
			"path":         it.Path,
			"folderName":   it.FolderName,
			"seriesId":     seriesID,
			"episodeIds":   ids,
			"quality":      it.Quality,
			"languages":    it.Languages,
			"releaseGroup": it.ReleaseGroup,
			"downloadId":   it.DownloadID,
			"indexerFlags": it.IndexerFlags,
			"releaseType":  it.ReleaseType,
		}
		files = append(files, f)
	}
	return s.RunCommand(ctx, map[string]any{"name": "ManualImport", "files": files, "importMode": "copy"})
}

// RescanSeries asks Sonarr to rescan a series folder.
func (s *Sonarr) RescanSeries(ctx context.Context, seriesID int) error {
	_, err := s.RunCommand(ctx, map[string]any{"name": "RescanSeries", "seriesId": seriesID})
	return err
}
