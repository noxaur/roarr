package arr

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Prowlarr is a Prowlarr (API v1) client.
type Prowlarr struct{ *Client }

// NewProwlarr returns a Prowlarr client.
func NewProwlarr(base, key string) *Prowlarr {
	return &Prowlarr{newClient("prowlarr", base, key, "/api/v1")}
}

// Indexer is a Prowlarr indexer with the search capabilities roarr needs to
// build the right query for it.
type Indexer struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Enable       bool   `json:"enable"`
	Protocol     string `json:"protocol"`
	Privacy      string `json:"privacy"`
	Priority     int    `json:"priority"`
	Capabilities struct {
		SearchParams      []string `json:"searchParams"`
		TvSearchParams    []string `json:"tvSearchParams"`
		MovieSearchParams []string `json:"movieSearchParams"`
		LimitsDefault     int      `json:"limitsDefault"`
		LimitsMax         int      `json:"limitsMax"`
		Categories        []struct {
			ID int `json:"id"`
		} `json:"categories"`
	} `json:"capabilities"`
}

// SupportsTV reports tv-search support.
func (i Indexer) SupportsTV() bool { return len(i.Capabilities.TvSearchParams) > 0 }

// SupportsMovies reports movie-search support.
func (i Indexer) SupportsMovies() bool { return len(i.Capabilities.MovieSearchParams) > 0 }

// HasCategory reports whether the indexer carries any category in [lo, hi).
func (i Indexer) HasCategory(lo, hi int) bool {
	if len(i.Capabilities.Categories) == 0 {
		return true
	}
	for _, c := range i.Capabilities.Categories {
		if c.ID >= lo && c.ID < hi {
			return true
		}
	}
	return false
}

// TVParam reports whether a tv-search parameter is supported (case-insensitive).
func (i Indexer) TVParam(p string) bool { return hasFold(i.Capabilities.TvSearchParams, p) }

// MovieParam reports whether a movie-search parameter is supported.
func (i Indexer) MovieParam(p string) bool { return hasFold(i.Capabilities.MovieSearchParams, p) }

func hasFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// AllIndexers lists indexers with capabilities.
func (p *Prowlarr) AllIndexers(ctx context.Context) ([]Indexer, error) {
	var out []Indexer
	err := p.get(ctx, "/indexer", nil, &out)
	return out, err
}

// IndexerStatus lists indexers Prowlarr has temporarily disabled after failures.
func (p *Prowlarr) DisabledIndexers(ctx context.Context) (map[int]time.Time, error) {
	var out []struct {
		IndexerID    int       `json:"indexerId"`
		DisabledTill time.Time `json:"disabledTill"`
	}
	if err := p.get(ctx, "/indexerstatus", nil, &out); err != nil {
		return nil, err
	}
	m := make(map[int]time.Time, len(out))
	for _, s := range out {
		if s.DisabledTill.After(time.Now()) {
			m[s.IndexerID] = s.DisabledTill
		}
	}
	return m, nil
}

// SearchResult is one release from Prowlarr's search API.
type SearchResult struct {
	GUID        string    `json:"guid"`
	Title       string    `json:"title"`
	Size        int64     `json:"size"`
	IndexerID   int       `json:"indexerId"`
	Indexer     string    `json:"indexer"`
	PublishDate time.Time `json:"publishDate"`
	DownloadURL string    `json:"downloadUrl"`
	MagnetURL   string    `json:"magnetUrl"`
	InfoURL     string    `json:"infoUrl"`
	InfoHash    string    `json:"infoHash"`
	Seeders     *int      `json:"seeders"`
	Leechers    *int      `json:"leechers"`
	Grabs       int       `json:"grabs"`
	Protocol    string    `json:"protocol"`
	ImdbID      int       `json:"imdbId"`
	TmdbID      int       `json:"tmdbId"`
	TvdbID      int       `json:"tvdbId"`
	Categories  []struct {
		ID int `json:"id"`
	} `json:"categories"`
	IndexerFlags []string `json:"indexerFlags"`
}

// Query is one search against one indexer. Prowlarr's query string accepts
// {TvdbId:..}{Season:..}{Episode:..}{ImdbId:..}{TmdbId:..} tokens which it
// translates to native indexer parameters.
type Query struct {
	IndexerID  int
	Type       string // search | tvsearch | movie
	Term       string // free text (may include tokens)
	Categories []int
	Limit      int
	Offset     int
}

// Key is a stable cache key for the query.
func (q Query) Key() string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(q.IndexerID))
	b.WriteByte('|')
	b.WriteString(q.Type)
	b.WriteByte('|')
	b.WriteString(strings.ToLower(q.Term))
	b.WriteByte('|')
	for _, c := range q.Categories {
		b.WriteString(strconv.Itoa(c))
		b.WriteByte(',')
	}
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(q.Offset))
	return b.String()
}

// Search runs a query against a single indexer.
func (p *Prowlarr) Search(ctx context.Context, q Query) ([]SearchResult, error) {
	v := url.Values{}
	v.Set("query", q.Term)
	v.Set("type", q.Type)
	v.Add("indexerIds", strconv.Itoa(q.IndexerID))
	for _, c := range q.Categories {
		v.Add("categories", strconv.Itoa(c))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	var out []SearchResult
	err := p.get(ctx, "/search", v, &out)
	return out, err
}

// Applications lists Prowlarr app links (Sonarr/Radarr), used to discover
// their URLs when the user only configured Prowlarr.
func (p *Prowlarr) Applications(ctx context.Context) ([]Provider, error) {
	var out []Provider
	err := p.get(ctx, "/applications", nil, &out)
	return out, err
}
