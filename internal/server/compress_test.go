package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAccepts(t *testing.T) {
	for _, c := range []struct {
		header, enc string
		want        bool
	}{
		{"gzip, deflate, br", "br", true},
		{"gzip, deflate, br", "gzip", true},
		{"deflate", "gzip", false},
		{"gzip;q=0, br", "gzip", false},
		{"GZIP; q=0.5", "gzip", true},
		{"", "gzip", false},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", c.header)
		if got := accepts(r, c.enc); got != c.want {
			t.Errorf("accepts(%q, %q) = %v, want %v", c.header, c.enc, got, c.want)
		}
	}
}

func TestMiddlewareGzipsJSON(t *testing.T) {
	s := &Server{}
	body := `{"items":[` + strings.Repeat(`"x",`, 500) + `"x"]}`
	h := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "1" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, body)
	}))

	r := httptest.NewRequest("GET", "/api/library", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", w.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != body {
		t.Fatalf("body does not round-trip: %d bytes, want %d", len(got), len(body))
	}

	r = httptest.NewRequest("GET", "/api/library", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	r.Header.Set("If-None-Match", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 304 || w.Header().Get("Content-Encoding") != "" || w.Body.Len() != 0 {
		t.Fatalf("304 got code %d, encoding %q, %d body bytes", w.Code, w.Header().Get("Content-Encoding"), w.Body.Len())
	}

	r = httptest.NewRequest("GET", "/api/library", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != body {
		t.Fatal("a client without gzip must get the plain body")
	}
}

func TestMiddlewareLeavesSearchStreamAlone(t *testing.T) {
	s := &Server{}
	h := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{}")
	}))
	r := httptest.NewRequest("GET", "/api/search", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatal("the search stream must not be compressed")
	}
}

func TestStaticServesPrecompressed(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":          {Data: []byte("<html>")},
		"index.html.br":       {Data: []byte("BR-HTML")},
		"assets/app-1.js":     {Data: []byte("plain")},
		"assets/app-1.js.br":  {Data: []byte("BR")},
		"assets/app-1.js.gz":  {Data: []byte("GZ")},
		"assets/font-1.woff2": {Data: []byte("font")},
	}
	s := &Server{ui: ui}
	h := s.static()
	get := func(p, ae string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", p, nil)
		if ae != "" {
			r.Header.Set("Accept-Encoding", ae)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, c := range []struct{ path, ae, enc, body string }{
		{"/assets/app-1.js", "gzip, br", "br", "BR"},
		{"/assets/app-1.js", "gzip", "gzip", "GZ"},
		{"/assets/app-1.js", "", "", "plain"},
		{"/assets/font-1.woff2", "gzip, br", "", "font"},
		{"/tv/12", "br", "br", "BR-HTML"},
	} {
		w := get(c.path, c.ae)
		if w.Header().Get("Content-Encoding") != c.enc || w.Body.String() != c.body {
			t.Errorf("%s (%q): encoding %q body %q, want %q %q", c.path, c.ae, w.Header().Get("Content-Encoding"), w.Body.String(), c.enc, c.body)
		}
	}
	if ct := get("/assets/app-1.js", "br").Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want javascript", ct)
	}
}
