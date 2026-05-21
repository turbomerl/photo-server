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

func probe(t *testing.T, s *Server, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	return rec
}

func TestCaptiveProbesValidateCleanly(t *testing.T) {
	s := captiveTestServer(t)

	// Android: …/generate_204 → 204 No Content (network validated).
	if rec := probe(t, s, "connectivitycheck.gstatic.com", "/generate_204"); rec.Code != http.StatusNoContent {
		t.Errorf("android probe = %d, want 204", rec.Code)
	}
	if rec := probe(t, s, "clients3.google.com", "/gen_204"); rec.Code != http.StatusNoContent {
		t.Errorf("android gen_204 = %d, want 204", rec.Code)
	}
	// iOS: Apple "Success" page.
	rec := probe(t, s, "captive.apple.com", "/hotspot-detect.html")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Success") {
		t.Errorf("apple probe = %d body=%q, want 200 Success", rec.Code, rec.Body.String())
	}
	// Windows.
	if rec := probe(t, s, "www.msftncsi.com", "/ncsi.txt"); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "Microsoft NCSI") {
		t.Errorf("ncsi probe = %d body=%q", rec.Code, rec.Body.String())
	}
	// None of these should be a redirect (no captive nag).
	if rec := probe(t, s, "captive.apple.com", "/hotspot-detect.html"); rec.Code == http.StatusFound {
		t.Error("probe must not 302 (would re-trigger the captive sheet)")
	}
}

func TestCaptiveSoftLandsOtherForeignHosts(t *testing.T) {
	s := captiveTestServer(t)
	rec := probe(t, s, "some-random.example", "/")
	if rec.Code != http.StatusFound {
		t.Fatalf("foreign host = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "http://photos.wedding/welcome" {
		t.Errorf("Location = %q, want the /welcome landing", loc)
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
