package arr

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
