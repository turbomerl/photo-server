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
	"runtime"
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
	// ConvertWorkers bounds concurrent HEIC→JPEG conversions so a
	// speech-time burst can't thrash the Dell (PRD R5).
	ConvertWorkers int
	// GalleryMaxPx caps the longest edge of the browser-viewable
	// gallery JPEG (the original is preserved untouched).
	GalleryMaxPx int
	// JPEGQuality is the gallery JPEG quality (1–100).
	JPEGQuality int
	// ThumbPx is the longest edge of the gallery-grid webp thumbnail.
	ThumbPx int
	// ThumbQuality is the thumbnail webp quality (1–100).
	ThumbQuality int
	// VipsThumbnailBin is the vipsthumbnail executable; resolved via
	// PATH at startup. Overridable for the locked-down systemd unit.
	VipsThumbnailBin string
	// SessionMaxAge is the guest-session cookie lifetime. PRD: "no
	// expiry within the event window" — default well beyond a wedding.
	SessionMaxAge time.Duration
	// AdminPassword gates /admin (HTTP Basic). Empty disables the
	// admin surface entirely (fail-closed if not configured — PRD F13).
	AdminPassword string
}

const envPrefix = "PHOTO_SERVER_"

// Load resolves configuration from PHOTO_SERVER_* environment variables,
// applying appliance-sane defaults. It errors only for values that are
// present but unparseable — absence always falls back to a default.
func Load() (Config, error) {
	c := Config{
		Addr:             getenv("ADDR", ":8080"),
		DataDir:          defaultDataDir(),
		LogLevel:         slog.LevelInfo,
		LogFile:          getenv("LOG_FILE", ""),
		ShutdownTimeout:  15 * time.Second,
		MaxUploadBytes:   64 << 20, // 64 MiB
		ConvertWorkers:   defaultWorkers(),
		GalleryMaxPx:     2560,
		JPEGQuality:      85,
		ThumbPx:          400,
		ThumbQuality:     80,
		VipsThumbnailBin: getenv("VIPSTHUMBNAIL_BIN", "vipsthumbnail"),
		SessionMaxAge:    30 * 24 * time.Hour, // ~30 days
		AdminPassword:    getenv("ADMIN_PASSWORD", ""),
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

	if err := posIntEnv("CONVERT_WORKERS", &c.ConvertWorkers); err != nil {
		return Config{}, err
	}
	if err := posIntEnv("GALLERY_MAX_PX", &c.GalleryMaxPx); err != nil {
		return Config{}, err
	}
	if err := posIntEnv("JPEG_QUALITY", &c.JPEGQuality); err != nil {
		return Config{}, err
	}
	if err := posIntEnv("THUMB_PX", &c.ThumbPx); err != nil {
		return Config{}, err
	}
	if err := posIntEnv("THUMB_QUALITY", &c.ThumbQuality); err != nil {
		return Config{}, err
	}
	if c.JPEGQuality > 100 || c.ThumbQuality > 100 {
		return Config{}, fmt.Errorf("%sJPEG_QUALITY/THUMB_QUALITY must be 1–100", envPrefix)
	}

	if v := getenv("SESSION_MAX_AGE", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("%sSESSION_MAX_AGE %q: must be a positive duration", envPrefix, v)
		}
		c.SessionMaxAge = d
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

// posIntEnv overrides *dst from PHOTO_SERVER_<key> if set, requiring a
// positive integer. Absent leaves the default in place.
func posIntEnv(key string, dst *int) error {
	v := getenv(key, "")
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fmt.Errorf("%s%s %q: must be a positive integer", envPrefix, key, v)
	}
	*dst = n
	return nil
}

// defaultWorkers caps conversion concurrency at the smaller of the CPU
// count and 4 — enough to keep the queue draining during a burst
// without the fanless Dell thermal-throttling (PRD R5).
func defaultWorkers() int {
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}
