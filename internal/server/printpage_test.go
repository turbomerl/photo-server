package server

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/session"
	"github.com/turbomerl/photo-server/internal/store"
)

func printServer(t *testing.T, base, access string) *Server {
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
		Sessions: session.NewManager(st, time.Hour, false),
		MaxBody:  64 << 20, AdminPassword: "pw",
		BaseURL: base, AccessPassword: access,
	})
}

func TestPrintPageRequiresAdmin(t *testing.T) {
	s := printServer(t, "https://photos.example.com/", "letmein")
	if rec := doAdmin(t, s, http.MethodGet, "/admin/print", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no creds = %d, want 401", rec.Code)
	}
}

func TestPrintPageNotConfigured(t *testing.T) {
	s := printServer(t, "", "")
	rec := doAdmin(t, s, http.MethodGet, "/admin/print", "pw")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PHOTO_SERVER_BASE_URL") {
		t.Error("missing 'configure BASE_URL' guidance")
	}
}

func TestPrintPageRendersEntryQRAndPassword(t *testing.T) {
	s := printServer(t, "https://photos.example.com/", "letmein")
	rec := doAdmin(t, s, http.MethodGet, "/admin/print", "pw")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	b := rec.Body.String()
	// One entry QR per card (4 cards).
	if c := strings.Count(b, `src="data:image/png;base64,`); c != 4 {
		t.Errorf("expected 4 QR data URLs (one per card), got %d", c)
	}
	for _, want := range []string{
		"photos.example.com", // host to open in a browser
		"letmein",            // the event password printed on the card
		"window.print()",     // print button
		"@page",              // print CSS
	} {
		if !strings.Contains(b, want) {
			t.Errorf("print body missing %q", want)
		}
	}
}

func TestEntryURLBakesInKey(t *testing.T) {
	s := printServer(t, "https://photos.example.com/", "letmein")
	got := s.entryURL()
	if got != "https://photos.example.com/?k=letmein" {
		t.Errorf("entryURL = %q", got)
	}
	// No password -> bare BASE_URL.
	s2 := printServer(t, "https://photos.example.com/", "")
	if got := s2.entryURL(); got != "https://photos.example.com/" {
		t.Errorf("entryURL (no key) = %q", got)
	}
}
