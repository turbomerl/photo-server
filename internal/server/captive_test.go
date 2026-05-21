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

// captiveTestServer enables AllowedHosts so the captive middleware
// activates.
func captiveTestServer(t *testing.T) *Server {
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
		Sessions:     session.NewManager(st, time.Hour),
		MaxBody:      64 << 20,
		BaseURL:      "http://photos.wedding/",
		AllowedHosts: []string{"photos.wedding", "127.0.0.1", "localhost", "192.168.50.1"},
	})
}

func TestCaptiveRedirectsForeignHost(t *testing.T) {
	s := captiveTestServer(t)
	for _, host := range []string{
		"captive.apple.com",
		"connectivitycheck.gstatic.com",
		"www.msftncsi.com",
		"some-random.example",
	} {
		req := httptest.NewRequest(http.MethodGet, "/anything", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.httpSrv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Errorf("host %q = %d, want 302", host, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "http://photos.wedding/welcome" {
			t.Errorf("host %q Location = %q, want the /welcome landing", host, loc)
		}
	}
}

func TestCaptivePassesThroughAllowedHosts(t *testing.T) {
	s := captiveTestServer(t)
	for _, host := range []string{
		"photos.wedding",
		"photos.wedding:8080",
		"127.0.0.1",
		"192.168.50.1:80",
		"LOCALHOST", // case-insensitive
	} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.httpSrv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("host %q = %d, want 200 (allowed)", host, rec.Code)
		}
	}
}

func TestWelcomeLandingServed(t *testing.T) {
	s := captiveTestServer(t) // BaseURL=http://photos.wedding/, host allowed
	req := httptest.NewRequest(http.MethodGet, "/welcome", nil)
	req.Host = "photos.wedding" // allowed → passes the captive middleware
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/welcome = %d, want 200", rec.Code)
	}
	b := rec.Body.String()
	if !strings.Contains(b, "You're connected") {
		t.Error("welcome page missing the 'connected' message")
	}
	if !strings.Contains(b, "photos.wedding") {
		t.Error("welcome page should name the host to open in a browser")
	}
	// It's a standalone landing — no bottom tab nav.
	if strings.Contains(b, `class="tabbar"`) {
		t.Error("welcome page should not render the guest bottom nav")
	}
}

func TestCaptiveDisabledWithNoAllowedHosts(t *testing.T) {
	// newTestServer doesn't set AllowedHosts → captive is a no-op.
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Host = "captive.apple.com"
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("captive should be off in dev/tests: got %d", rec.Code)
	}
}
