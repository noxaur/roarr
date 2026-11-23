package server

import (
	"net/http"
)

// nativeRoutes registers the native-mode API: the blocklist, and moving
// from Sonarr/Radarr to roarr.
func (s *Server) nativeRoutes(guarded func(string, http.HandlerFunc)) {
	guarded("GET /api/blocklist", s.blocklist)
	guarded("DELETE /api/blocklist/{id}", s.unblock)
	guarded("GET /api/takeover", s.takeoverPlan)
	guarded("POST /api/takeover", s.startTakeover)
}

// takeoverPlan: GET /api/takeover. What taking over the Sonarr/Radarr
// library would move, what roarr still needs, and a running takeover's
// progress.
func (s *Server) takeoverPlan(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.app.TakeoverPlan(r.Context()))
}

// startTakeover: POST /api/takeover. Starts the takeover; poll GET.
func (s *Server) startTakeover(w http.ResponseWriter, r *http.Request) {
	p, err := s.app.StartTakeover(r.Context())
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "plan": p})
		return
	}
	writeJSON(w, 202, p)
}

// blocklist: GET /api/blocklist
func (s *Server) blocklist(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": s.app.Blocklist().List()})
}

// unblock: DELETE /api/blocklist/{id}
func (s *Server) unblock(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Blocklist().Remove(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
