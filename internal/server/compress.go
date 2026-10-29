package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// accepts reports whether the request allows a content encoding.
func accepts(r *http.Request, enc string) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), enc) {
			continue
		}
		q := strings.ReplaceAll(params, " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

// The UI build writes a .br and a .gz copy next to each text asset
// (web/vite.config.ts). Serving those costs no CPU per request.
var precompressed = []struct{ enc, ext string }{{"br", ".br"}, {"gzip", ".gz"}}

// buildTime stands in for the modification time of embedded files, which
// embed.FS does not keep. It changes with every start, as the files can.
var buildTime = time.Now()

// serveEncoded answers with the precompressed copy of p, when the build made
// one and the client takes it. It reports whether it answered.
func serveEncoded(w http.ResponseWriter, r *http.Request, ui fs.FS, p string) bool {
	for _, c := range precompressed {
		if !accepts(r, c.enc) {
			continue
		}
		b, err := fs.ReadFile(ui, p+c.ext)
		if err != nil {
			continue
		}
		h := w.Header()
		if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
			h.Set("Content-Type", ct)
		}
		h.Set("Content-Encoding", c.enc)
		h.Add("Vary", "Accept-Encoding")
		http.ServeContent(w, r, p, buildTime, bytes.NewReader(b))
		return true
	}
	return false
}

// gzipWriter compresses JSON answers. The library alone is hundreds of
// kilobytes on a big setup; gzip cuts it to about a tenth. It decides on the
// first WriteHeader, so 304s, errors without a body and streams pass as they
// are.
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

var gzPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return w
}}

func (g *gzipWriter) WriteHeader(code int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		if code >= 200 && code != http.StatusNoContent && code != http.StatusNotModified &&
			h.Get("Content-Encoding") == "" && strings.HasPrefix(h.Get("Content-Type"), "application/json") {
			h.Del("Content-Length")
			h.Set("Content-Encoding", "gzip")
			g.gz = gzPool.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipWriter) close() {
	if g.gz == nil {
		return
	}
	_ = g.gz.Close()
	gzPool.Put(g.gz)
	g.gz = nil
}
