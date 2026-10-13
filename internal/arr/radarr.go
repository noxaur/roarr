package arr

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Radarr is a Radarr v5+ (API v3) client.
type Radarr struct{ *Client }

// NewRadarr returns a Radarr client.
func NewRadarr(base, key string) *Radarr { return &Radarr{newClient("radarr", base, key, "/api/v3")} }

// Movie is a Radarr movie.
type Movie struct {
	ID               int       `json:"id"`
	Title            string    `json:"title"`
	OriginalTitle    string    `json:"originalTitle"`
	Year             int       `json:"year"`
	TmdbID           int       `json:"tmdbId"`
	ImdbID           string    `json:"imdbId"`
	Overview         string    `json:"overview"`
	Studio           string    `json:"studio"`
	Status           string    `json:"status"`
	Runtime          int       `json:"runtime"`
	HasFile          bool      `json:"hasFile"`
	Monitored        bool      `json:"monitored"`
	IsAvailable      bool      `json:"isAvailable"`
	SizeOnDisk       int64     `json:"sizeOnDisk"`
	Path             string    `json:"path"`
	QualityProfileID int       `json:"qualityProfileId"`
	Images           []Image   `json:"images"`
	Genres           []string  `json:"genres"`
	InCinemas        time.Time `json:"inCinemas"`
	DigitalRelease   time.Time `json:"digitalRelease"`
	Collection       *struct {
		TmdbID int    `json:"tmdbId"`
		Title  string `json:"title"`
	} `json:"collection"`
	AlternateTitles []struct {
		Title string `json:"title"`
	} `json:"alternateTitles"`
	Ratings *struct {
		Tmdb *struct {
			Value float64 `json:"value"`
		} `json:"tmdb"`
	} `json:"ratings"`
}

// Movies lists the library.
func (r *Radarr) Movies(ctx context.Context) ([]Movie, error) {
	var out []Movie
	err := r.get(ctx, "/movie", nil, &out)
	return out, err
}

// Movie fetches one movie.
func (r *Radarr) Movie(ctx context.Context, id int) (Movie, error) {
	var out Movie
	err := r.get(ctx, "/movie/"+strconv.Itoa(id), nil, &out)
	return out, err
}

// Lookup searches TMDB through Radarr.
func (r *Radarr) Lookup(ctx context.Context, term string) ([]Movie, error) {
	var out []Movie
	err := r.get(ctx, "/movie/lookup", url.Values{"term": {term}}, &out)
	return out, err
}

// LookupTmdb fetches movie metadata by TMDB id.
func (r *Radarr) LookupTmdb(ctx context.Context, tmdbID int) (map[string]any, error) {
	var out map[string]any
	err := r.get(ctx, "/movie/lookup/tmdb", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, &out)
	return out, err
}

// AddMovie adds a movie without triggering a Radarr search.
func (r *Radarr) AddMovie(ctx context.Context, tmdbID, profileID int, root string) (Movie, error) {
	body, err := r.LookupTmdb(ctx, tmdbID)
	if err != nil {
		return Movie{}, err
	}
	if id, _ := body["id"].(float64); id > 0 {
		return r.Movie(ctx, int(id))
	}
	body["qualityProfileId"] = profileID
	body["rootFolderPath"] = root
	body["monitored"] = true
	body["minimumAvailability"] = "released"
	body["addOptions"] = map[string]any{"searchForMovie": false, "monitor": "movieOnly"}
	var out Movie
	err = r.send(ctx, http.MethodPost, "/movie", body, &out)
	return out, err
}

// Collection is a TMDB collection as Radarr knows it.
type Collection struct {
	ID       int     `json:"id"`
	Title    string  `json:"title"`
	TmdbID   int     `json:"tmdbId"`
	Overview string  `json:"overview"`
	Images   []Image `json:"images"`
	Movies   []struct {
		TmdbID   int     `json:"tmdbId"`
		ImdbID   string  `json:"imdbId"`
		Title    string  `json:"title"`
		Year     int     `json:"year"`
		Runtime  int     `json:"runtime"`
		Overview string  `json:"overview"`
		Images   []Image `json:"images"`
	} `json:"movies"`
}

// Collection fetches a collection by TMDB id (Radarr only knows
// collections of movies already in the library).
func (r *Radarr) Collection(ctx context.Context, tmdbID int) (*Collection, error) {
	var out []Collection
	if err := r.get(ctx, "/collection", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, &out); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].TmdbID == tmdbID {
			return &out[i], nil
		}
	}
	return nil, nil
}

// Monitor sets a movie monitored.
func (r *Radarr) Monitor(ctx context.Context, id int) error {
	return r.send(ctx, http.MethodPut, "/movie/editor", map[string]any{"movieIds": []int{id}, "monitored": true}, nil)
}

// SetQualityProfile moves a movie to a quality profile.
func (r *Radarr) SetQualityProfile(ctx context.Context, movieID, profileID int) error {
	return r.send(ctx, http.MethodPut, "/movie/editor", map[string]any{"movieIds": []int{movieID}, "qualityProfileId": profileID}, nil)
}
