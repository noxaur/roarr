// Package seerr talks to Seerr (the merged Overseerr/Jellyseerr). roarr uses
// it for fast TMDB-backed discovery and for the request inbox.
package seerr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/httpx"
)

// Client is a Seerr API client.
type Client struct{ *httpx.Client }

// New returns a client.
func New(base, key string) *Client {
	h := http.Header{}
	h.Set("X-Api-Key", key)
	return &Client{httpx.New("seerr", base, h)}
}

// TMDB image base for posters.
const ImageBase = "https://image.tmdb.org/t/p/"

// Poster turns a TMDB path into a URL.
func Poster(path, size string) string {
	if path == "" {
		return ""
	}
	return ImageBase + size + path
}

// MediaInfo is Seerr's library/request state for a title.
type MediaInfo struct {
	ID     int `json:"id"`
	TmdbID int `json:"tmdbId"`
	TvdbID int `json:"tvdbId"`
	// 1 unknown, 2 pending, 3 processing, 4 partially available, 5 available
	Status int `json:"status"`
}

// Result is one search hit.
type Result struct {
	ID           int        `json:"id"` // TMDB id
	MediaType    string     `json:"mediaType"`
	Title        string     `json:"title"`
	Name         string     `json:"name"`
	ReleaseDate  string     `json:"releaseDate"`
	FirstAirDate string     `json:"firstAirDate"`
	PosterPath   string     `json:"posterPath"`
	BackdropPath string     `json:"backdropPath"`
	Overview     string     `json:"overview"`
	VoteAverage  float64    `json:"voteAverage"`
	Popularity   float64    `json:"popularity"`
	MediaInfo    *MediaInfo `json:"mediaInfo"`
}

// DisplayTitle returns title or name.
func (r Result) DisplayTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

// Year returns the release/first-air year.
func (r Result) Year() int {
	d := r.ReleaseDate
	if d == "" {
		d = r.FirstAirDate
	}
	if len(d) >= 4 {
		y, _ := strconv.Atoi(d[:4])
		return y
	}
	return 0
}

// Status pings Seerr.
func (c *Client) Status(ctx context.Context) (string, error) {
	var s struct {
		Version string `json:"version"`
	}
	err := c.Get(ctx, "/api/v1/status", nil, &s)
	return s.Version, err
}

// Search queries TMDB through Seerr. Seerr rejects '+' for spaces, so the
// query is percent-encoded by hand.
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	var res struct {
		Results []Result `json:"results"`
	}
	path := "/api/v1/search?query=" + strings.ReplaceAll(url.QueryEscape(query), "+", "%20") + "&page=1&language=en"
	err := c.Get(ctx, path, nil, &res)
	return res.Results, err
}

// TV is the subset of /tv/{id} roarr needs.
type TV struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"originalName"`
	FirstAirDate string `json:"firstAirDate"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"posterPath"`
	BackdropPath string `json:"backdropPath"`
	Status       string `json:"status"`
	Genres       []struct {
		Name string `json:"name"`
	} `json:"genres"`
	ExternalIDs struct {
		TvdbID int    `json:"tvdbId"`
		ImdbID string `json:"imdbId"`
	} `json:"externalIds"`
	Seasons []struct {
		SeasonNumber int    `json:"seasonNumber"`
		EpisodeCount int    `json:"episodeCount"`
		Name         string `json:"name"`
		AirDate      string `json:"airDate"`
	} `json:"seasons"`
	Keywords []struct {
		Name string `json:"name"`
	} `json:"keywords"`
	MediaInfo *MediaInfo `json:"mediaInfo"`
}

// TV fetches TV details by TMDB id.
func (c *Client) TV(ctx context.Context, tmdbID int) (TV, error) {
	var out TV
	err := c.Get(ctx, fmt.Sprintf("/api/v1/tv/%d", tmdbID), nil, &out)
	return out, err
}

// Year is the first-air year.
func (t TV) Year() int { return year(t.FirstAirDate) }

func year(d string) int {
	if len(d) >= 4 {
		y, _ := strconv.Atoi(d[:4])
		return y
	}
	return 0
}

// SeasonEpisode is one episode of a TMDB season.
type SeasonEpisode struct {
	EpisodeNumber int    `json:"episodeNumber"`
	SeasonNumber  int    `json:"seasonNumber"`
	Name          string `json:"name"`
	AirDate       string `json:"airDate"`
}

// Season fetches the episode list of one season.
func (c *Client) Season(ctx context.Context, tmdbID, season int) ([]SeasonEpisode, error) {
	var out struct {
		Episodes []SeasonEpisode `json:"episodes"`
	}
	err := c.Get(ctx, fmt.Sprintf("/api/v1/tv/%d/season/%d", tmdbID, season), nil, &out)
	return out.Episodes, err
}

// IsAnime uses TMDB keywords to detect anime.
func (t TV) IsAnime() bool {
	for _, k := range t.Keywords {
		if strings.EqualFold(k.Name, "anime") {
			return true
		}
	}
	return false
}

// Movie is the subset of /movie/{id} roarr needs.
type Movie struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle"`
	ReleaseDate   string `json:"releaseDate"`
	Overview      string `json:"overview"`
	PosterPath    string `json:"posterPath"`
	BackdropPath  string `json:"backdropPath"`
	Runtime       int    `json:"runtime"`
	Status        string `json:"status"`
	Genres        []struct {
		Name string `json:"name"`
	} `json:"genres"`
	ImdbID      string `json:"imdbId"`
	ExternalIDs struct {
		ImdbID string `json:"imdbId"`
	} `json:"externalIds"`
	Collection *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"collection"`
	MediaInfo *MediaInfo `json:"mediaInfo"`
}

// Year is the release year.
func (m Movie) Year() int { return year(m.ReleaseDate) }

// Movie fetches movie details by TMDB id.
func (c *Client) Movie(ctx context.Context, tmdbID int) (Movie, error) {
	var out Movie
	err := c.Get(ctx, fmt.Sprintf("/api/v1/movie/%d", tmdbID), nil, &out)
	return out, err
}

// Collection is a TMDB collection.
type Collection struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Overview     string   `json:"overview"`
	PosterPath   string   `json:"posterPath"`
	BackdropPath string   `json:"backdropPath"`
	Parts        []Result `json:"parts"`
}

// Collection fetches a collection by TMDB id.
func (c *Client) Collection(ctx context.Context, id int) (Collection, error) {
	var out Collection
	err := c.Get(ctx, fmt.Sprintf("/api/v1/collection/%d", id), nil, &out)
	return out, err
}

// Request is a user request.
type Request struct {
	ID        int       `json:"id"`
	Status    int       `json:"status"` // 1 pending, 2 approved, 3 declined, 4 failed, 5 completed
	Type      string    `json:"type"`   // movie | tv
	Is4K      bool      `json:"is4k"`
	CreatedAt time.Time `json:"createdAt"`
	Media     struct {
		TmdbID    int    `json:"tmdbId"`
		TvdbID    int    `json:"tvdbId"`
		MediaType string `json:"mediaType"`
		Status    int    `json:"status"`
	} `json:"media"`
	Seasons []struct {
		SeasonNumber int `json:"seasonNumber"`
		Status       int `json:"status"`
	} `json:"seasons"`
	RequestedBy struct {
		DisplayName string `json:"displayName"`
		Avatar      string `json:"avatar"`
	} `json:"requestedBy"`
}

// Requests lists requests. filter: all|pending|approved|processing|available|unavailable|failed.
func (c *Client) Requests(ctx context.Context, filter string, take int) ([]Request, error) {
	var res struct {
		Results []Request `json:"results"`
	}
	q := url.Values{"take": {strconv.Itoa(take)}, "skip": {"0"}, "filter": {filter}, "sort": {"added"}}
	err := c.Get(ctx, "/api/v1/request", q, &res)
	return res.Results, err
}

// Approve approves a request (Seerr then adds it to Sonarr/Radarr).
func (c *Client) Approve(ctx context.Context, id int) error {
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/request/%d/approve", id), nil, nil, nil)
}

// Decline declines a request.
func (c *Client) Decline(ctx context.Context, id int) error {
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/request/%d/decline", id), nil, nil, nil)
}

// Trending returns trending titles for an empty search page.
func (c *Client) Trending(ctx context.Context) ([]Result, error) {
	var res struct {
		Results []Result `json:"results"`
	}
	err := c.Get(ctx, "/api/v1/discover/trending?page=1&language=en", nil, &res)
	return res.Results, err
}
