package session

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/store"
)

func newManager(t *testing.T, secure bool) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "photo-server.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewManager(st, 30*24*time.Hour, secure)
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	return nil
}

func TestTokenValidity(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b {
		t.Fatal("two tokens collided")
	}
	if !ValidToken(a) || !ValidToken(b) {
		t.Fatal("freshly minted token failed ValidToken")
	}
	for _, bad := range []string{"", "short", "has spaces and+slash/=", a + "x"} {
		if ValidToken(bad) {
			t.Errorf("ValidToken(%q) = true, want false", bad)
		}
	}
}

func TestEnsureIssuesAndPersistsCookie(t *testing.T) {
	m := newManager(t, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, err := m.Ensure(rec, req)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	c := sessionCookie(rec)
	if c == nil {
		t.Fatal("no session cookie set")
	}
	if c.Value != sess.ID || !ValidToken(c.Value) {
		t.Fatalf("cookie %q vs session %q", c.Value, sess.ID)
	}
	if !c.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if c.Secure {
		t.Error("cookie must NOT be Secure (plain-HTTP LAN, PRD F4)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.MaxAge <= 0 {
		t.Errorf("MaxAge = %d, want long-lived", c.MaxAge)
	}

	// Re-presenting the cookie resolves the same session.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: CookieName, Value: c.Value})
	sess2, err := m.Ensure(rec2, req2)
	if err != nil || sess2.ID != sess.ID {
		t.Fatalf("re-Ensure: id=%q err=%v, want %q", sess2.ID, err, sess.ID)
	}
}

func TestSecureCookieWhenHTTPS(t *testing.T) {
	m := newManager(t, true)
	rec := httptest.NewRecorder()
	if _, err := m.Ensure(rec, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	c := sessionCookie(rec)
	if c == nil {
		t.Fatal("no session cookie set")
	}
	if !c.Secure {
		t.Error("cookie must be Secure when BaseURL is https (cloud/Caddy)")
	}
	// HttpOnly + SameSite must not regress when Secure flips on.
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("HttpOnly=%v SameSite=%v, want true/Lax", c.HttpOnly, c.SameSite)
	}
}

func TestAdoptRebindsLocalStorageToken(t *testing.T) {
	m := newManager(t, false)

	// Simulate a returning guest whose cookie was dropped but whose
	// localStorage token survived.
	ls := NewToken()
	rec := httptest.NewRecorder()
	sess, err := m.Adopt(rec, ls)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if sess.ID != ls {
		t.Fatalf("adopted id = %q, want %q", sess.ID, ls)
	}
	if c := sessionCookie(rec); c == nil || c.Value != ls {
		t.Fatalf("cookie not rebound to localStorage token")
	}

	// Garbage token → a fresh valid one is issued instead of erroring.
	rec2 := httptest.NewRecorder()
	s2, err := m.Adopt(rec2, "not-a-real-token")
	if err != nil {
		t.Fatalf("Adopt(garbage): %v", err)
	}
	if s2.ID == "" || !ValidToken(s2.ID) || s2.ID == ls {
		t.Fatalf("garbage adopt should mint a fresh token, got %q", s2.ID)
	}
}

func TestSetDisplayNamePersists(t *testing.T) {
	m := newManager(t, false)
	rec := httptest.NewRecorder()
	sess, _ := m.Ensure(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if err := m.SetDisplayName(sess.ID, "Aunt Sue"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	got, ok, err := m.st.GetSession(sess.ID)
	if err != nil || !ok || got.DisplayName != "Aunt Sue" {
		t.Fatalf("display name not persisted: %+v ok=%v err=%v", got, ok, err)
	}
	// Empty name must not clobber it (re-Ensure bumps last_seen only).
	_, _ = m.Ensure(httptest.NewRecorder(), withCookie(sess.ID))
	got, _, _ = m.st.GetSession(sess.ID)
	if got.DisplayName != "Aunt Sue" {
		t.Errorf("name lost after re-Ensure: %q", got.DisplayName)
	}
}

func withCookie(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	return r
}
