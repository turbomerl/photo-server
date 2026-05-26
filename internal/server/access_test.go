package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/session"
	"github.com/turbomerl/photo-server/internal/store"
)

func accessServer(t *testing.T, password string) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "photo-server.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, _ := blobstore.New(dir)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(":0", Deps{
		Log: log, Version: "test", Store: st, Blobs: blobs,
		Sessions:       session.NewManager(st, time.Hour, false),
		MaxBody:        64 << 20,
		AdminPassword:  "pw",
		AccessPassword: password,
	})
}

func accessGet(t *testing.T, s *Server, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	return rec
}

// isGate reports whether the body is the access gate page (vs the app).
func isGate(body string) bool {
	return strings.Contains(body, `action="/access"`)
}

func accessCookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == accessCookie {
			return c
		}
	}
	return nil
}

func TestGateBlocksUnauthed(t *testing.T) {
	s := accessServer(t, "letmein")
	rec := accessGet(t, s, "/")
	if !isGate(rec.Body.String()) {
		t.Fatal("unauthed / should serve the gate page")
	}
}

func TestGateDisabledWhenNoPassword(t *testing.T) {
	s := accessServer(t, "") // gate off
	rec := accessGet(t, s, "/")
	if rec.Code != http.StatusOK || isGate(rec.Body.String()) {
		t.Fatalf("no password -> app should serve directly (code=%d gate=%v)", rec.Code, isGate(rec.Body.String()))
	}
}

func TestKeyInQuerySetsCookieAndRedirects(t *testing.T) {
	s := accessServer(t, "letmein")
	rec := accessGet(t, s, "/?k=letmein")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want / (key stripped)", loc)
	}
	c := accessCookieOf(rec)
	if c == nil {
		t.Fatal("no ps_access cookie set after valid key")
	}
	if c.Value != accessCookieValue("letmein") || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie attrs off: value/httponly/samesite = %q/%v/%v", c.Value, c.HttpOnly, c.SameSite)
	}
}

func TestWrongKeyShowsGate(t *testing.T) {
	s := accessServer(t, "letmein")
	rec := accessGet(t, s, "/?k=nope")
	if rec.Code == http.StatusSeeOther {
		t.Fatal("wrong key must not redirect/grant")
	}
	if !isGate(rec.Body.String()) {
		t.Fatal("wrong key should show the gate")
	}
	if accessCookieOf(rec) != nil {
		t.Fatal("wrong key must not set a cookie")
	}
}

func TestValidCookieGrantsEntry(t *testing.T) {
	s := accessServer(t, "letmein")
	ck := &http.Cookie{Name: accessCookie, Value: accessCookieValue("letmein")}
	rec := accessGet(t, s, "/", ck)
	if rec.Code != http.StatusOK || isGate(rec.Body.String()) {
		t.Fatalf("valid cookie should serve the app (code=%d gate=%v)", rec.Code, isGate(rec.Body.String()))
	}
}

func TestFormPasswordGrantsAndRejects(t *testing.T) {
	s := accessServer(t, "letmein")

	// Correct password -> cookie + redirect to /.
	form := url.Values{"password": {"letmein"}}
	req := httptest.NewRequest(http.MethodPost, "/access", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || accessCookieOf(rec) == nil {
		t.Fatalf("correct form password: code=%d cookie=%v, want 303 + cookie", rec.Code, accessCookieOf(rec) != nil)
	}

	// Wrong password -> 401 gate, no cookie.
	form = url.Values{"password": {"wrong"}}
	req = httptest.NewRequest(http.MethodPost, "/access", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !isGate(rec.Body.String()) || accessCookieOf(rec) != nil {
		t.Fatalf("wrong form password: code=%d gate=%v cookie=%v", rec.Code, isGate(rec.Body.String()), accessCookieOf(rec) != nil)
	}
}

func TestHealthzAndAdminBypassGate(t *testing.T) {
	s := accessServer(t, "letmein")

	// /healthz is open for monitoring.
	if rec := accessGet(t, s, "/healthz"); rec.Code != http.StatusOK || isGate(rec.Body.String()) {
		t.Fatalf("/healthz should bypass the gate (code=%d)", rec.Code)
	}
	// /admin reaches its own auth (401), not the guest gate page.
	rec := accessGet(t, s, "/admin")
	if isGate(rec.Body.String()) {
		t.Fatal("/admin should hit admin auth, not the guest gate")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/admin without creds = %d, want 401", rec.Code)
	}
}

func TestNoindexHeader(t *testing.T) {
	s := accessServer(t, "letmein")
	rec := accessGet(t, s, "/")
	if got := rec.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
}

func TestSelfHostedFontsBypassGate(t *testing.T) {
	s := accessServer(t, "letmein") // gate ON

	// The gate page is served to unauthed guests and uses the fonts, so
	// /static/fonts/ must be reachable without the access cookie.
	rec := accessGet(t, s, "/static/fonts/pinyon-script.woff2")
	if rec.Code != http.StatusOK {
		t.Fatalf("font behind gate = %d, want 200 (exempt)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
		t.Errorf("font content-type = %q, want font/woff2", ct)
	}
	if rec := accessGet(t, s, "/static/fonts/nope.woff2"); rec.Code != http.StatusNotFound {
		t.Errorf("missing font = %d, want 404", rec.Code)
	}

	// The gate page declares self-hosted fonts, never an external CDN.
	gate := accessGet(t, s, "/").Body.String()
	if !strings.Contains(gate, "/static/fonts/") {
		t.Error("gate page missing self-hosted @font-face")
	}
	if strings.Contains(gate, "googleapis") || strings.Contains(gate, "gstatic") {
		t.Error("gate page references an external font CDN")
	}
}
