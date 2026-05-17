// Command photo-server is the single binary for the offline wedding
// photo appliance: it serves the upload/gallery web UI and stores
// photos locally on the device.
//
// This is the service skeleton (kgu.8) — storage, upload, gallery,
// admin and slideshow handlers land in later server-core work.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/config"
	"github.com/turbomerl/photo-server/internal/convert"
	"github.com/turbomerl/photo-server/internal/server"
	"github.com/turbomerl/photo-server/internal/store"
)

// version is overridden at build time via the Makefile:
//
//	go build -ldflags "-X main.version=$(git describe --tags --always)"
var version = "dev"

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet; stderr is journald-visible.
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, closeLog, err := newLogger(cfg)
	if err != nil {
		return err
	}
	defer closeLog()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}

	logger.Info("starting photo-server",
		"version", version,
		"addr", cfg.Addr,
		"data_dir", cfg.DataDir,
		"log_level", cfg.LogLevel.String(),
	)

	dbPath := filepath.Join(cfg.DataDir, "photo-server.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	schemaVer, err := st.SchemaVersion()
	if err != nil {
		return err
	}
	logger.Info("database ready", "path", dbPath, "schema_version", schemaVer)

	blobs, err := blobstore.New(cfg.DataDir)
	if err != nil {
		return err
	}
	logger.Info("blob store ready", "root", blobs.Root())

	// Cancel the root context on SIGINT/SIGTERM so the HTTP server can
	// drain in-flight uploads before exit (PRD F15 clean shutdown, N6
	// durability). systemd sends SIGTERM on stop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// HEIC→JPEG conversion pool. If libvips tooling is missing the
	// server still runs (degraded: HEIC just won't get gallery JPEGs).
	var pool *convert.Pool
	if conv, err := convert.NewConverter(cfg.VipsThumbnailBin, blobs, cfg.GalleryMaxPx, cfg.JPEGQuality, logger); err != nil {
		logger.Warn("HEIC→JPEG conversion disabled", "err", err)
	} else {
		pool = convert.NewPool(conv, cfg.ConvertWorkers, 256, logger)
		pool.Start(ctx)
		defer pool.Stop()
		logger.Info("conversion pool ready", "workers", cfg.ConvertWorkers)
		backfillGalleryJPEGs(st, blobs, pool, logger)
	}

	srv := server.New(cfg.Addr, server.Deps{
		Log:     logger,
		Version: version,
		Store:   st,
		Blobs:   blobs,
		Convert: pool,
		MaxBody: cfg.MaxUploadBytes,
	})
	return srv.Run(ctx, cfg.ShutdownTimeout)
}

// backfillGalleryJPEGs re-enqueues any HEIC/HEIF photo missing its
// gallery JPEG (crash recovery / dropped queue items) so the appliance
// self-heals on restart (PRD N8).
func backfillGalleryJPEGs(st *store.Store, blobs *blobstore.Store, pool *convert.Pool, logger *slog.Logger) {
	refs, err := st.HEICPhotos()
	if err != nil {
		logger.Warn("gallery backfill query failed", "err", err)
		return
	}
	queued := 0
	for _, r := range refs {
		ext := ".heic"
		if r.MIME == "image/heif" {
			ext = ".heif"
		}
		if blobs.Exists(blobstore.Gallery, r.Hash, "") {
			continue
		}
		pool.Enqueue(r.Hash, ext)
		queued++
	}
	if queued > 0 {
		logger.Info("gallery backfill queued", "count", queued)
	}
}

// newLogger writes structured logs to stderr (captured by journald
// under systemd) and, when PHOTO_SERVER_LOG_FILE is set, additionally
// to that file. The returned func closes the file, if any.
func newLogger(cfg config.Config) (*slog.Logger, func(), error) {
	w := io.Writer(os.Stderr)
	closer := func() {}

	if cfg.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o750); err != nil {
			return nil, nil, err
		}
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
		if err != nil {
			return nil, nil, err
		}
		w = io.MultiWriter(os.Stderr, f)
		closer = func() { _ = f.Close() }
	}

	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: cfg.LogLevel})
	return slog.New(h), closer, nil
}
