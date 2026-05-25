package server

import (
	"net/http"
	"strings"
)

// handleHeart toggles the current guest session's heart on a visible
// photo (kgu.23). The JS client sends Accept: application/json and gets
// {count, hearted} back for an optimistic UI; the no-JS <form> fallback
// gets a PRG redirect back to the page it was submitted from.
func (s *Server) handleHeart(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isHexSHA256(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	if s.sessions == nil {
		http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	sess, err := s.sessions.Ensure(w, r)
	if err != nil {
		s.log.Error("heart session", "err", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	count, hearted, ok, err := s.st.ToggleHeart(hash, sess.ID)
	if err != nil {
		s.log.Error("toggle heart", "hash", hash, "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, map[string]any{"count": count, "hearted": hearted})
		return
	}
	// No-JS fallback: back to the gallery/photo page that posted.
	dest := r.Referer()
	if dest == "" {
		dest = "/p/" + hash
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}
