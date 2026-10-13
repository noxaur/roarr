// Package qbit is a small qBittorrent WebUI API v2 client covering what
// roarr needs for watch-first downloads: per-file progress and priorities,
// sequential mode, and adding torrents.
package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/httpx"
)

// File priorities.
const (
	PrioSkip   = 0
	PrioNormal = 1
	PrioHigh   = 6
	PrioMax    = 7
)

// Client talks to one qBittorrent instance.
type Client struct {
	Base     string
	Username string
	Password string

	mu       sync.Mutex
	http     *http.Client
	loggedIn bool
}

// New returns a client. Login happens lazily.
func New(base, user, pass string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		Base:     strings.TrimRight(base, "/"),
		Username: user,
		Password: pass,
		http:     &http.Client{Transport: httpx.Transport, Jar: jar, Timeout: 20 * time.Second},
	}
}

// Configured reports whether a URL is set.
func (c *Client) Configured() bool { return c != nil && c.Base != "" }

func (c *Client) login(ctx context.Context) error {
	if c.Password == "" {
		// No password: rely on qBittorrent's "bypass authentication" for
		// localhost / whitelisted subnets.
		c.loggedIn = true
		return nil
	}
	form := url.Values{"username": {c.Username}, "password": {c.Password}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.Base)
	req.Header.Set("Origin", c.Base)
	res, err := c.http.Do(req)
	if err != nil {
		return &httpx.Error{Service: "qbittorrent", Message: httpx.Unreachable(err)}
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	if res.StatusCode >= 300 || strings.TrimSpace(string(b)) == "Fails." {
		return &httpx.Error{Service: "qbittorrent", Status: res.StatusCode, Message: "login failed (check QBIT_USERNAME / QBIT_PASSWORD)"}
	}
	c.loggedIn = true
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, form url.Values, body io.Reader, contentType string, out any) error {
	if !c.Configured() {
		return &httpx.Error{Service: "qbittorrent", Message: "not configured"}
	}
	for attempt := 0; attempt < 2; attempt++ {
		c.mu.Lock()
		if !c.loggedIn {
			if err := c.login(ctx); err != nil {
				c.mu.Unlock()
				return err
			}
		}
		c.mu.Unlock()

		u := c.Base + path
		var rd io.Reader
		ct := contentType
		if body != nil {
			rd = body
		} else if method == http.MethodPost {
			rd = strings.NewReader(form.Encode())
			ct = "application/x-www-form-urlencoded"
		} else if len(form) > 0 {
			u += "?" + form.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return err
		}
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		req.Header.Set("Referer", c.Base)
		req.Header.Set("Origin", c.Base)
		res, err := c.http.Do(req)
		if err != nil {
			return &httpx.Error{Service: "qbittorrent", Message: httpx.Unreachable(err)}
		}
		if res.StatusCode == http.StatusForbidden && attempt == 0 && c.Password != "" {
			res.Body.Close()
			c.mu.Lock()
			c.loggedIn = false
			c.mu.Unlock()
			continue
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusForbidden && c.Password == "" {
			return &httpx.Error{Service: "qbittorrent", Status: res.StatusCode, Message: "qBittorrent wants a login: set QBIT_PASSWORD, or whitelist roarr's network under Web UI → Bypass authentication"}
		}
		if res.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
			return &httpx.Error{Service: "qbittorrent", Status: res.StatusCode, Message: strings.TrimSpace(string(b))}
		}
		switch o := out.(type) {
		case nil:
			_, _ = io.Copy(io.Discard, res.Body)
			return nil
		case *string:
			b, err := io.ReadAll(io.LimitReader(res.Body, 4096))
			*o = strings.TrimSpace(string(b))
			return err
		}
		return json.NewDecoder(res.Body).Decode(out)
	}
	return &httpx.Error{Service: "qbittorrent", Message: "forbidden"}
}

// Version returns the application version.
func (c *Client) Version(ctx context.Context) (string, error) {
	var v string
	err := c.do(ctx, http.MethodGet, "/api/v2/app/version", nil, nil, "", &v)
	return v, err
}

// Torrent is a subset of /torrents/info.
type Torrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	State        string  `json:"state"`
	Progress     float64 `json:"progress"`
	Size         int64   `json:"size"`
	DLSpeed      int64   `json:"dlspeed"`
	ETA          int64   `json:"eta"`
	NumSeeds     int     `json:"num_seeds"`
	Category     string  `json:"category"`
	SavePath     string  `json:"save_path"`
	ContentPath  string  `json:"content_path"`
	SeqDL        bool    `json:"seq_dl"`
	FirstLast    bool    `json:"f_l_piece_prio"`
	AddedOn      int64   `json:"added_on"`
	CompletionOn int64   `json:"completion_on"`
}

// Torrents returns torrents by hash (lowercase or uppercase accepted).
func (c *Client) Torrents(ctx context.Context, hashes ...string) ([]Torrent, error) {
	form := url.Values{}
	if len(hashes) > 0 {
		hs := make([]string, len(hashes))
		for i, h := range hashes {
			hs[i] = strings.ToLower(h)
		}
		form.Set("hashes", strings.Join(hs, "|"))
	}
	var out []Torrent
	err := c.do(ctx, http.MethodGet, "/api/v2/torrents/info", form, nil, "", &out)
	return out, err
}

// File is one file inside a torrent.
type File struct {
	Index        int     `json:"index"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Priority     int     `json:"priority"`
	Availability float64 `json:"availability"`
}

// Files lists a torrent's files.
func (c *Client) Files(ctx context.Context, hash string) ([]File, error) {
	var out []File
	err := c.do(ctx, http.MethodGet, "/api/v2/torrents/files", url.Values{"hash": {strings.ToLower(hash)}}, nil, "", &out)
	for i := range out {
		// Older versions omit index; it equals the position.
		if out[i].Index == 0 && i > 0 {
			out[i].Index = i
		}
	}
	return out, err
}

// SetFilePriority sets priority for file indexes.
func (c *Client) SetFilePriority(ctx context.Context, hash string, ids []int, prio int) error {
	if len(ids) == 0 {
		return nil
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return c.do(ctx, http.MethodPost, "/api/v2/torrents/filePrio", url.Values{"hash": {strings.ToLower(hash)}, "id": {strings.Join(parts, "|")}, "priority": {strconv.Itoa(prio)}}, nil, "", nil)
}

// SetSequential turns sequential download on or off (the API only toggles,
// so current state is read first).
func (c *Client) SetSequential(ctx context.Context, hash string, on bool) error {
	ts, err := c.Torrents(ctx, hash)
	if err != nil || len(ts) == 0 {
		return err
	}
	if ts[0].SeqDL == on {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/api/v2/torrents/toggleSequentialDownload", url.Values{"hashes": {strings.ToLower(hash)}}, nil, "", nil)
}

// SetFirstLast turns first/last piece priority on or off.
func (c *Client) SetFirstLast(ctx context.Context, hash string, on bool) error {
	ts, err := c.Torrents(ctx, hash)
	if err != nil || len(ts) == 0 {
		return err
	}
	if ts[0].FirstLast == on {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/api/v2/torrents/toggleFirstLastPiecePrio", url.Values{"hashes": {strings.ToLower(hash)}}, nil, "", nil)
}

// Add adds a torrent by URL/magnet or raw .torrent bytes.
type Add struct {
	URL       string
	Torrent   []byte
	Category  string
	SavePath  string
	Paused    bool
	Sequental bool
	FirstLast bool
}

// AddTorrent adds a torrent.
func (c *Client) AddTorrent(ctx context.Context, a Add) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if a.URL != "" {
		_ = w.WriteField("urls", a.URL)
	}
	if len(a.Torrent) > 0 {
		fw, _ := w.CreateFormFile("torrents", "release.torrent")
		_, _ = fw.Write(a.Torrent)
	}
	if a.Category != "" {
		_ = w.WriteField("category", a.Category)
	}
	if a.SavePath != "" {
		_ = w.WriteField("savepath", a.SavePath)
	}
	if a.Paused {
		_ = w.WriteField("paused", "true")
		_ = w.WriteField("stopped", "true")
	}
	if a.Sequental {
		_ = w.WriteField("sequentialDownload", "true")
	}
	if a.FirstLast {
		_ = w.WriteField("firstLastPiecePrio", "true")
	}
	_ = w.Close()
	return c.do(ctx, http.MethodPost, "/api/v2/torrents/add", nil, &buf, w.FormDataContentType(), nil)
}

// Start resumes torrents (qBittorrent 5 renamed resume -> start).
func (c *Client) Start(ctx context.Context, hash string) error {
	err := c.do(ctx, http.MethodPost, "/api/v2/torrents/start", url.Values{"hashes": {strings.ToLower(hash)}}, nil, "", nil)
	if err != nil {
		return c.do(ctx, http.MethodPost, "/api/v2/torrents/resume", url.Values{"hashes": {strings.ToLower(hash)}}, nil, "", nil)
	}
	return nil
}

// ErrNotFound is returned when a torrent is not (yet) known.
var ErrNotFound = fmt.Errorf("torrent not found")
