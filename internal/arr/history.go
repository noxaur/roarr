package arr

import (
	"context"
	"net/url"
	"strconv"
)

// Import is one "downloadFolderImported" history event: a file that was
// moved or linked into the library.
type Import struct {
	ID       int    // history record id (grows with time)
	SeriesID int    // Sonarr
	MovieID  int    // Radarr
	Path     string // file in the library, as the *arr sees it
}

// RecentImports lists the newest import events, newest first. Event type 3
// is downloadFolderImported in both Sonarr and Radarr.
func (c *Client) RecentImports(ctx context.Context, pageSize int) ([]Import, error) {
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
	q := url.Values{"page": {"1"}, "pageSize": {strconv.Itoa(pageSize)}, "sortKey": {"date"}, "sortDirection": {"descending"}, "eventType": {"3"}}
	if err := c.get(ctx, "/history", q, &res); err != nil {
		return nil, err
	}
	out := make([]Import, 0, len(res.Records))
	for _, r := range res.Records {
		out = append(out, Import{ID: r.ID, SeriesID: r.SeriesID, MovieID: r.MovieID, Path: r.Data.ImportedPath})
	}
	return out, nil
}
