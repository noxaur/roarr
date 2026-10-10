// Package jellyfin covers the few Jellyfin calls roarr needs: user login
// (for the optional auth gate) and targeted library refreshes so a freshly
// imported episode shows up in seconds instead of at the next scheduled scan.
package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/noxaur/roarr/internal/httpx"
)

const clientHeader = `MediaBrowser Client="roarr", Device="roarr", DeviceId="roarr-server", Version="1.0"`

// Client is a Jellyfin API client authenticated with an API key.
type Client struct {
	*httpx.Client
}

// New returns a client. apiKey may be empty if only login is needed.
func New(base, apiKey string) *Client {
	h := http.Header{}
	auth := clientHeader
	if apiKey != "" {
		auth += fmt.Sprintf(`, Token="%s"`, apiKey)
	}
	h.Set("Authorization", auth)
	return &Client{httpx.New("jellyfin", base, h)}
}

// PublicInfo is /System/Info/Public.
type PublicInfo struct {
	ServerName string `json:"ServerName"`
	Version    string `json:"Version"`
}

// Info pings the server.
func (c *Client) Info(ctx context.Context) (PublicInfo, error) {
	var out PublicInfo
	err := c.Get(ctx, "/System/Info/Public", nil, &out)
	return out, err
}

// User is the authenticated Jellyfin user.
type User struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"`
	IsAdmin bool   `json:"-"`
}

// Login authenticates a user by name and password.
func (c *Client) Login(ctx context.Context, username, password string) (User, error) {
	var res struct {
		User struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Policy struct {
				IsAdministrator bool `json:"IsAdministrator"`
			} `json:"Policy"`
		} `json:"User"`
		AccessToken string `json:"AccessToken"`
	}
	anon := &httpx.Client{Service: "jellyfin", Base: c.Base, HTTP: c.HTTP, Header: http.Header{"Authorization": {clientHeader}}}
	err := anon.Do(ctx, http.MethodPost, "/Users/AuthenticateByName", nil, map[string]string{"Username": username, "Pw": password}, &res)
	if err != nil {
		return User{}, err
	}
	return User{ID: res.User.ID, Name: res.User.Name, IsAdmin: res.User.Policy.IsAdministrator}, nil
}

// Item is a library item.
type Item struct {
	ID          string            `json:"Id"`
	Name        string            `json:"Name"`
	Type        string            `json:"Type"`
	Path        string            `json:"Path"`
	ProviderIDs map[string]string `json:"ProviderIds"`
}

// FindByProvider finds a Series or Movie by provider id (tvdb/tmdb/imdb).
func (c *Client) FindByProvider(ctx context.Context, kind, title, provider, id string) (*Item, error) {
	q := url.Values{
		"Recursive":        {"true"},
		"IncludeItemTypes": {kind},
		"SearchTerm":       {title},
		"Fields":           {"ProviderIds,Path"},
		"Limit":            {"25"},
	}
	var res struct {
		Items []Item `json:"Items"`
	}
	if err := c.Get(ctx, "/Items", q, &res); err != nil {
		return nil, err
	}
	for i := range res.Items {
		for k, v := range res.Items[i].ProviderIDs {
			if strings.EqualFold(k, provider) && v == id {
				return &res.Items[i], nil
			}
		}
	}
	return nil, nil
}

// RefreshItem re-scans one item (recursively for series) for new files.
func (c *Client) RefreshItem(ctx context.Context, id string) error {
	q := url.Values{
		"Recursive":           {"true"},
		"MetadataRefreshMode": {"Default"},
		"ImageRefreshMode":    {"Default"},
		"ReplaceAllMetadata":  {"false"},
		"ReplaceAllImages":    {"false"},
	}
	return c.Do(ctx, http.MethodPost, "/Items/"+id+"/Refresh", q, nil, nil)
}

// RefreshLibrary starts a full library scan (fallback for new shows).
func (c *Client) RefreshLibrary(ctx context.Context) error {
	return c.Do(ctx, http.MethodPost, "/Library/Refresh", nil, nil, nil)
}

// Refresh makes Jellyfin pick up new media for a show or movie as quickly as
// possible: refresh the existing item when Jellyfin already knows it,
// otherwise fall back to a library scan.
func (c *Client) Refresh(ctx context.Context, kind, title, provider, id string) (string, error) {
	if c == nil || !c.Configured() {
		return "", nil
	}
	item, err := c.FindByProvider(ctx, kind, title, provider, id)
	if err != nil {
		return "", err
	}
	if item != nil {
		return "item", c.RefreshItem(ctx, item.ID)
	}
	return "library", c.RefreshLibrary(ctx)
}
