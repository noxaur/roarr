package arr

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Import is one "downloadFolderImported" history event: a file that was
// moved or linked into the library.
type Import struct {
	ID       int    // history record id (grows with time)
	SeriesID int    // Sonarr
	MovieID  int    // Radarr
	Path     string // file in the library, as the *arr sees it
}

// RecentImports lists one page (1-based) of import events, newest first.
// Event type 3 is downloadFolderImported in both Sonarr and Radarr.
func (c *Client) RecentImports(ctx context.Context, page, pageSize int) ([]Import, error) {
	var res struct {
		Records []struct {
			ID       int `json:"id"`
			SeriesID int `json:"seriesId"`
			MovieID  int `json:"movieId"`
			Data     struct {
				ImportedPath string `json:"importedPath"`
			} `json:"data"`
		} `json:"records"`
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}, "sortKey": {"date"}, "sortDirection": {"descending"}, "eventType": {"3"}}
	if err := c.get(ctx, "/history", q, &res); err != nil {
		return nil, err
	}
	out := make([]Import, 0, len(res.Records))
	for _, r := range res.Records {
		out = append(out, Import{ID: r.ID, SeriesID: r.SeriesID, MovieID: r.MovieID, Path: r.Data.ImportedPath})
	}
	return out, nil
}

// HistoryRecord is one *arr history event.
type HistoryRecord struct {
	ID          int               `json:"id"`
	EpisodeID   int               `json:"episodeId"`
	MovieID     int               `json:"movieId"`
	SourceTitle string            `json:"sourceTitle"`
	EventType   string            `json:"eventType"` // grabbed | downloadFolderImported | seriesFolderImported | movieFolderImported | episodeFileDeleted | ...
	Date        time.Time         `json:"date"`
	DownloadID  string            `json:"downloadId"`
	Data        map[string]string `json:"data"` // importedPath, fileId, droppedPath, ...
}

// Field reads a data value; key case varies between *arr versions.
func (h HistoryRecord) Field(key string) string {
	if v, ok := h.Data[key]; ok {
		return v
	}
	for k, v := range h.Data {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// SeriesHistory lists the history of a series. Sonarr ignores an episodeId
// filter on this endpoint, so callers filter by EpisodeID themselves.
func (s *Sonarr) SeriesHistory(ctx context.Context, seriesID int) ([]HistoryRecord, error) {
	var out []HistoryRecord
	err := s.get(ctx, "/history/series", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, &out)
	return out, err
}

// MovieHistory lists the history of one movie.
func (r *Radarr) MovieHistory(ctx context.Context, movieID int) ([]HistoryRecord, error) {
	var out []HistoryRecord
	err := r.get(ctx, "/history/movie", url.Values{"movieId": {strconv.Itoa(movieID)}}, &out)
	return out, err
}
