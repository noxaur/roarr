package qbit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func fakeQbit(sid *atomic.Value, logins *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			logins.Add(1)
			_ = r.ParseForm()
			if r.Form.Get("password") != "secret" {
				w.Write([]byte("Fails."))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: sid.Load().(string), Path: "/"})
			w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			if c, err := r.Cookie("SID"); err != nil || c.Value != sid.Load().(string) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte("v5.0.0\n"))
		}
	}))
}

// An expired session (qBittorrent restarted) logs in again transparently.
func TestVersionReloginsAfterExpiry(t *testing.T) {
	var sid atomic.Value
	var logins atomic.Int32
	sid.Store("one")
	srv := fakeQbit(&sid, &logins)
	defer srv.Close()
	c := New(srv.URL, "admin", "secret")
	for i, s := range []string{"one", "two"} {
		sid.Store(s)
		v, err := c.Version(context.Background())
		if err != nil || v != "v5.0.0" {
			t.Fatalf("call %d: v=%q err=%v", i, v, err)
		}
	}
	if logins.Load() != 2 {
		t.Fatalf("logins = %d, want 2", logins.Load())
	}
}

// Without a password roarr relies on qBittorrent's auth bypass and says so
// when that is not set up.
func TestNoPasswordExplainsWhitelist(t *testing.T) {
	var sid atomic.Value
	var logins atomic.Int32
	sid.Store("x")
	srv := fakeQbit(&sid, &logins)
	defer srv.Close()
	_, err := New(srv.URL, "", "").Version(context.Background())
	if err == nil || logins.Load() != 0 {
		t.Fatalf("err=%v logins=%d", err, logins.Load())
	}
	if want := "QBIT_PASSWORD"; !contains(err.Error(), want) {
		t.Fatalf("error %q does not mention %s", err, want)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
