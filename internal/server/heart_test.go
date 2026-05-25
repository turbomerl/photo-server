package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/store"
)

func TestHeartToggleEndpoint(t *testing.T) {
	s := newTestServer(t) // no access gate; sessions enabled
	hash := strings.Repeat("a", 64)
	if _, _, err := s.st.InsertPhoto(store.Photo{
		ContentHash: hash, MIME: "image/jpeg", UploadedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	toggle := func(cookie string) (body struct {
		Count   int64 `json:"count"`
		Hearted bool  `json:"hearted"`
	}, setCookie string, code int) {
		req := httptest.NewRequest(http.MethodPost, "/photo/"+hash+"/heart", nil)
		req.Header.Set("Accept", "application/json")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "ps_session", Value: cookie})
		}
		rec := httptest.NewRecorder()
		s.httpSrv.Handler.ServeHTTP(rec, req)
		code = rec.Code
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		for _, c := range rec.Result().Cookies() {
			if c.Name == "ps_session" {
				setCookie = c.Value
			}
		}
		return
	}

	// First toggle: hearted, count 1; capture the new session cookie.
	b1, cookie, code := toggle("")
	if code != http.StatusOK || !b1.Hearted || b1.Count != 1 {
		t.Fatalf("first toggle: code=%d %+v, want 200 hearted/1", code, b1)
	}
	if cookie == "" {
		t.Fatal("expected a session cookie to be set")
	}
	// Same session toggles off: count back to 0.
	b2, _, _ := toggle(cookie)
	if b2.Hearted || b2.Count != 0 {
		t.Fatalf("second toggle: %+v, want un-hearted/0", b2)
	}

	// Bad hash → 400.
	rb := httptest.NewRequest(http.MethodPost, "/photo/not-a-hash/heart", nil)
	recb := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(recb, rb)
	if recb.Code != http.StatusBadRequest {
		t.Errorf("bad hash = %d, want 400", recb.Code)
	}

	// Valid-form but unknown hash → 404.
	ru := httptest.NewRequest(http.MethodPost, "/photo/"+strings.Repeat("b", 64)+"/heart", nil)
	ru.Header.Set("Accept", "application/json")
	recu := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(recu, ru)
	if recu.Code != http.StatusNotFound {
		t.Errorf("unknown hash = %d, want 404", recu.Code)
	}
}

// No-JS form fallback (no Accept: application/json) redirects back.
func TestHeartFormFallbackRedirects(t *testing.T) {
	s := newTestServer(t)
	hash := strings.Repeat("c", 64)
	if _, _, err := s.st.InsertPhoto(store.Photo{
		ContentHash: hash, MIME: "image/jpeg", UploadedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/photo/"+hash+"/heart", nil)
	req.Header.Set("Referer", "/gallery?sort=top")
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("form fallback = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/gallery?sort=top" {
		t.Errorf("redirect = %q, want /gallery?sort=top", loc)
	}
}
