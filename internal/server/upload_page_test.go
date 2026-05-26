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

func sessionFrom(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "ps_session" {
			return c.Value
		}
	}
	return ""
}

func TestUploadPagePickerNotCamera(t *testing.T) {
	s := newTestServer(t)
	b := get(t, s, "/upload").Body.String()

	if !strings.Contains(b, "<title>Drop") {
		t.Error("/upload is not the Upload (Drop) page")
	}
	if !strings.Contains(b, `id="up-input"`) || !strings.Contains(b, "multiple") {
		t.Error("upload picker missing multi-select <input>")
	}
	// Upload picks from the photostream — must NOT force the camera
	// (that's Polaroid's job).
	if strings.Contains(b, "capture=") {
		t.Error("upload input must not use capture= (library picker, not camera)")
	}
	if !strings.Contains(b, `src="/static/upload.js"`) {
		t.Error("upload page does not include upload.js")
	}
	if !strings.Contains(b, "Your contributions") {
		t.Error("upload page missing the recent-uploads section")
	}
}

func TestUploadPageRendersRecentServerSide(t *testing.T) {
	s := newTestServer(t)

	// First hit issues the session cookie.
	rec := get(t, s, "/upload")
	tok := sessionFrom(rec)
	if tok == "" {
		t.Fatal("no session issued on /upload")
	}
	if _, _, err := s.st.InsertPhoto(store.Photo{
		ContentHash: "abc123def", MIME: "image/jpeg",
		UploaderSessionID: tok, UploadedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed photo: %v", err)
	}

	// Re-fetch with the cookie: the tile is server-rendered (no JS).
	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	req.AddCookie(&http.Cookie{Name: "ps_session", Value: tok})
	rec2 := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec2, req)
	if !strings.Contains(rec2.Body.String(), "/thumb/abc123def") {
		t.Error("server-rendered recent grid missing the seeded photo")
	}
}

func TestMyUploadsAPI(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/uploads/mine", nil)
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	tok := sessionFrom(rec)
	var first struct {
		Photos []photoTile `json:"photos"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &first)
	if len(first.Photos) != 0 {
		t.Fatalf("new session should have no uploads, got %d", len(first.Photos))
	}

	if _, _, err := s.st.InsertPhoto(store.Photo{
		ContentHash: "feed00", MIME: "image/jpeg",
		UploaderSessionID: tok, UploadedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/uploads/mine", nil)
	req2.AddCookie(&http.Cookie{Name: "ps_session", Value: tok})
	rec2 := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec2, req2)
	var second struct {
		Photos []photoTile `json:"photos"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &second)
	if len(second.Photos) != 1 || second.Photos[0].Hash != "feed00" ||
		second.Photos[0].ThumbURL != "/thumb/feed00" {
		t.Fatalf("my-uploads = %+v, want one feed00 tile", second.Photos)
	}
}

func TestUploadJSServed(t *testing.T) {
	s := newTestServer(t)
	rec := get(t, s, "/static/upload.js")
	if rec.Code != http.StatusOK ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("upload.js code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/upload") || !strings.Contains(body, "XMLHttpRequest") {
		t.Error("upload.js missing the XHR upload path")
	}
}

func TestResizeJSServedAndWired(t *testing.T) {
	s := newTestServer(t)
	rec := get(t, s, "/static/resize.js")
	if rec.Code != http.StatusOK ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("resize.js code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if b := rec.Body.String(); !strings.Contains(b, "psResize") || !strings.Contains(b, "createImageBitmap") {
		t.Error("resize.js missing the client-side downscale logic")
	}
	// Both upload paths must load resize.js (jz9 client-side downscale).
	for _, path := range []string{"/", "/upload"} {
		if !strings.Contains(get(t, s, path).Body.String(), `src="/static/resize.js"`) {
			t.Errorf("%s does not load resize.js", path)
		}
	}
}
