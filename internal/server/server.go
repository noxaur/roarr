// Package server exposes roarr's HTTP API and serves the web UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/noxaur/roarr/internal/app"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/httpx"
	"github.com/noxaur/roarr/internal/jellyfin"
	"github.com/noxaur/roarr/internal/release"
)

// Version is set at build time.
var Version = "dev"

// Server is the HTTP layer.
type Server struct {
	app  *app.App
	auth *auth
	ui   fs.FS

	statusMu sync.Mutex
	statusAt time.Time
	status   map[string]serviceStatus
}

// New builds the server.
func New(a *app.App, ui fs.FS) (*Server, error) {
	au, err := newAuth(a.DataDir)
	if err != nil {
		return nil, err
	}
	return &Server{app: a, auth: au, ui: ui}, nil
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	open := func(p string, h http.HandlerFunc) { mux.HandleFunc(p, h) }
	guarded := func(p string, h http.HandlerFunc) { mux.Handle(p, s.requireAuth(h)) }

	open("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "version": Version})
	})
	open("GET /api/me", s.me)
	open("POST /api/login", s.login)
	open("POST /api/logout", s.logout)

	guarded("GET /api/status", s.statusHandler)
	guarded("GET /api/library", s.library)
	guarded("GET /api/discover", s.discover)
	guarded("GET /api/tv/{id}", s.series)
	guarded("GET /api/tv/tvdb/{tvdb}", s.seriesByTvdb)
	guarded("GET /api/movie/{id}", s.movie)
	guarded("GET /api/movie/tmdb/{tmdb}", s.movieByTmdb)
	guarded("POST /api/tv/{id}/episodes", s.seriesEpisodes)
	guarded("PATCH /api/{kind}/{id}", s.patchTitle)
	guarded("DELETE /api/{kind}/{id}", s.removeTitle)
	guarded("POST /api/{kind}/{id}/command/{name}", s.titleCommand)
	guarded("DELETE /api/movie/{id}/file", s.deleteMovieFile)
	guarded("GET /api/profiles/{kind}", s.profiles)
	guarded("GET /api/search", s.search)
	guarded("POST /api/plan", s.plan)
	guarded("POST /api/grab", s.grab)
	guarded("POST /api/auto", s.auto)
	guarded("GET /api/activity", s.activity)
	guarded("DELETE /api/activity/{id}", s.removeActivity)
	guarded("GET /api/requests", s.requests)
	guarded("POST /api/requests/{id}/{action}", s.requestAction)
	guarded("GET /api/settings", s.getSettings)
	guarded("PUT /api/settings", s.putSettings)
	s.verifyRoutes(guarded)

	mux.Handle("/", s.static())
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: http: data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'self'")
		// Cheap CSRF guard: state-changing API calls must come from our JS.
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Roarr") != "1" {
				writeErr(w, http.StatusForbidden, errors.New("missing X-Roarr header"))
				return
			}
		}
		start := time.Now()
		// A panic would drop the connection and show up in front of roarr as a
		// bare 502. Log it and answer with JSON instead.
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic in handler", "method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				writeErr(w, http.StatusInternalServerError, fmt.Errorf("internal error: %v", v))
			}
		}()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/search" {
			slog.Debug("http", "method", r.Method, "path", r.URL.Path, "ms", time.Since(start).Milliseconds())
		}
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := s.app.Settings.Get()
		if st.AuthMode == "none" {
			next.ServeHTTP(w, r)
			return
		}
		sess, ok := s.auth.current(r)
		if !ok || (st.AuthMode == "jellyfin" && st.JellyfinAdminOnly && !sess.Admin) {
			writeErr(w, http.StatusUnauthorized, errors.New("sign in required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	st := s.app.Settings.Get()
	out := map[string]any{"authMode": st.AuthMode, "version": Version}
	if st.AuthMode == "none" {
		out["authenticated"] = true
	} else if sess, ok := s.auth.current(r); ok {
		out["authenticated"] = !(st.AuthMode == "jellyfin" && st.JellyfinAdminOnly && !sess.Admin)
		out["user"] = sess.User
	} else {
		out["authenticated"] = false
	}
	writeJSON(w, 200, out)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.throttle(r); err != nil {
		writeErr(w, http.StatusTooManyRequests, err)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, err)
		return
	}
	st := s.app.Settings.Get()
	switch st.AuthMode {
	case "none":
		writeJSON(w, 200, map[string]any{"ok": true})
	case "password":
		if !passwordOK(body.Password, st.AuthPassword) {
			s.auth.fail(r)
			writeErr(w, http.StatusUnauthorized, errors.New("wrong password"))
			return
		}
		s.auth.issue(w, r, "admin", true)
		writeJSON(w, 200, map[string]any{"ok": true})
	case "jellyfin":
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		u, err := jellyfin.New(st.Jellyfin.URL, "").Login(ctx, body.Username, body.Password)
		if err != nil {
			var he *httpx.Error
			if errors.As(err, &he) && (he.Status == 401 || he.Status == 400) {
				s.auth.fail(r)
				writeErr(w, http.StatusUnauthorized, errors.New("wrong username or password"))
				return
			}
			writeErr(w, http.StatusFailedDependency, fmt.Errorf("could not reach Jellyfin: %w", err))
			return
		}
		if st.JellyfinAdminOnly && !u.IsAdmin {
			writeErr(w, http.StatusForbidden, errors.New("only Jellyfin administrators can use roarr"))
			return
		}
		s.auth.issue(w, r, u.Name, u.IsAdmin)
		writeJSON(w, 200, map[string]any{"ok": true, "user": u.Name})
	default:
		writeErr(w, 500, fmt.Errorf("unknown AUTH_MODE %q", st.AuthMode))
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.clear(w, r)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------

type serviceStatus struct {
	Configured bool   `json:"configured"`
	OK         bool   `json:"ok"`
	Version    string `json:"version,omitempty"`
	Error      string `json:"error,omitempty"`
	URL        string `json:"url,omitempty"`
	MS         int    `json:"ms,omitempty"`
}

func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	fresh := r.URL.Query().Get("fresh") == "1"
	s.statusMu.Lock()
	if !fresh && time.Since(s.statusAt) < 15*time.Second && s.status != nil {
		st := s.status
		s.statusMu.Unlock()
		writeJSON(w, 200, map[string]any{"services": st, "version": Version})
		return
	}
	s.statusMu.Unlock()

	c := s.app.C()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	type probe struct {
		name string
		url  string
		fn   func() (string, error)
	}
	var probes []probe
	add := func(name, url string, configured bool, fn func() (string, error)) {
		if !configured {
			probes = append(probes, probe{name: name})
			return
		}
		probes = append(probes, probe{name, url, fn})
	}
	add("sonarr", baseOf(c.Sonarr != nil, func() string { return c.Sonarr.Base }), c.Sonarr != nil, func() (string, error) { v, err := c.Sonarr.Status(ctx); return v.Version, err })
	add("radarr", baseOf(c.Radarr != nil, func() string { return c.Radarr.Base }), c.Radarr != nil, func() (string, error) { v, err := c.Radarr.Status(ctx); return v.Version, err })
	add("prowlarr", baseOf(c.Prowlarr != nil, func() string { return c.Prowlarr.Base }), c.Prowlarr != nil, func() (string, error) { v, err := c.Prowlarr.Status(ctx); return v.Version, err })
	add("seerr", baseOf(c.Seerr != nil, func() string { return c.Seerr.Base }), c.Seerr != nil, func() (string, error) { return c.Seerr.Status(ctx) })
	add("jellyfin", baseOf(c.Jellyfin != nil, func() string { return c.Jellyfin.Base }), c.Jellyfin != nil, func() (string, error) { v, err := c.Jellyfin.Info(ctx); return v.Version, err })
	add("qbittorrent", baseOf(c.QBit != nil, func() string { return c.QBit.Base }), c.QBit != nil, func() (string, error) { return c.QBit.Version(ctx) })

	out := map[string]serviceStatus{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range probes {
		if p.fn == nil {
			out[p.name] = serviceStatus{}
			continue
		}
		wg.Add(1)
		go func(p probe) {
			defer wg.Done()
			start := time.Now()
			v, err := p.fn()
			st := serviceStatus{Configured: true, OK: err == nil, Version: v, URL: p.url, MS: int(time.Since(start).Milliseconds())}
			if err != nil {
				st.Error = err.Error()
			}
			mu.Lock()
			out[p.name] = st
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	s.statusMu.Lock()
	s.status, s.statusAt = out, time.Now()
	s.statusMu.Unlock()
	writeJSON(w, 200, map[string]any{"services": out, "version": Version})
}

func baseOf(ok bool, f func() string) string {
	if !ok {
		return ""
	}
	return f()
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	items, etag, errs := s.app.Library.Snapshot()
	if r.URL.Query().Get("refresh") == "1" {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		s.app.Library.Load(ctx)
		cancel()
		items, etag, errs = s.app.Library.Snapshot()
	}
	if etag != "" && r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if items == nil {
		items = []app.LibraryItem{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "errors": errs})
}

func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := s.app.Discover(ctx, r.URL.Query().Get("q"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if res == nil {
		res = []app.DiscoverItem{}
	}
	writeJSON(w, 200, map[string]any{"results": res})
}

func (s *Server) series(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	d, err := s.app.Series(r.Context(), id, r.URL.Query().Get("fresh") == "1")
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) seriesByTvdb(w http.ResponseWriter, r *http.Request) {
	tvdb, _ := strconv.Atoi(r.PathValue("tvdb"))
	tmdb, _ := strconv.Atoi(r.URL.Query().Get("tmdb"))
	d, err := s.app.SeriesByTvdb(r.Context(), tvdb, tmdb)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) movie(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	d, err := s.app.Movie(r.Context(), id)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) movieByTmdb(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("tmdb"))
	d, err := s.app.MovieByTmdb(r.Context(), id)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, d)
}

// ---------------------------------------------------------------------------
// Managing library titles

func titleKind(r *http.Request) (string, int, error) {
	kind := r.PathValue("kind")
	if kind != "tv" && kind != "movie" {
		return "", 0, fmt.Errorf("unknown kind %q", kind)
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	if id <= 0 {
		return "", 0, errors.New("bad id")
	}
	return kind, id, nil
}

func (s *Server) seriesEpisodes(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	var body app.EpisodeAction
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, err)
		return
	}
	switch body.Action {
	case "delete", "monitor", "unmonitor", "search":
	default:
		writeErr(w, 400, fmt.Errorf("unknown action %q", body.Action))
		return
	}
	res, err := s.app.SeriesEpisodes(r.Context(), id, body)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) patchTitle(w http.ResponseWriter, r *http.Request) {
	kind, id, err := titleKind(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var p app.TitlePatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, err)
		return
	}
	if kind == "movie" {
		err = s.app.PatchMovie(r.Context(), id, p)
	} else {
		err = s.app.PatchSeries(r.Context(), id, p)
	}
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) removeTitle(w http.ResponseWriter, r *http.Request) {
	kind, id, err := titleKind(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.app.RemoveTitle(r.Context(), kind, id, r.URL.Query().Get("deleteFiles") == "1"); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) titleCommand(w http.ResponseWriter, r *http.Request) {
	kind, id, err := titleKind(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if n := r.PathValue("name"); n != "refresh" && n != "search" {
		writeErr(w, 400, fmt.Errorf("unknown command %q", n))
		return
	}
	res, err := s.app.TitleCommand(r.Context(), kind, id, r.PathValue("name"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) deleteMovieFile(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	res, err := s.app.DeleteMovieFile(r.Context(), id)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	ps, err := s.app.Profiles(r.Context(), r.PathValue("kind"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]any{"profiles": ps})
}

func ints(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// search streams results as Server-Sent Events.
//
//	/api/search?kind=tv&id=12&show=1&seasons=1,2&episodes=101,102
//	/api/search?kind=tv&tvdb=371980&tmdb=95396&show=1
//	/api/search?kind=movie&tmdb=603,604,605
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx := r.Context()
	var t app.Target
	var sc app.Scope
	switch q.Get("kind") {
	case "movie":
		t.Kind = "movie"
		for _, id := range ints(q.Get("tmdb")) {
			m, err := s.app.MovieByTmdb(ctx, id)
			if err != nil {
				writeErr(w, statusFor(err), err)
				return
			}
			t.Movies = append(t.Movies, m)
		}
		if len(t.Movies) == 0 {
			writeErr(w, 400, errors.New("tmdb ids required"))
			return
		}
	default:
		t.Kind = "tv"
		var d *app.SeriesDetail
		var err error
		if id, _ := strconv.Atoi(q.Get("id")); id > 0 {
			d, err = s.app.Series(ctx, id, false)
		} else {
			tvdb, _ := strconv.Atoi(q.Get("tvdb"))
			tmdb, _ := strconv.Atoi(q.Get("tmdb"))
			d, err = s.app.SeriesByTvdb(ctx, tvdb, tmdb)
		}
		if err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		t.Series = d
		sc = app.Scope{Show: q.Get("show") == "1", Seasons: ints(q.Get("seasons")), Episodes: ints(q.Get("episodes"))}
	}

	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, errors.New("streaming unsupported"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer
	w.WriteHeader(200)
	fl.Flush()

	var wmu sync.Mutex
	send := func(ev any) {
		b, _ := json.Marshal(ev)
		wmu.Lock()
		defer wmu.Unlock()
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				wmu.Lock()
				fmt.Fprint(w, ": ping\n\n")
				fl.Flush()
				wmu.Unlock()
			}
		}
	}()
	defer close(stop)
	if _, err := s.app.Search(ctx, t, sc, func(ev app.SearchEvent) { send(ev) }); err != nil && !errors.Is(err, context.Canceled) {
		send(app.SearchEvent{Type: "error", Error: err.Error()})
	}
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Wanted  []int            `json:"wanted"`
		Options []release.Option `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, release.PlanEpisodes(body.Wanted, body.Options))
}

func (s *Server) grab(w http.ResponseWriter, r *http.Request) {
	var g app.GrabRequest
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeErr(w, 400, err)
		return
	}
	// Skipping checks may run an interactive search inside Sonarr/Radarr,
	// which is as slow as their slowest indexer.
	//
	// The grab must not die with the browser connection: a proxy in front of
	// roarr may give up (502/504) while roarr is mid-way, and a half-finished
	// grab leaves a torrent in qBittorrent that nothing tracks.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Minute)
	defer cancel()
	res, err := s.app.Grab(ctx, g)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) auto(w http.ResponseWriter, r *http.Request) {
	var req app.AutoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	res, err := s.app.Auto(ctx, req)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	items, errs := s.app.Activity(ctx)
	if items == nil {
		items = []app.ActivityItem{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "errors": errs})
}

func (s *Server) removeActivity(w http.ResponseWriter, r *http.Request) {
	if err := s.app.RemoveActivity(r.Context(), r.PathValue("id"), r.URL.Query().Get("blocklist") == "1"); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) requests(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	items, err := s.app.Requests(ctx, r.URL.Query().Get("filter"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if items == nil {
		items = []app.RequestItem{}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) requestAction(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if err := s.app.RequestAction(r.Context(), id, r.PathValue("action")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Settings

const masked = "••••••••"

func mask(v string) string {
	if v == "" {
		return ""
	}
	return masked
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st := s.app.Settings.Get()
	for _, svc := range []*config.Service{&st.Sonarr, &st.Radarr, &st.Prowlarr, &st.Seerr, &st.Jellyfin, &st.QBit} {
		svc.APIKey = mask(svc.APIKey)
		svc.Password = mask(svc.Password)
	}
	st.AuthPassword = mask(st.AuthPassword)
	writeJSON(w, 200, map[string]any{"settings": st, "locked": s.app.Settings.Locked()})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in config.Settings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if in.AuthMode == "password" && in.AuthPassword == "" && s.app.Settings.Get().AuthPassword == "" {
		writeErr(w, 400, errors.New("set a password before switching to password sign-in"))
		return
	}
	keep := func(next *string, prev string) {
		if *next == masked {
			*next = prev
		}
	}
	_, err := s.app.Settings.Update(func(cur *config.Settings) {
		pairs := []struct{ next, prev *config.Service }{
			{&in.Sonarr, &cur.Sonarr}, {&in.Radarr, &cur.Radarr}, {&in.Prowlarr, &cur.Prowlarr},
			{&in.Seerr, &cur.Seerr}, {&in.Jellyfin, &cur.Jellyfin}, {&in.QBit, &cur.QBit},
		}
		for _, p := range pairs {
			keep(&p.next.APIKey, p.prev.APIKey)
			keep(&p.next.Password, p.prev.Password)
		}
		keep(&in.AuthPassword, cur.AuthPassword)
		*cur = in
	})
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.statusMu.Lock()
	s.status = nil
	s.statusMu.Unlock()
	s.getSettings(w, r)
}

// ---------------------------------------------------------------------------

func (s *Server) static() http.Handler {
	files := http.FileServerFS(s.ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeErr(w, 404, errors.New("not found"))
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if st, err := fs.Stat(s.ui, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback.
		b, err := fs.ReadFile(s.ui, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(503)
			fmt.Fprintln(w, "roarr: web UI not built (run `make web`)")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

// statusFor maps an error to an HTTP status. A failing upstream service is
// reported as 424, never 502/503/504: reverse proxies and Cloudflare replace
// the body of those with their own error page, which hides roarr's message.
func statusFor(err error) int {
	var he *httpx.Error
	switch {
	case errors.Is(err, app.ErrNotConfigured):
		return http.StatusFailedDependency
	case errors.As(err, &he):
		if he.Status == 404 {
			return 404
		}
		return http.StatusFailedDependency
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusFailedDependency
	}
	return http.StatusInternalServerError
}
