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

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MaxUploadBytes != 1048576 {
		t.Errorf("MaxUploadBytes = %d, want 1048576", c.MaxUploadBytes)
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
}
