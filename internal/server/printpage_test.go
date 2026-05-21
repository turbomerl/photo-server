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

func printServer(t *testing.T, ssid, psk, base string) *Server {
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
		Sessions: session.NewManager(st, time.Hour),
		MaxBody:  64 << 20, AdminPassword: "pw",
		BaseURL: base, SSID: ssid, WiFiPSK: psk,
	})
}

func TestPrintPageRequiresAdmin(t *testing.T) {
	s := printServer(t, "photo-server", "photos2026", "http://photos.wedding/")
	if rec := doAdmin(t, s, http.MethodGet, "/admin/print", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no creds = %d, want 401", rec.Code)
	}
}

func TestPrintPageNotConfigured(t *testing.T) {
	s := printServer(t, "", "", "")
	rec := doAdmin(t, s, http.MethodGet, "/admin/print", "pw")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PHOTO_SERVER_SSID") {
		t.Error("missing 'configure these env vars' guidance")
	}
}

func TestPrintPageRendersQRsAndLabels(t *testing.T) {
	s := printServer(t, "photo-server", "photos2026", "http://photos.wedding/")
	rec := doAdmin(t, s, http.MethodGet, "/admin/print", "pw")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	b := rec.Body.String()
	// Two distinct QR PNGs embedded as data URLs.
	if c := strings.Count(b, "src=\"data:image/png;base64,"); c < 2 {
		t.Errorf("expected at least 2 QR data URLs, got %d", c)
	}
	for _, want := range []string{
		"photo-server",           // SSID label
		"photos2026",             // PSK label
		"http://photos.wedding/", // URL label
		"window.print()",         // print button
		"@page",                  // print CSS
	} {
		if !strings.Contains(b, want) {
			t.Errorf("print body missing %q", want)
		}
	}
}

func TestWIFIURIEscaping(t *testing.T) {
	got := wifiEscape(`tricky; weird: "ssid", with\back`)
	want := `tricky\; weird\: \"ssid\"\, with\\back`
	if got != want {
		t.Errorf("wifiEscape =\n  %q\n  want\n  %q", got, want)
	}
}
