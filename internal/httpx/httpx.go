// Package httpx holds the shared HTTP plumbing for every upstream client:
// one tuned transport (keep-alive, HTTP/2, many idle conns per host so
// parallel indexer fan-out never waits on a handshake) and a small JSON
// helper that turns upstream failures into readable errors.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Transport is shared by all clients so connections to the same host are
// pooled across features.
var Transport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           dialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          256,
	MaxIdleConnsPerHost:   64,
	IdleConnTimeout:       120 * time.Second,
	TLSHandshakeTimeout:   6 * time.Second,
	ExpectContinueTimeout: time.Second,
	ResponseHeaderTimeout: 0, // per-request contexts decide
}

// UserAgent is sent on every request. Some reverse proxies (Cloudflare)
// reject empty or obviously scripted agents.
const UserAgent = "Mozilla/5.0 (X11; Linux x86_64) roarr/1 (+https://github.com/noxaur/roarr)"

// Error is an upstream failure with enough context to show a user.
type Error struct {
	Service string `json:"service"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s: HTTP %d: %s", e.Service, e.Status, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Service, e.Message)
}

// Client is a JSON API client for one upstream service.
type Client struct {
	Service string
	Base    string // scheme://host[:port][/base]
	Header  http.Header
	HTTP    *http.Client
}

// New returns a client for base with the given static headers.
func New(service, base string, header http.Header) *Client {
	if header == nil {
		header = http.Header{}
	}
	return &Client{
		Service: service,
		Base:    strings.TrimRight(base, "/"),
		Header:  header,
		HTTP:    &http.Client{Transport: Transport, Timeout: 60 * time.Second},
	}
}

// Configured reports whether the client has somewhere to talk to.
func (c *Client) Configured() bool { return c != nil && c.Base != "" }

// Do sends a request and decodes a JSON response into out (if non-nil).
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if !c.Configured() {
		return &Error{Service: c.Service, Message: "not configured"}
	}
	u := c.Base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	for k, v := range c.Header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return c.wrap(err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &Error{Service: c.Service, Status: res.StatusCode, Message: upstreamMessage(b, res.Status)}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return &Error{Service: c.Service, Message: "bad response: " + err.Error()}
	}
	return nil
}

// Get is Do with GET.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, http.MethodGet, path, query, nil, out)
}

func (c *Client) wrap(err error) error {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return &Error{Service: c.Service, Message: "timed out"}
	case errors.Is(err, context.Canceled):
		return err
	}
	return &Error{Service: c.Service, Message: Unreachable(err)}
}

// Unreachable turns a transport error into a short message with a hint for
// the usual Docker networking mistakes.
func Unreachable(err error) string {
	full := err.Error()
	msg := full
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	out := "unreachable (" + msg + ")"
	switch {
	case strings.Contains(full, "no such host"):
		out += ": the host name does not resolve here. Put roarr on the same Docker network as this service, or use the host's LAN IP"
	case strings.Contains(full, "connection refused"):
		out += ": nothing listens on that address as seen from roarr. Inside Docker, use the container name and its internal port"
	case strings.Contains(full, "unsupported protocol scheme"):
		out += ": the URL needs http:// or https://"
	}
	return out
}

// upstreamMessage pulls a human message out of *arr / Seerr error bodies.
func upstreamMessage(b []byte, fallback string) string {
	var one struct {
		Message      string `json:"message"`
		ErrorMessage string `json:"errorMessage"`
		Error        string `json:"error"`
	}
	if json.Unmarshal(b, &one) == nil {
		for _, m := range []string{one.Message, one.ErrorMessage, one.Error} {
			if m != "" {
				return m
			}
		}
	}
	var many []struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(b, &many) == nil && len(many) > 0 && many[0].ErrorMessage != "" {
		return many[0].ErrorMessage
	}
	s := strings.TrimSpace(string(b))
	if s == "" || strings.HasPrefix(s, "<") {
		return fallback
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
