package config

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// t.Setenv isolates env per-test and restores it afterwards.
	t.Setenv("STATE_DIRECTORY", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if !filepath.IsAbs(c.DataDir) {
		t.Errorf("DataDir = %q, want absolute", c.DataDir)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", c.LogLevel)
	}
	if c.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", c.ShutdownTimeout)
	}
	if c.MaxUploadBytes != 64<<20 {
		t.Errorf("MaxUploadBytes = %d, want %d", c.MaxUploadBytes, 64<<20)
	}
	if c.ConvertWorkers < 1 || c.ConvertWorkers > 4 {
		t.Errorf("ConvertWorkers = %d, want 1..4", c.ConvertWorkers)
	}
	if c.GalleryMaxPx != 2560 {
		t.Errorf("GalleryMaxPx = %d, want 2560", c.GalleryMaxPx)
	}
	if c.JPEGQuality != 85 {
		t.Errorf("JPEGQuality = %d, want 85", c.JPEGQuality)
	}
	if c.ThumbPx != 400 || c.ThumbQuality != 80 {
		t.Errorf("thumb defaults = (%d,%d), want (400,80)", c.ThumbPx, c.ThumbQuality)
	}
	if c.SessionMaxAge != 30*24*time.Hour {
		t.Errorf("SessionMaxAge = %v, want 720h", c.SessionMaxAge)
	}
	if c.AdminPassword != "" {
		t.Errorf("AdminPassword default = %q, want empty (admin fail-closed)", c.AdminPassword)
	}
	if c.BaseURL != "http://photos.wedding/" {
		t.Errorf("BaseURL default = %q", c.BaseURL)
	}
	if c.IsHTTPS {
		t.Error("IsHTTPS = true for default http BaseURL, want false")
	}
	if c.SSID != "" || c.WiFiPSK != "" {
		t.Errorf("QR defaults should be empty: ssid=%q psk=%q", c.SSID, c.WiFiPSK)
	}
	if c.VipsThumbnailBin != "vipsthumbnail" {
		t.Errorf("VipsThumbnailBin = %q, want vipsthumbnail", c.VipsThumbnailBin)
	}
}

func TestLoadIsHTTPSFromBaseURL(t *testing.T) {
	t.Setenv("PHOTO_SERVER_BASE_URL", "https://photos.example.com/")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.IsHTTPS {
		t.Errorf("IsHTTPS = false for https BaseURL %q, want true", c.BaseURL)
	}
}

func TestLoadStateDirectoryWins(t *testing.T) {
	t.Setenv("STATE_DIRECTORY", "/var/lib/photo-server:/ignored")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DataDir != "/var/lib/photo-server" {
		t.Errorf("DataDir = %q, want /var/lib/photo-server", c.DataDir)
	}
}

func TestLoadExplicitOverrides(t *testing.T) {
	t.Setenv("PHOTO_SERVER_ADDR", ":9000")
	t.Setenv("PHOTO_SERVER_DATA_DIR", "/srv/photos")
	t.Setenv("PHOTO_SERVER_LOG_LEVEL", "debug")
	t.Setenv("PHOTO_SERVER_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("PHOTO_SERVER_MAX_UPLOAD_BYTES", "1048576")
	t.Setenv("PHOTO_SERVER_CONVERT_WORKERS", "2")
	t.Setenv("PHOTO_SERVER_GALLERY_MAX_PX", "1600")
	t.Setenv("PHOTO_SERVER_JPEG_QUALITY", "70")
	t.Setenv("PHOTO_SERVER_THUMB_PX", "256")
	t.Setenv("PHOTO_SERVER_THUMB_QUALITY", "60")
	t.Setenv("PHOTO_SERVER_SESSION_MAX_AGE", "48h")
	t.Setenv("PHOTO_SERVER_ADMIN_PASSWORD", "swordfish")
	t.Setenv("PHOTO_SERVER_BASE_URL", "http://example.lan/")
	t.Setenv("PHOTO_SERVER_SSID", "WeddingPhotos")
	t.Setenv("PHOTO_SERVER_WIFI_PSK", "ourwedding2026")
	t.Setenv("PHOTO_SERVER_VIPSTHUMBNAIL_BIN", "/usr/bin/vipsthumbnail")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MaxUploadBytes != 1048576 {
		t.Errorf("MaxUploadBytes = %d, want 1048576", c.MaxUploadBytes)
	}
	if c.ConvertWorkers != 2 || c.GalleryMaxPx != 1600 || c.JPEGQuality != 70 {
		t.Errorf("convert knobs = (%d,%d,%d), want (2,1600,70)",
			c.ConvertWorkers, c.GalleryMaxPx, c.JPEGQuality)
	}
	if c.ThumbPx != 256 || c.ThumbQuality != 60 {
		t.Errorf("thumb knobs = (%d,%d), want (256,60)", c.ThumbPx, c.ThumbQuality)
	}
	if c.SessionMaxAge != 48*time.Hour {
		t.Errorf("SessionMaxAge = %v, want 48h", c.SessionMaxAge)
	}
	if c.AdminPassword != "swordfish" {
		t.Errorf("AdminPassword = %q", c.AdminPassword)
	}
	if c.BaseURL != "http://example.lan/" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.SSID != "WeddingPhotos" || c.WiFiPSK != "ourwedding2026" {
		t.Errorf("SSID/PSK = %q/%q", c.SSID, c.WiFiPSK)
	}
	if c.VipsThumbnailBin != "/usr/bin/vipsthumbnail" {
		t.Errorf("VipsThumbnailBin = %q", c.VipsThumbnailBin)
	}
	if c.Addr != ":9000" {
		t.Errorf("Addr = %q, want :9000", c.Addr)
	}
	if c.DataDir != "/srv/photos" {
		t.Errorf("DataDir = %q, want /srv/photos", c.DataDir)
	}
	if c.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", c.LogLevel)
	}
	if c.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 30s", c.ShutdownTimeout)
	}
}

func TestLoadBadValuesError(t *testing.T) {
	t.Setenv("PHOTO_SERVER_LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for bad LOG_LEVEL, got nil")
	}

	t.Setenv("PHOTO_SERVER_LOG_LEVEL", "info")
	t.Setenv("PHOTO_SERVER_SHUTDOWN_TIMEOUT", "soon")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for bad SHUTDOWN_TIMEOUT, got nil")
	}

	t.Setenv("PHOTO_SERVER_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("PHOTO_SERVER_MAX_UPLOAD_BYTES", "-5")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for non-positive MAX_UPLOAD_BYTES, got nil")
	}

	t.Setenv("PHOTO_SERVER_MAX_UPLOAD_BYTES", "1048576")
	t.Setenv("PHOTO_SERVER_CONVERT_WORKERS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for non-positive CONVERT_WORKERS, got nil")
	}

	t.Setenv("PHOTO_SERVER_CONVERT_WORKERS", "2")
	t.Setenv("PHOTO_SERVER_JPEG_QUALITY", "150")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for out-of-range JPEG_QUALITY, got nil")
	}
}
