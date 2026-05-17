// Package config loads photo-server configuration from the environment.
//
// This is an offline-first appliance: configuration is environment-only
// (no config file to hand-edit on the device, no network lookups). When
// run under systemd, the unit's StateDirectory= is honoured via the
// $STATE_DIRECTORY it exports.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	// Addr is the HTTP listen address, e.g. ":8080".
	Addr string
	// DataDir holds the SQLite database and photo storage. It is created
	// on startup if missing.
	DataDir string
	// LogLevel is the minimum slog level.
	LogLevel slog.Level
	// LogFile, if non-empty, additionally writes logs to this file.
	// stderr is always written (captured by journald under systemd).
	LogFile string
	// ShutdownTimeout bounds graceful drain of in-flight requests.
	ShutdownTimeout time.Duration
	// MaxUploadBytes caps a single uploaded file. Not a guest-visible
	// quota (PRD encourages volume) — just a sanity bound so one
	// pathological file can't exhaust the disk.
	MaxUploadBytes int64
}

const envPrefix = "PHOTO_SERVER_"

// Load resolves configuration from PHOTO_SERVER_* environment variables,
// applying appliance-sane defaults. It errors only for values that are
// present but unparseable — absence always falls back to a default.
func Load() (Config, error) {
	c := Config{
		Addr:            getenv("ADDR", ":8080"),
		DataDir:         defaultDataDir(),
		LogLevel:        slog.LevelInfo,
		LogFile:         getenv("LOG_FILE", ""),
		ShutdownTimeout: 15 * time.Second,
		MaxUploadBytes:  64 << 20, // 64 MiB
	}

	if v := getenv("DATA_DIR", ""); v != "" {
		c.DataDir = v
	}

	if v := getenv("LOG_LEVEL", ""); v != "" {
		var lv slog.Level
		if err := lv.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("%sLOG_LEVEL %q: %w", envPrefix, v, err)
		}
		c.LogLevel = lv
	}

	if v := getenv("SHUTDOWN_TIMEOUT", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("%sSHUTDOWN_TIMEOUT %q: %w", envPrefix, v, err)
		}
		c.ShutdownTimeout = d
	}

	if v := getenv("MAX_UPLOAD_BYTES", ""); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("%sMAX_UPLOAD_BYTES %q: must be a positive integer", envPrefix, v)
		}
		c.MaxUploadBytes = n
	}

	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir %q: %w", c.DataDir, err)
	}
	c.DataDir = abs

	return c, nil
}

// defaultDataDir prefers systemd's StateDirectory (exported as
// $STATE_DIRECTORY by StateDirectory=photo-server in the unit), falling
// back to ./data for local development.
func defaultDataDir() string {
	if sd := os.Getenv("STATE_DIRECTORY"); sd != "" {
		// StateDirectory may be a colon-separated list; take the first.
		return strings.Split(sd, ":")[0]
	}
	return "data"
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(envPrefix + key); ok {
		return v
	}
	return def
}
