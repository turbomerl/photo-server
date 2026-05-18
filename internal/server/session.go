package server

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/turbomerl/photo-server/internal/store"
)

//go:embed assets/session.js
var sessionJS []byte

// sessionResponse is the JSON the client mirrors into localStorage.
// Returning the token in the body is deliberate: it is the only way to
// copy an HttpOnly cookie to JS-readable storage, and the token is an
// opaque continuity key on a trusted LAN (PRD N11), not a secret.
type sessionResponse struct {
	Token       string `json:"token"`
	DisplayName string `json:"display_name"`
}

// handleSession establishes or resolves the guest session.
//
//	GET  /session            → ensure via cookie, return token + name
//	POST /session {token?,    → adopt a localStorage token (cookie-loss
//	               display_name?}  recovery) and/or set the display name
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Token       string `json:"token"`
		DisplayName string `json:"display_name"`
	}
	if r.Method == http.MethodPost {
		dec := json.NewDecoder(io.LimitReader(r.Body, 4096))
		_ = dec.Decode(&body) // tolerate empty/garbage body
	}

	se, err := s.resolveSession(w, r, body.Token)
	if err != nil {
		s.log.Error("session resolve", "err", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	if name := strings.TrimSpace(body.DisplayName); name != "" {
		if err := s.sessions.SetDisplayName(se.ID, name); err != nil {
			s.log.Error("set display name", "err", err)
		} else {
			se.DisplayName = name
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(sessionResponse{
		Token:       se.ID,
		DisplayName: se.DisplayName,
	})
}

// resolveSession adopts a client-presented token (localStorage
// recovery after cookie loss) when one is given, otherwise resolves or
// issues the session from the cookie. Either way the cookie is
// (re)written.
func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request, token string) (store.Session, error) {
	if token != "" {
		return s.sessions.Adopt(w, token)
	}
	return s.sessions.Ensure(w, r)
}

// handleSessionJS serves the bundled localStorage-mirror script.
func (s *Server) handleSessionJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	// Versioned implicitly by the binary; revalidate cheaply on LAN.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(sessionJS)
}
