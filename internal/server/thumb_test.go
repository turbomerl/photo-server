package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/convert"
	"github.com/turbomerl/photo-server/internal/store"
)

const realHEIC = "/home/isambard-poulson/Downloads/classic-car.heic"

// thumbServer builds a Server with a real converter + a seeded HEIC
// original and DB row, so the lazy-regenerate path is exercised.
// Skips if libvips tooling or the sample HEIC is unavailable.
func thumbServer(t *testing.T) (*Server, string) {
	t.Helper()
	if _, err := exec.LookPath("vipsthumbnail"); err != nil {
		t.Skip("vipsthumbnail not on PATH")
	}
	hf, err := os.Open(realHEIC)
	if err != nil {
		t.Skipf("real HEIC sample absent: %v", err)
	}
	defer hf.Close()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "photo-server.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bs, err := blobstore.New(dir)
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	hash, _, err := bs.PutOriginal(hf, ".heic")
	if err != nil {
		t.Fatalf("PutOriginal: %v", err)
	}
	if _, _, err := st.InsertPhoto(store.Photo{
		ContentHash: hash, MIME: "image/heic", UploadedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertPhoto: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	conv, err := convert.NewConverter("vipsthumbnail", bs, 2560, 85, 400, 80, log)
	if err != nil {
		t.Fatalf("NewConverter: %v", err)
	}
	srv := New(":0", Deps{
		Log: log, Version: "test", Store: st, Blobs: bs,
		Conv: conv, MaxBody: 64 << 20,
	})
	return srv, hash
}

func TestThumbLazyRegenerateAndCache(t *testing.T) {
	s, hash := thumbServer(t)

	// Thumb does not exist yet → lazily regenerated and served.
	req := httptest.NewRequest(http.MethodGet, "/thumb/"+hash, nil)
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/webp" {
		t.Errorf("Content-Type = %q, want image/webp", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
	etag := rec.Header().Get("ETag")
	if etag != `"`+hash+`"` {
		t.Errorf("ETag = %q, want %q", etag, `"`+hash+`"`)
	}
	body := rec.Body.Bytes()
	if !(len(body) > 12 && string(body[0:4]) == "RIFF" && string(body[8:12]) == "WEBP") {
		t.Fatalf("body is not webp (first bytes: % x)", body[:min(12, len(body))])
	}
	if !s.blobs.Exists(blobstore.Thumb, hash, "") {
		t.Error("thumb should now be persisted on disk")
	}

	// Conditional request with the ETag → 304 Not Modified.
	req2 := httptest.NewRequest(http.MethodGet, "/thumb/"+hash, nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional GET status = %d, want 304", rec2.Code)
	}
}

func TestThumbBadHash(t *testing.T) {
	s, _ := thumbServer(t)
	req := httptest.NewRequest(http.MethodGet, "/thumb/not-a-valid-hash", nil)
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestThumbUnknownPhoto(t *testing.T) {
	s, _ := thumbServer(t)
	// Well-formed hash, but no such photo → 404 (nothing to regen).
	ghost := "1111111111111111111111111111111111111111111111111111111111111111"
	req := httptest.NewRequest(http.MethodGet, "/thumb/"+ghost, nil)
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
