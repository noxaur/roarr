package arr

import (
	"context"
	"net/url"
	"strconv"
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
	Data        map[string]string `json:"data"` // importedPath, droppedPath, ...
}

// EpisodeHistory lists the history of one episode.
func (s *Sonarr) EpisodeHistory(ctx context.Context, seriesID, episodeID int) ([]HistoryRecord, error) {
	var out []HistoryRecord
	q := url.Values{"seriesId": {strconv.Itoa(seriesID)}, "episodeId": {strconv.Itoa(episodeID)}}
	err := s.get(ctx, "/history/series", q, &out)
	return out, err
}

// MovieHistory lists the history of one movie.
func (r *Radarr) MovieHistory(ctx context.Context, movieID int) ([]HistoryRecord, error) {
	var out []HistoryRecord
	err := r.get(ctx, "/history/movie", url.Values{"movieId": {strconv.Itoa(movieID)}}, &out)
	return out, err
}
