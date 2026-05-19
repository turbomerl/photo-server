package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/store"
)

// seedJPEG stores a JPEG original + its photo row and returns the hash.
func seedJPEG(t *testing.T, s *Server, filename, name string) string {
	t.Helper()
	data := jpegWithEXIF("2024:06:01 12:00:00")
	hash, _, err := s.blobs.PutOriginal(bytes.NewReader(data), ".jpg")
	if err != nil {
		t.Fatalf("PutOriginal: %v", err)
	}
	if _, _, err := s.st.InsertPhoto(store.Photo{
		ContentHash: hash, MIME: "image/jpeg",
		OriginalFilename: filename, DisplayName: name,
		UploadedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertPhoto: %v", err)
	}
	return hash
}

func TestPhotoViewJPEGServesOriginal(t *testing.T) {
	s := newTestServer(t)
	h := seedJPEG(t, s, "IMG_9.JPG", "Aunt Sue")

	rec := get(t, s, "/photo/"+h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	b := rec.Body.Bytes()
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		t.Error("body is not the JPEG original")
	}
}

func TestOriginalDownloadAttachment(t *testing.T) {
	s := newTestServer(t)
	h := seedJPEG(t, s, "IMG_9.JPG", "")

	rec := get(t, s, "/original/"+h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, `filename="IMG_9.JPG"`) {
		t.Errorf("Content-Disposition = %q, want attachment IMG_9.JPG", cd)
	}
}

func TestOriginalDownloadFallbackName(t *testing.T) {
	s := newTestServer(t)
	// Empty original filename → <hash>.jpg.
	h := seedJPEG(t, s, "", "")
	rec := get(t, s, "/original/"+h)
	if !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="`+h+`.jpg"`) {
		t.Errorf("fallback name wrong: %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestPhotoPageServerRendered(t *testing.T) {
	s := newTestServer(t)
	h := seedJPEG(t, s, "x.jpg", "Bob")

	b := get(t, s, "/p/"+h).Body.String()
	if !strings.Contains(b, `src="/photo/`+h+`"`) {
		t.Error("photo page missing full-size image")
	}
	if !strings.Contains(b, `href="/original/`+h+`"`) {
		t.Error("photo page missing download-original link")
	}
	if !strings.Contains(b, "Shared by Bob") {
		t.Error("photo page missing uploader name")
	}
	if !strings.Contains(b, `href="/gallery"`) {
		t.Error("photo page missing back-to-gallery / nav")
	}
}

func TestPhotoRoutesHiddenAndBadInputs(t *testing.T) {
	s := newTestServer(t)
	h := seedJPEG(t, s, "h.jpg", "")
	if _, err := s.st.DB().Exec(
		`UPDATE photos SET hidden_at=1 WHERE content_hash=?`, h); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/photo/" + h, "/original/" + h, "/p/" + h} {
		if rec := get(t, s, p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s (hidden) = %d, want 404", p, rec.Code)
		}
	}
	if rec := get(t, s, "/photo/not-hex"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad hash = %d, want 400", rec.Code)
	}
	ghost := strings.Repeat("a", 64)
	if rec := get(t, s, "/p/"+ghost); rec.Code != http.StatusNotFound {
		t.Errorf("unknown = %d, want 404", rec.Code)
	}
}

func TestGalleryTilesAreLinks(t *testing.T) {
	s := newTestServer(t)
	seedPhotos(t, s, 3)
	b := get(t, s, "/gallery").Body.String()
	if !strings.Contains(b, `<a href="/p/h0002" data-name=`) {
		t.Error("gallery tiles are not links to the photo view")
	}
	if !strings.Contains(b, `src="/static/viewer.js"`) {
		t.Error("gallery does not load the lightbox script")
	}
}

func TestViewerJSServed(t *testing.T) {
	s := newTestServer(t)
	rec := get(t, s, "/static/viewer.js")
	if rec.Code != http.StatusOK ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("viewer.js code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "/photo/") ||
		!strings.Contains(rec.Body.String(), "/original/") {
		t.Error("viewer.js missing the full-size/download wiring")
	}
}

// HEIC path: full-size view must serve the gallery JPEG, lazily
// regenerating it if absent. Uses the real-converter test server.
func TestPhotoViewHEICRegeneratesGalleryJPEG(t *testing.T) {
	s, h := thumbServer(t) // seeds a real iPhone HEIC original + row

	if s.blobs.Exists(blobstore.Gallery, h, "") {
		t.Fatal("precondition: gallery JPEG should not exist yet")
	}
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/photo/"+h, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg (HEIC→gallery JPEG)", ct)
	}
	body := rec.Body.Bytes()
	if len(body) < 4 || body[0] != 0xFF || body[1] != 0xD8 {
		t.Error("served body is not a JPEG")
	}
	if !s.blobs.Exists(blobstore.Gallery, h, "") {
		t.Error("gallery JPEG was not persisted by the lazy regenerate")
	}

	// And /original serves the untouched HEIC.
	rec2 := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/original/"+h, nil))
	if rec2.Code != http.StatusOK ||
		!strings.Contains(rec2.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("original download: code=%d cd=%q", rec2.Code,
			rec2.Header().Get("Content-Disposition"))
	}
}
