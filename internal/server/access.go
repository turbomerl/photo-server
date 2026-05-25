// Event access gate (ycl). On the public internet the guest surface —
// everything except /healthz and the separately-authed /admin* — sits
// behind a single shared event password. The entry QR carries it as ?k=
// for scan-to-enter; the gate page also accepts it typed. A successful
// check sets a long-lived HttpOnly cookie so the guest browses normally
// afterwards.
//
// This is event-level access (prove you have the invite), not per-guest
// auth — the app stays account-less (PRD). An empty AccessPassword
// disables the gate entirely (only safe on a trusted LAN).
package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const accessCookie = "ps_access"

// accessCookieValue is the gate cookie's value: a hex SHA-256 of the
// password, so the raw secret is never echoed back in a cookie while
// staying unguessable without it.
func accessCookieValue(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// requireAccess gates the guest surface behind s.accessPassword. It is a
// no-op (returns next unchanged) when no password is configured.
func (s *Server) requireAccess(next http.Handler) http.Handler {
	if s.accessPassword == "" {
		return next
	}
	wantCookie := accessCookieValue(s.accessPassword)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health check + the separately-authed admin surface bypass the
		// guest gate.
		if r.URL.Path == "/healthz" || r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			next.ServeHTTP(w, r)
			return
		}

		// Already through the gate?
		if c, err := r.Cookie(accessCookie); err == nil &&
			subtle.ConstantTimeCompare([]byte(c.Value), []byte(wantCookie)) == 1 {
			next.ServeHTTP(w, r)
			return
		}

		// Scan-to-enter: the key rides in ?k=. On a match set the cookie,
		// then redirect to the same path with k stripped so the secret
		// doesn't linger in the address bar, history, or any Referer.
		if k := r.URL.Query().Get("k"); k != "" &&
			subtle.ConstantTimeCompare([]byte(k), []byte(s.accessPassword)) == 1 {
			s.setAccessCookie(w)
			q := r.URL.Query()
			q.Del("k")
			clean := &url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
			http.Redirect(w, r, clean.RequestURI(), http.StatusSeeOther)
			return
		}

		// Typed password from the gate form.
		if r.Method == http.MethodPost && r.URL.Path == "/access" {
			if subtle.ConstantTimeCompare([]byte(r.PostFormValue("password")), []byte(s.accessPassword)) == 1 {
				s.setAccessCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			s.renderGate(w, http.StatusUnauthorized, true)
			return
		}

		// Otherwise: the gate page.
		s.renderGate(w, http.StatusOK, false)
	})
}

func (s *Server) setAccessCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookie,
		Value:    accessCookieValue(s.accessPassword),
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

var tplGate = template.Must(template.New("gate").Parse(gateHTML))

// renderGate serves the self-contained gate page (the guest assets are
// themselves gated, so the page carries its own inline CSS).
func (s *Server) renderGate(w http.ResponseWriter, status int, badPassword bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(status)
	_ = tplGate.Execute(w, map[string]any{"Bad": badPassword})
}

const gateHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Wedding photos</title>
<style>
  :root { color-scheme: light dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
    font:17px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    background:#faf7f2; color:#1c1c1e; }
  .card { width:min(92vw, 22rem); padding:28px 24px; text-align:center;
    background:#fff; border-radius:16px; box-shadow:0 6px 30px rgba(0,0,0,.08); }
  h1 { font-size:22px; margin:0 0 6px; }
  p { color:#666; margin:0 0 18px; }
  form { display:flex; gap:8px; }
  input { flex:1; min-width:0; font:inherit; padding:11px 12px; border:1px solid #ccc; border-radius:10px; }
  button { font:inherit; padding:11px 16px; border:0; border-radius:10px; cursor:pointer;
    background:#1c1c1e; color:#fff; }
  .err { color:#b00020; font-size:14px; margin:0 0 12px; }
  .hint { margin-top:16px; font-size:13px; color:#999; }
</style>
</head>
<body>
  <div class="card">
    <h1>📸 Wedding photos</h1>
    <p>These photos are private. Enter the event password to continue.</p>
    {{if .Bad}}<p class="err">That password didn't match — try again.</p>{{end}}
    <form method="post" action="/access">
      <input type="password" name="password" placeholder="Event password"
        autofocus autocomplete="current-password" aria-label="Event password">
      <button type="submit">Enter</button>
    </form>
    <p class="hint">Or scan the QR on your table card.</p>
  </div>
</body>
</html>
`
