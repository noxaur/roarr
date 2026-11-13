package qbit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAddTorrentFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Fails."))
	}))
	defer srv.Close()
	err := New(srv.URL, "", "").AddTorrent(context.Background(), Add{URL: "magnet:?xt=urn:btih:x"})
	if err != ErrRejected {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
}

// A session that expired between login and the add must be renewed, and the
// retried upload must still carry its body.
func TestAddTorrentRetryKeepsBody(t *testing.T) {
	var adds int32
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			b, _ := io.ReadAll(r.Body)
			lastBody = string(b)
			if atomic.AddInt32(&adds, 1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	defer srv.Close()
	if err := New(srv.URL, "u", "p").AddTorrent(context.Background(), Add{URL: "magnet:?xt=urn:btih:abc", Category: "tv"}); err != nil {
		t.Fatal(err)
	}
	if adds != 2 || !strings.Contains(lastBody, "magnet:?xt=urn:btih:abc") {
		t.Fatalf("adds=%d body=%q", adds, lastBody)
	}
}

// A refused login must not be repeated on every call, or roarr gets its own
// IP banned by qBittorrent.
func TestLoginBackoff(t *testing.T) {
	var logins int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&logins, 1)
		if r.Header.Get("Origin") != "" {
			t.Error("login sent an Origin header")
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(srv.URL, "u", "wrong")
	for i := 0; i < 5; i++ {
		if _, err := c.Version(context.Background()); err == nil {
			t.Fatal("want login error")
		}
	}
	if logins != 1 {
		t.Fatalf("logins = %d, want 1", logins)
	}
}

// A URL that cannot be parsed is an error, not a nil request.
func TestMalformedURL(t *testing.T) {
	if _, err := New("http://bad host", "u", "p").Version(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("err = %v, want the URL parse error", err)
	}
}

// Only a failed connection is "unreachable": a timeout and a cancelled
// request keep their own cause.
func TestTransportErrorKeepsCause(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-hang }))
	defer srv.Close()
	defer close(hang)
	for _, password := range []string{"", "p"} { // with a password the login is the request that fails
		c := New(srv.URL, "u", password)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := c.Version(ctx)
		cancel()
		if err == nil || !strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "unreachable") {
			t.Errorf("password %q: timeout err = %v, want timed out", password, err)
		}
		ctx, cancel = context.WithCancel(context.Background())
		time.AfterFunc(20*time.Millisecond, cancel)
		if _, err := c.Version(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("password %q: cancel err = %v, want context.Canceled", password, err)
		}
	}
}
