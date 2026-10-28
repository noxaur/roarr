package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/noxaur/roarr/internal/app"
)

// verifyRoutes registers the media verification API.
func (s *Server) verifyRoutes(guarded func(string, http.HandlerFunc)) {
	guarded("GET /api/verify", s.verifyList)
	guarded("GET /api/verify/{hash}", s.verifyGet)
	guarded("POST /api/verify/{hash}/recheck", s.verifyRecheck)
	guarded("POST /api/verify/{hash}/regrab", s.verifyRegrab)
	guarded("POST /api/verify/{hash}/dismiss", s.verifyDismiss)
}

// verifyList: GET /api/verify[?seriesId=&movieId=&level=fail]
func (s *Server) verifyList(w http.ResponseWriter, r *http.Request) {
	v := s.app.Verify
	seriesID, _ := strconv.Atoi(r.URL.Query().Get("seriesId"))
	movieID, _ := strconv.Atoi(r.URL.Query().Get("movieId"))
	level := r.URL.Query().Get("level")
	items := []app.VerifyRecord{}
	for _, rec := range v.Records(seriesID, movieID) {
		if level != "" && (rec.Verdict == nil || string(rec.Verdict.Level) != level) {
			continue
		}
		items = append(items, rec)
	}
	out := map[string]any{"enabled": v.Enabled(), "items": items}
	if v != nil {
		out["tools"] = v.Tools()
		out["autoRegrab"] = v.Settings().AutoRegrab
	}
	writeJSON(w, 200, out)
}

func (s *Server) verifyGet(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.app.Verify.Record(r.PathValue("hash"))
	if !ok {
		writeErr(w, 404, errors.New("download not verified"))
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) verifyRecheck(w http.ResponseWriter, r *http.Request) {
	if s.app.Verify == nil {
		writeErr(w, 404, errors.New("download not verified"))
		return
	}
	// ffmpeg on a large file takes a while; do not die with the browser.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	defer cancel()
	rec, err := s.app.Verify.Recheck(ctx, r.PathValue("hash"))
	if err != nil {
		writeErr(w, verifyStatus(s, r, err), err)
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) verifyRegrab(w http.ResponseWriter, r *http.Request) {
	if s.app.Verify == nil {
		writeErr(w, 404, errors.New("download not verified"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Minute)
	defer cancel()
	rec, err := s.app.Verify.Regrab(ctx, r.PathValue("hash"), false)
	if err != nil {
		writeErr(w, verifyStatus(s, r, err), err)
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) verifyDismiss(w http.ResponseWriter, r *http.Request) {
	if s.app.Verify == nil {
		writeErr(w, 404, errors.New("download not verified"))
		return
	}
	rec, err := s.app.Verify.Dismiss(r.PathValue("hash"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, rec)
}

func verifyStatus(s *Server, r *http.Request, err error) int {
	if _, ok := s.app.Verify.Record(r.PathValue("hash")); !ok {
		return 404
	}
	return statusFor(err)
}
