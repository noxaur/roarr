// Package arr talks to Sonarr, Radarr and Prowlarr. The three share the
// Servarr API conventions (X-Api-Key, /api/v3 or /api/v1, paged queue).
package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/httpx"
)

// Client is a Servarr API client.
type Client struct {
	*httpx.Client
	prefix string
}

func newClient(service, base, key, prefix string) *Client {
	h := http.Header{}
	h.Set("X-Api-Key", key)
	return &Client{Client: httpx.New(service, base, h), prefix: prefix}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.Do(ctx, http.MethodGet, c.prefix+path, q, nil, out)
}

func (c *Client) send(ctx context.Context, method, path string, body, out any) error {
	return c.Do(ctx, method, c.prefix+path, nil, body, out)
}

// Status is /system/status.
type Status struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

// Status pings the service.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	err := c.get(ctx, "/system/status", nil, &s)
	return s, err
}

// Image is an artwork reference.
type Image struct {
	CoverType string `json:"coverType"`
	URL       string `json:"url"`
	RemoteURL string `json:"remoteUrl"`
}

// Poster picks the best poster URL (remote TMDB/TVDB artwork when present).
func Poster(imgs []Image) string {
	for _, i := range imgs {
		if i.CoverType == "poster" {
			if i.RemoteURL != "" {
				return smallTMDB(i.RemoteURL, "w342")
			}
			return i.URL
		}
	}
	return ""
}

// smallTMDB swaps TMDB's multi-megabyte "original" size for a thumbnail.
func smallTMDB(u, size string) string {
	return strings.Replace(u, "image.tmdb.org/t/p/original/", "image.tmdb.org/t/p/"+size+"/", 1)
}

// Fanart picks the backdrop.
func Fanart(imgs []Image) string {
	for _, i := range imgs {
		if i.CoverType == "fanart" {
			if i.RemoteURL != "" {
				return smallTMDB(i.RemoteURL, "w1280")
			}
			return i.URL
		}
	}
	return ""
}

// Command is a queued *arr command.
type Command struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"` // queued, started, completed, failed
	Message string `json:"message"`
	Result  string `json:"result"`
}

// RunCommand posts a command.
func (c *Client) RunCommand(ctx context.Context, body map[string]any) (Command, error) {
	var cmd Command
	err := c.send(ctx, http.MethodPost, "/command", body, &cmd)
	return cmd, err
}

// GetCommand polls a command.
func (c *Client) GetCommand(ctx context.Context, id int) (Command, error) {
	var cmd Command
	err := c.get(ctx, "/command/"+strconv.Itoa(id), nil, &cmd)
	return cmd, err
}

// WaitCommand polls until a command finishes or ctx expires.
func (c *Client) WaitCommand(ctx context.Context, id int) (Command, error) {
	for {
		cmd, err := c.GetCommand(ctx, id)
		if err != nil {
			return cmd, err
		}
		switch cmd.Status {
		case "completed", "failed", "aborted", "cancelled", "orphaned":
			return cmd, nil
		}
		select {
		case <-ctx.Done():
			return cmd, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
}

// Field is a provider settings field (indexers, download clients).
type Field struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// Provider is an indexer / download client / application definition.
type Provider struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Implementation string  `json:"implementation"`
	Enable         bool    `json:"enable"`
	Protocol       string  `json:"protocol"`
	Priority       int     `json:"priority"`
	Fields         []Field `json:"fields"`
}

// FieldString returns a string field value.
func (p Provider) FieldString(name string) string {
	for _, f := range p.Fields {
		if f.Name == name {
			var s string
			if json.Unmarshal(f.Value, &s) == nil {
				return s
			}
			return strings.Trim(string(f.Value), `"`)
		}
	}
	return ""
}

// FieldBool returns a boolean field value.
func (p Provider) FieldBool(name string) bool {
	return p.FieldString(name) == "true"
}

// DownloadClients lists configured download clients.
func (c *Client) DownloadClients(ctx context.Context) ([]Provider, error) {
	var out []Provider
	err := c.get(ctx, "/downloadclient", nil, &out)
	return out, err
}

// Indexers lists configured indexers.
func (c *Client) Indexers(ctx context.Context) ([]Provider, error) {
	var out []Provider
	err := c.get(ctx, "/indexer", nil, &out)
	return out, err
}

// QualityProfile is a quality profile with the allowed quality names.
type QualityProfile struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Items []struct {
		Allowed bool `json:"allowed"`
		Quality *struct {
			Name       string `json:"name"`
			Resolution int    `json:"resolution"`
		} `json:"quality"`
		Items []struct {
			Allowed bool `json:"allowed"`
			Quality *struct {
				Name       string `json:"name"`
				Resolution int    `json:"resolution"`
			} `json:"quality"`
		} `json:"items"`
	} `json:"items"`
}

// QualityProfiles lists quality profiles.
func (c *Client) QualityProfiles(ctx context.Context) ([]QualityProfile, error) {
	var out []QualityProfile
	err := c.get(ctx, "/qualityprofile", nil, &out)
	return out, err
}

// RootFolder is a library root.
type RootFolder struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	FreeSpace  int64  `json:"freeSpace"`
	Accessible bool   `json:"accessible"`
}

// RootFolders lists library roots.
func (c *Client) RootFolders(ctx context.Context) ([]RootFolder, error) {
	var out []RootFolder
	err := c.get(ctx, "/rootfolder", nil, &out)
	return out, err
}

// PushRelease is the body for POST /release/push.
type PushRelease struct {
	Title       string    `json:"title"`
	DownloadURL string    `json:"downloadUrl,omitempty"`
	MagnetURL   string    `json:"magnetUrl,omitempty"`
	InfoURL     string    `json:"infoUrl,omitempty"`
	GUID        string    `json:"guid,omitempty"`
	Protocol    string    `json:"protocol"`
	PublishDate time.Time `json:"publishDate"`
	Size        int64     `json:"size,omitempty"`
	Indexer     string    `json:"indexer,omitempty"`
	IndexerID   int       `json:"indexerId,omitempty"`
	Seeders     *int      `json:"seeders,omitempty"`
	Leechers    *int      `json:"leechers,omitempty"`
	InfoHash    string    `json:"infoHash,omitempty"`
	// Radarr/Sonarr accept these hints to skip parsing ambiguity.
	SeriesID   int    `json:"seriesId,omitempty"`
	TvdbID     int    `json:"tvdbId,omitempty"`
	MovieID    int    `json:"movieId,omitempty"`
	TmdbID     int    `json:"tmdbId,omitempty"`
	ImdbID     string `json:"imdbId,omitempty"`
	EpisodeIDs []int  `json:"episodeIds,omitempty"`
}

// PushDecision is one decision returned by /release/push.
type PushDecision struct {
	GUID            string   `json:"guid"`
	Title           string   `json:"title"`
	Approved        bool     `json:"approved"`
	Rejected        bool     `json:"rejected"`
	Rejections      []string `json:"rejections"`
	DownloadAllowed bool     `json:"downloadAllowed"`
}

// Push sends a release to the decision engine. If accepted, the *arr sends it
// to its download client immediately and tracks it for import.
func (c *Client) Push(ctx context.Context, r PushRelease) ([]PushDecision, error) {
	var raw json.RawMessage
	if err := c.send(ctx, http.MethodPost, "/release/push", r, &raw); err != nil {
		return nil, err
	}
	return decodeDecisions(raw)
}

func decodeDecisions(raw json.RawMessage) ([]PushDecision, error) {
	type wire struct {
		GUID            string            `json:"guid"`
		Title           string            `json:"title"`
		Approved        bool              `json:"approved"`
		Rejected        bool              `json:"rejected"`
		Rejections      []json.RawMessage `json:"rejections"`
		DownloadAllowed bool              `json:"downloadAllowed"`
	}
	var list []wire
	if len(raw) > 0 && raw[0] == '{' {
		var one wire
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, err
		}
		list = []wire{one}
	} else if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]PushDecision, 0, len(list))
	for _, w := range list {
		d := PushDecision{GUID: w.GUID, Title: w.Title, Approved: w.Approved, Rejected: w.Rejected, DownloadAllowed: w.DownloadAllowed}
		for _, r := range w.Rejections {
			var s string
			if json.Unmarshal(r, &s) == nil {
				d.Rejections = append(d.Rejections, s)
				continue
			}
			var o struct {
				Reason  string `json:"reason"`
				Message string `json:"message"`
			}
			if json.Unmarshal(r, &o) == nil {
				d.Rejections = append(d.Rejections, firstNonEmpty(o.Message, o.Reason))
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// QueueRecord is one active download as the *arr sees it.
type QueueRecord struct {
	ID                      int       `json:"id"`
	SeriesID                int       `json:"seriesId"`
	EpisodeID               int       `json:"episodeId"`
	SeasonNumber            int       `json:"seasonNumber"`
	MovieID                 int       `json:"movieId"`
	Title                   string    `json:"title"`
	Status                  string    `json:"status"`
	TrackedDownloadStatus   string    `json:"trackedDownloadStatus"`
	TrackedDownloadState    string    `json:"trackedDownloadState"`
	DownloadID              string    `json:"downloadId"`
	Protocol                string    `json:"protocol"`
	DownloadClient          string    `json:"downloadClient"`
	Indexer                 string    `json:"indexer"`
	OutputPath              string    `json:"outputPath"`
	Size                    float64   `json:"size"`
	SizeLeft                float64   `json:"sizeleft"`
	TimeLeft                string    `json:"timeleft"`
	EstimatedCompletionTime time.Time `json:"estimatedCompletionTime"`
	ErrorMessage            string    `json:"errorMessage"`
	StatusMessages          []struct {
		Title    string   `json:"title"`
		Messages []string `json:"messages"`
	} `json:"statusMessages"`
	Episode *struct {
		ID            int    `json:"id"`
		SeasonNumber  int    `json:"seasonNumber"`
		EpisodeNumber int    `json:"episodeNumber"`
		Title         string `json:"title"`
	} `json:"episode"`
	Series *struct {
		ID     int     `json:"id"`
		Title  string  `json:"title"`
		Images []Image `json:"images"`
	} `json:"series"`
	Movie *struct {
		ID     int     `json:"id"`
		Title  string  `json:"title"`
		Year   int     `json:"year"`
		Images []Image `json:"images"`
	} `json:"movie"`
}

// Queue returns all queue records (pages are merged).
func (c *Client) Queue(ctx context.Context, include string) ([]QueueRecord, error) {
	var all []QueueRecord
	for page := 1; page <= 20; page++ {
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {"250"}, "includeUnknownSeriesItems": {"true"}, "includeUnknownMovieItems": {"true"}}
		for _, inc := range strings.Split(include, ",") {
			if inc != "" {
				q.Set(inc, "true")
			}
		}
		var res struct {
			TotalRecords int           `json:"totalRecords"`
			Records      []QueueRecord `json:"records"`
		}
		if err := c.get(ctx, "/queue", q, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Records...)
		if len(all) >= res.TotalRecords || len(res.Records) == 0 {
			break
		}
	}
	return all, nil
}

// RemoveQueue deletes a queue item (optionally from the download client).
func (c *Client) RemoveQueue(ctx context.Context, id int, removeFromClient, blocklist bool) error {
	q := url.Values{"removeFromClient": {strconv.FormatBool(removeFromClient)}, "blocklist": {strconv.FormatBool(blocklist)}}
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("%s/queue/%d", c.prefix, id), q, nil, nil)
}

// RecentlyGrabbed reports whether a release with this title was grabbed
// within the window (history event type 1 = grabbed).
func (c *Client) RecentlyGrabbed(ctx context.Context, title string, within time.Duration) (bool, error) {
	var res struct {
		Records []struct {
			SourceTitle string    `json:"sourceTitle"`
			EventType   string    `json:"eventType"`
			Date        time.Time `json:"date"`
		} `json:"records"`
	}
	q := url.Values{"page": {"1"}, "pageSize": {"25"}, "sortKey": {"date"}, "sortDirection": {"descending"}, "eventType": {"1"}}
	if err := c.get(ctx, "/history", q, &res); err != nil {
		return false, err
	}
	for _, r := range res.Records {
		if strings.EqualFold(r.SourceTitle, title) && time.Since(r.Date) < within {
			return true, nil
		}
	}
	return false, nil
}

// RemotePathMapping translates a download client's path to the *arr's.
type RemotePathMapping struct {
	Host       string `json:"host"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
}

// RemotePathMappings lists the configured mappings.
func (c *Client) RemotePathMappings(ctx context.Context) ([]RemotePathMapping, error) {
	var out []RemotePathMapping
	err := c.get(ctx, "/remotepathmapping", nil, &out)
	return out, err
}

// MapRemotePath rewrites a download client path (as qBittorrent reports it)
// to the path the *arr sees, using the longest matching mapping. Mappings
// for host are preferred; others apply when none of host's match.
func MapRemotePath(ms []RemotePathMapping, host, p string) string {
	best, bestLen, bestHost := -1, -1, false
	for i, m := range ms {
		rp := strings.TrimRight(strings.ReplaceAll(m.RemotePath, `\`, "/"), "/")
		pp := strings.ReplaceAll(p, `\`, "/")
		if pp != rp && !strings.HasPrefix(pp, rp+"/") {
			continue
		}
		hostOK := strings.EqualFold(m.Host, host)
		if (hostOK && !bestHost) || (hostOK == bestHost && len(rp) > bestLen) {
			best, bestLen, bestHost = i, len(rp), hostOK
		}
	}
	if best < 0 {
		return p
	}
	m := ms[best]
	rest := strings.TrimPrefix(strings.ReplaceAll(p, `\`, "/"), strings.TrimRight(strings.ReplaceAll(m.RemotePath, `\`, "/"), "/"))
	return strings.TrimRight(m.LocalPath, `/\`) + rest
}

// BypassProfileName is the permissive quality profile roarr creates to get
// a release past the *arr's quality and custom format rules.
const BypassProfileName = "roarr: any quality"

// EnsureBypassProfile returns the id of a quality profile that allows every
// quality, scores every custom format 0 and allows upgrades, creating it
// from the *arr's profile schema the first time.
func (c *Client) EnsureBypassProfile(ctx context.Context) (int, error) {
	profiles, err := c.QualityProfiles(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range profiles {
		if p.Name == BypassProfileName {
			return p.ID, nil
		}
	}
	var schema map[string]any
	if err := c.get(ctx, "/qualityprofile/schema", nil, &schema); err != nil {
		return 0, err
	}
	body := BypassProfile(schema)
	var out struct {
		ID int `json:"id"`
	}
	if err := c.send(ctx, http.MethodPost, "/qualityprofile", body, &out); err != nil {
		return 0, fmt.Errorf("creating the %q quality profile: %w", BypassProfileName, err)
	}
	return out.ID, nil
}

// BypassProfile turns a /qualityprofile/schema answer into the permissive
// profile.
func BypassProfile(schema map[string]any) map[string]any {
	p := map[string]any{}
	for k, v := range schema {
		p[k] = v
	}
	delete(p, "id")
	p["name"] = BypassProfileName
	p["upgradeAllowed"] = true
	p["minFormatScore"] = 0
	p["cutoffFormatScore"] = 0
	if _, ok := p["minUpgradeFormatScore"]; ok {
		p["minUpgradeFormatScore"] = 1
	}
	if _, ok := p["language"]; ok {
		p["language"] = map[string]any{"id": -1, "name": "Any"} // Radarr
	}
	items, _ := p["items"].([]any)
	var allow func([]any)
	allow = func(list []any) {
		for _, it := range list {
			if m, ok := it.(map[string]any); ok {
				m["allowed"] = true
				if sub, ok := m["items"].([]any); ok {
					allow(sub)
				}
			}
		}
	}
	allow(items)
	// Cutoff at the top quality, so nothing counts as "already good enough".
	for i := len(items) - 1; i >= 0; i-- {
		m, _ := items[i].(map[string]any)
		if m == nil {
			continue
		}
		if q, ok := m["quality"].(map[string]any); ok && q != nil {
			p["cutoff"] = q["id"]
			break
		}
		if id, ok := m["id"]; ok {
			p["cutoff"] = id
			break
		}
	}
	if fis, ok := p["formatItems"].([]any); ok {
		for _, fi := range fis {
			if m, ok := fi.(map[string]any); ok {
				m["score"] = 0
			}
		}
	}
	return p
}

// CachedRelease is one result of an interactive search, held in the *arr's
// release cache for 30 minutes.
type CachedRelease struct {
	GUID       string   `json:"guid"`
	IndexerID  int      `json:"indexerId"`
	Indexer    string   `json:"indexer"`
	Title      string   `json:"title"`
	Size       int64    `json:"size"`
	InfoHash   string   `json:"infoHash"`
	Protocol   string   `json:"protocol"`
	Rejected   bool     `json:"rejected"`
	Rejections []string `json:"rejections"`
}

// InteractiveSearch runs the *arr's own indexer search (GET /release) with
// q, e.g. episodeId, seriesId+seasonNumber or movieId. It is as slow as the
// slowest indexer, so it gets its own long timeout.
func (c *Client) InteractiveSearch(ctx context.Context, q url.Values) ([]CachedRelease, error) {
	slow := *c.Client
	hc := *c.HTTP
	hc.Timeout = 3 * time.Minute
	slow.HTTP = &hc
	var out []CachedRelease
	err := slow.Do(ctx, http.MethodGet, c.prefix+"/release", q, nil, &out)
	return out, err
}

// GrabCached downloads a release from the interactive search cache. This is
// the *arr UI's "grab anyway": it skips every decision engine rejection.
// extra is merged into the body (seriesId, episodeIds, movieId hints).
func (c *Client) GrabCached(ctx context.Context, r CachedRelease, extra map[string]any) error {
	body := map[string]any{"guid": r.GUID, "indexerId": r.IndexerID}
	for k, v := range extra {
		body[k] = v
	}
	return c.send(ctx, http.MethodPost, "/release", body, nil)
}

// MatchCached finds the release with the given guid, info hash or title.
func MatchCached(rs []CachedRelease, guid, infoHash, title string, size int64) (CachedRelease, bool) {
	norm := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '.' || r == '_' || r == '-' || r == ' ' {
				return -1
			}
			return r
		}, strings.ToLower(s))
	}
	tests := []func(CachedRelease) bool{
		func(r CachedRelease) bool { return guid != "" && r.GUID == guid },
		func(r CachedRelease) bool { return infoHash != "" && strings.EqualFold(r.InfoHash, infoHash) },
		func(r CachedRelease) bool {
			if norm(r.Title) != norm(title) {
				return false
			}
			if size <= 0 || r.Size <= 0 {
				return true
			}
			d := r.Size - size
			return d < 0 && -d < size/50 || d >= 0 && d < size/50
		},
	}
	for _, t := range tests {
		for _, r := range rs {
			if t(r) {
				return r, true
			}
		}
	}
	return CachedRelease{}, false
}
