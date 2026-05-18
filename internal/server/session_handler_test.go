package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getSessionCookie(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "ps_session" {
			return c.Value
		}
	}
	return ""
}

func TestSessionGetEstablishes(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/session", nil)
	s.httpSrv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	tok := getSessionCookie(rec)
	if tok == "" {
		t.Fatal("no ps_session cookie issued")
	}
	var body sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Token != tok {
		t.Errorf("body token %q != cookie %q", body.Token, tok)
	}
}

func TestSessionPostAdoptRecoversAfterCookieLoss(t *testing.T) {
	s := newTestServer(t)

	// First visit establishes a token and sets a name.
	rec1 := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/session", nil))
	tok := getSessionCookie(rec1)

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/session",
		strings.NewReader(`{"display_name":"Bob"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: "ps_session", Value: tok})
	s.httpSrv.Handler.ServeHTTP(rec2, req2)
	var r2 sessionResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &r2)
	if r2.DisplayName != "Bob" {
		t.Fatalf("display_name not set: %q", r2.DisplayName)
	}

	// Cookie dropped (Safari/Private Relay). The page re-presents the
	// localStorage token via POST; the same identity is rebound.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/session",
		strings.NewReader(`{"token":"`+tok+`"}`))
	req3.Header.Set("Content-Type", "application/json")
	s.httpSrv.Handler.ServeHTTP(rec3, req3)

	if got := getSessionCookie(rec3); got != tok {
		t.Fatalf("adopt rebound to %q, want original %q", got, tok)
	}
	var r3 sessionResponse
	_ = json.Unmarshal(rec3.Body.Bytes(), &r3)
	if r3.Token != tok || r3.DisplayName != "Bob" {
		t.Errorf("recovered session = %+v, want token %q name Bob", r3, tok)
	}
}

func TestSessionJSServed(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/session.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "localStorage") {
		t.Error("session.js does not reference localStorage")
	}
}
