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
		// guest gate. /static/fonts/ is open so the gate page's own
		// self-hosted fonts load before a guest is through the gate.
		if r.URL.Path == "/healthz" || r.URL.Path == "/admin" ||
			strings.HasPrefix(r.URL.Path, "/admin/") ||
			strings.HasPrefix(r.URL.Path, "/static/fonts/") {
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
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#fdf3e2">
<meta name="robots" content="noindex, nofollow">
<title>Wedding photos · M&I</title>


<style>
  /* Self-hosted fonts (served from /static/fonts/; exempt from the gate). */
  @font-face{font-family:'Pinyon Script';font-style:normal;font-weight:400;font-display:swap;src:url(/static/fonts/pinyon-script.woff2) format('woff2')}
  @font-face{font-family:'Libre Baskerville';font-style:normal;font-weight:400 700;font-display:swap;src:url(/static/fonts/libre-baskerville.woff2) format('woff2')}
  @font-face{font-family:'Libre Baskerville';font-style:italic;font-weight:400;font-display:swap;src:url(/static/fonts/libre-baskerville-italic.woff2) format('woff2')}

  :root {
    --paper: #fdf3e2; --paper-alt: #f6e4c8; --paper-card: #fefdf6;
    --ink: #2a1c12; --ink-soft: #6a4a32; --line: #e8d5b2;
    --accent: #e8631d; --accent-dk: #b34614; --stamp: #d63f2c;
    --f-script: "Pinyon Script", "Snell Roundhand", cursive;
    --f-serif:  "Libre Baskerville", Baskerville, serif;
    --grain: url("data:image/svg+xml;utf8,%3Csvg xmlns='http://www.w3.org/2000/svg' width='240' height='240'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' seed='3'/%3E%3CfeColorMatrix values='0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 0.06 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  html, body { background: var(--paper); color: var(--ink);
    font: 15px/1.45 var(--f-serif); -webkit-font-smoothing: antialiased; }
  body { min-height: 100vh; background-image: var(--grain);
         display: flex; flex-direction: column; align-items: center; }

  .stamp-bar {
    width: 100%; max-width: 480px;
    padding: calc(14px + env(safe-area-inset-top,0)) 18px 0;
    display: flex; justify-content: space-between;
    font: 700 9px var(--f-serif); color: var(--ink-soft);
    letter-spacing: 1.5px; text-transform: uppercase;
  }
  main {
    flex: 1; padding: 8px 24px 40px;
    max-width: 480px; width: 100%;
    display: flex; flex-direction: column; align-items: center;
    text-align: center;
  }
  .lockup { margin: 56px 0 6px;
    display: flex; align-items: baseline; gap: 14px; line-height: 1; }
  .lockup .m, .lockup .i { font: 92px/0.85 var(--f-script); color: var(--ink); }
  .lockup .amp { font: italic 28px var(--f-serif); color: var(--ink-soft); }

  .divider { display: flex; align-items: center; gap: 10px;
    margin-top: 32px; width: 70%; }
  .divider .line { flex: 1; height: 1px; background: var(--line); }
  .divider .label {
    font: italic 12px var(--f-serif); color: var(--ink-soft);
    letter-spacing: 1px; text-transform: uppercase;
  }
  .welcome { font: 15px/1.45 var(--f-serif); color: var(--ink);
    margin-top: 32px; max-width: 280px; }
  .welcome em { display: block; font: italic 13px var(--f-serif);
    color: var(--ink-soft); margin-top: 8px; }
  .err { font: italic 13px var(--f-serif); color: var(--stamp);
    margin-top: 14px; }

  .gate-form { position: relative; margin-top: 44px; width: 100%; max-width: 300px; }
  .pw-label-tag {
    position: absolute; top: -12px; left: 50%;
    transform: translateX(-50%) rotate(-2deg);
    background: var(--stamp); color: #fefdf8;
    padding: 3px 12px;
    font: 700 10px var(--f-serif); letter-spacing: 3px;
    text-transform: uppercase;
    box-shadow: 0 2px 4px rgba(0,0,0,0.15);
    z-index: 2;
  }
  .pw-card {
    background: #fefdf6;
    padding: 22px 18px 18px;
    box-shadow: 0 10px 22px -8px rgba(40,30,20,0.32), 0 1px 2px rgba(40,30,20,0.18);
    outline: 1px solid var(--line);
    text-align: left;
  }
  .pw-row {
    display: flex; align-items: center; gap: 8px;
    border-bottom: 2px solid var(--ink); padding-bottom: 8px;
  }
  .pw-row input {
    flex: 1; min-width: 0;
    font: 18px var(--f-serif); color: var(--ink);
    letter-spacing: 3px; background: transparent; border: 0; outline: 0;
    padding: 4px 0;
  }
  .pw-row input::placeholder { color: var(--ink-soft); opacity: 0.4;
    letter-spacing: 0; font-style: italic; font-size: 15px; }
  .pw-eye {
    background: transparent; border: 0; padding: 4px; cursor: pointer;
    color: var(--ink-soft); display: flex;
  }
  .pw-eye svg { width: 18px; height: 18px; stroke: currentColor; fill: none;
    stroke-width: 1.5; stroke-linecap: round; stroke-linejoin: round; }

  .submit {
    margin-top: 36px; padding: 14px 36px;
    background: var(--ink); color: var(--paper);
    border: 0;
    font: 700 16px var(--f-serif); letter-spacing: 0.5px;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.08), 0 6px 14px -4px rgba(0,0,0,0.4);
    display: inline-flex; align-items: center; gap: 12px;
  }
  .submit::before {
    content: ""; width: 10px; height: 10px; border-radius: 50%;
    background: var(--accent); box-shadow: 0 0 8px rgba(232,99,29,0.6);
  }
  .submit:active { transform: translateY(1px); }

  .hint {
    margin-top: 44px;
    font: italic 15px var(--f-serif); color: var(--ink-soft);
  }
  .hint b { font-weight: normal; color: var(--accent-dk);
    text-decoration: underline; text-decoration-style: dotted; }
</style>
</head>
<body>
  <div class="stamp-bar">
    <span>30·06·26</span>
    <span>Mariam &amp; Isambard</span>
  </div>

  <main>
    <div class="lockup">
      <span class="m">M</span><span class="amp">&amp;</span><span class="i">I</span>
    </div>

    <div class="divider"><span class="line"></span><span class="label">The photo booth</span><span class="line"></span></div>

    <p class="welcome">
      You found us.
      <em>Tap in the password from your invitation card and you're in.</em>
    </p>

    {{if .Bad}}<p class="err">That password didn't match — give it another go.</p>{{end}}

    <form class="gate-form" method="post" action="/access" autocomplete="off">
      <span class="pw-label-tag">Password</span>
      <div class="pw-card">
        <div class="pw-row">
          <input id="pw" name="password" type="password"
                 autofocus required
                 spellcheck="false" autocapitalize="off" autocorrect="off"
                 autocomplete="current-password"
                 placeholder="From your invitation card">
          <button type="button" class="pw-eye" id="pw-eye"
                  aria-label="Show password" aria-pressed="false">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/>
              <circle cx="12" cy="12" r="3"/>
            </svg>
          </button>
        </div>
      </div>
      <button type="submit" class="submit">Come on in</button>
    </form>

    <p class="hint">lost the card? <b>ask M or I</b></p>
  </main>

  <script>
    (function () {
      var pw = document.getElementById('pw');
      var eye = document.getElementById('pw-eye');
      if (eye && pw) eye.addEventListener('click', function () {
        var showing = pw.type === 'text';
        pw.type = showing ? 'password' : 'text';
        eye.setAttribute('aria-pressed', showing ? 'false' : 'true');
      });
    })();
  </script>
</body>
</html>
`
