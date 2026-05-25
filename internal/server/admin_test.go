package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/session"
	"github.com/turbomerl/photo-server/internal/store"
)

// adminTestServer mirrors newTestServer but sets an AdminPassword so
// the /admin surface is enabled.
func adminTestServer(t *testing.T, password string) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "photo-server.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.New(dir)
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(":0", Deps{
		Log: log, Version: "test", Store: st, Blobs: blobs,
		Sessions:      session.NewManager(st, 24*time.Hour, false),
		MaxBody:       64 << 20,
		AdminPassword: password,
	})
}

func doAdmin(t *testing.T, s *Server, method, path, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if password != "" {
		req.SetBasicAuth("admin", password)
	}
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminDisabledWhenNoPassword(t *testing.T) {
	s := newTestServer(t) // no AdminPassword
	for _, p := range []string{
		"/admin",
		"/admin/photos/0000000000000000000000000000000000000000000000000000000000000000/hide",
		"/admin/shutdown",
	} {
		method := http.MethodGet
		if strings.Contains(p, "/photos/") || p == "/admin/shutdown" {
			method = http.MethodPost
		}
		rec := doAdmin(t, s, method, p, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 (fail-closed)", method, p, rec.Code)
		}
	}
}

func TestAdminBasicAuth(t *testing.T) {
	s := adminTestServer(t, "swordfish")

	rec := doAdmin(t, s, http.MethodGet, "/admin", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no creds = %d, want 401", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Basic ") {
		t.Errorf("missing WWW-Authenticate: %q", rec.Header().Get("WWW-Authenticate"))
	}
	if rec := doAdmin(t, s, http.MethodGet, "/admin", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong creds = %d, want 401", rec.Code)
	}
	rec = doAdmin(t, s, http.MethodGet, "/admin", "swordfish")
	if rec.Code != http.StatusOK {
		t.Fatalf("good creds = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestAdminDashboardShowsStatsAndPhotos(t *testing.T) {
	s := adminTestServer(t, "pw")
	seedJPEG(t, s, "hello.jpg", "Bob")
	rec := doAdmin(t, s, http.MethodGet, "/admin", "pw")
	body := rec.Body.String()
	if !strings.Contains(body, "1 visible · 0 hidden") {
		t.Error("dashboard missing counts header")
	}
	if !strings.Contains(body, "hello.jpg") || !strings.Contains(body, "by Bob") {
		t.Error("dashboard missing the seeded photo row")
	}
	if !strings.Contains(body, `action="/admin/shutdown"`) {
		t.Error("dashboard missing shutdown button")
	}
}

func TestAdminHideUnhideFlow(t *testing.T) {
	s := adminTestServer(t, "pw")
	h := seedJPEG(t, s, "to-hide.jpg", "")

	rec := doAdmin(t, s, http.MethodPost, "/admin/photos/"+h+"/hide", "pw")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("hide = %d, want 303", rec.Code)
	}
	if _, hidden, _ := s.st.PhotoCounts(); hidden != 1 {
		t.Errorf("after hide: hidden count = %d, want 1", hidden)
	}
	// Hidden photo must not appear in the public feed.
	if rec := get(t, s, "/api/photos"); !strings.Contains(rec.Body.String(), `"photos":[]`) {
		t.Errorf("gallery feed still shows hidden photo: %s", rec.Body.String())
	}

	rec = doAdmin(t, s, http.MethodPost, "/admin/photos/"+h+"/unhide", "pw")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unhide = %d, want 303", rec.Code)
	}
	if v, hidden, _ := s.st.PhotoCounts(); v != 1 || hidden != 0 {
		t.Errorf("after unhide: (vis=%d hidden=%d), want (1,0)", v, hidden)
	}
}

func TestAdminDeleteRemovesRowAndBlobs(t *testing.T) {
	s := adminTestServer(t, "pw")
	h := seedJPEG(t, s, "to-delete.jpg", "")

	if !s.blobs.Exists(blobstore.Original, h, ".jpg") {
		t.Fatal("precondition: original blob should exist")
	}
	rec := doAdmin(t, s, http.MethodPost, "/admin/photos/"+h+"/delete", "pw")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", rec.Code)
	}
	if _, ok, _ := s.st.PhotoMeta(h); ok {
		t.Error("photo row should be gone")
	}
	if s.blobs.Exists(blobstore.Original, h, ".jpg") {
		t.Error("original blob should be removed")
	}
	// /photo & /original now 404.
	if rec := get(t, s, "/photo/"+h); rec.Code != http.StatusNotFound {
		t.Errorf("/photo after delete = %d, want 404", rec.Code)
	}
	if rec := get(t, s, "/original/"+h); rec.Code != http.StatusNotFound {
		t.Errorf("/original after delete = %d, want 404", rec.Code)
	}
	// Delete again → 404.
	if rec := doAdmin(t, s, http.MethodPost, "/admin/photos/"+h+"/delete", "pw"); rec.Code != http.StatusNotFound {
		t.Errorf("re-delete = %d, want 404", rec.Code)
	}
}

func TestAdminBadHashRejected(t *testing.T) {
	s := adminTestServer(t, "pw")
	rec := doAdmin(t, s, http.MethodPost, "/admin/photos/not-a-hash/hide", "pw")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad hash = %d, want 400", rec.Code)
	}
}

// Shutdown's auth + response; the real SIGTERM would kill the test
// runner, so we stub the hook and only assert that it fires.
func TestAdminShutdownRespondsThenSignals(t *testing.T) {
	s := adminTestServer(t, "pw")
	if rec := doAdmin(t, s, http.MethodPost, "/admin/shutdown", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("shutdown without creds = %d, want 401", rec.Code)
	}

	done := make(chan struct{}, 1)
	prev := shutdownSignal
	shutdownSignal = func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}
	t.Cleanup(func() { shutdownSignal = prev })

	rec := doAdmin(t, s, http.MethodPost, "/admin/shutdown", "pw")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Shutting down") {
		t.Fatalf("shutdown response: code=%d body=%q", rec.Code, rec.Body.String())
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdownSignal was not invoked within 2s")
	}
}
