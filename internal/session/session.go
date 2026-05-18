// Package session manages the persistent guest identity (kgu.14).
//
// A guest gets a long-lived random token in an HttpOnly cookie on
// first contact, plus a JS-readable copy mirrored to localStorage by
// the bundled session.js. iOS Private Relay / Safari ITP routinely
// drop cookies mid-event; when that happens the page re-presents the
// localStorage token to Adopt, which rebinds the same identity so the
// guest keeps their display name and their uploads stay grouped.
//
// Security posture is PRD N11: a trusted-guest LAN with no adversary.
// The token is an opaque continuity key, not auth — accepting a
// client-presented token to (re)bind a session is intentional, and
// losing it only re-prompts for a name (PRD R6), never blocks uploads.
package session

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/turbomerl/photo-server/internal/store"
)

// CookieName is the session cookie / localStorage key.
const CookieName = "ps_session"

// tokenBytes is the entropy of a session token (256-bit).
const tokenBytes = 32

// Manager issues and resolves guest sessions.
type Manager struct {
	st     *store.Store
	maxAge time.Duration
}

// NewManager builds a session manager. maxAge is the cookie lifetime
// (PRD: no expiry within the event window).
func NewManager(st *store.Store, maxAge time.Duration) *Manager {
	return &Manager{st: st, maxAge: maxAge}
}

// NewToken returns a fresh URL-safe random token.
func NewToken() string {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is fatal for the process anyway; panic
		// is appropriate — we must never issue a predictable token.
		panic("session: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ValidToken reports whether s is a well-formed token (right length,
// base64url alphabet). Cheap junk rejection before touching the DB.
func ValidToken(s string) bool {
	if len(s) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil
}

// Ensure resolves the session from the request cookie, creating one
// (new token + row + Set-Cookie) if absent or malformed. The cookie is
// always (re)written so its Max-Age slides forward on every visit.
func (m *Manager) Ensure(w http.ResponseWriter, r *http.Request) (store.Session, error) {
	token := ""
	if c, err := r.Cookie(CookieName); err == nil && ValidToken(c.Value) {
		token = c.Value
	}
	if token == "" {
		token = NewToken()
	}
	return m.bind(w, token)
}

// Adopt rebinds the response to a client-presented token (recovered
// from localStorage after the cookie was dropped). Invalid tokens fall
// back to issuing a fresh one so the guest is never stuck.
func (m *Manager) Adopt(w http.ResponseWriter, token string) (store.Session, error) {
	if !ValidToken(token) {
		token = NewToken()
	}
	return m.bind(w, token)
}

// bind ensures the sessions row exists for token, (re)writes the
// cookie, and returns the row.
func (m *Manager) bind(w http.ResponseWriter, token string) (store.Session, error) {
	// Empty name: creates the row if missing, bumps last_seen_at, and
	// preserves any existing display name (store.UpsertSession).
	if err := m.st.UpsertSession(token, ""); err != nil {
		return store.Session{}, err
	}
	m.setCookie(w, token)
	sess, _, err := m.st.GetSession(token)
	return sess, err
}

// SetDisplayName stores the guest's chosen name against their session.
func (m *Manager) SetDisplayName(token, name string) error {
	return m.st.UpsertSession(token, name)
}

// setCookie writes the session cookie.
//
//   - HttpOnly: JS can't read it (the localStorage mirror is the
//     JS-visible copy, refreshed via /session).
//   - Secure=false: the appliance serves plain HTTP on the LAN
//     (PRD F4 / §9.7); a Secure cookie would be silently dropped.
//   - SameSite=Lax: same-origin app; safe default behind the captive
//     portal.
func (m *Manager) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(m.maxAge.Seconds()),
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})
}
