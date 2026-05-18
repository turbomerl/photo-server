// Package server is the HTTP front of photo-server: routing, lifecycle,
// and the health endpoint. Feature handlers (upload, gallery, admin,
// slideshow) attach to the mux built here in later work.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/convert"
	"github.com/turbomerl/photo-server/internal/session"
	"github.com/turbomerl/photo-server/internal/store"
)

// Deps are the runtime dependencies the handlers share. Grouping them
// keeps New stable as upload/gallery/admin/slideshow are added.
type Deps struct {
	Log      *slog.Logger
	Version  string
	Store    *store.Store
	Blobs    *blobstore.Store
	Convert  *convert.Pool      // async pool; nil if libvips tooling absent
	Conv     *convert.Converter // sync renderer for lazy-regenerate-on-miss
	Sessions *session.Manager
	MaxBody  int64
}

// Server wraps the HTTP server and its dependencies.
type Server struct {
	log      *slog.Logger
	version  string
	st       *store.Store
	blobs    *blobstore.Store
	conv     *convert.Pool
	convr    *convert.Converter
	sessions *session.Manager
	maxBody  int64
	httpSrv  *http.Server
}

// New builds a Server listening on addr with the given dependencies.
func New(addr string, d Deps) *Server {
	s := &Server{
		log:      d.Log,
		version:  d.Version,
		st:       d.Store,
		blobs:    d.Blobs,
		conv:     d.Convert,
		convr:    d.Conv,
		sessions: d.Sessions,
		maxBody:  d.MaxBody,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /upload", s.handleUpload)
	mux.HandleFunc("GET /thumb/{hash}", s.handleThumb)
	mux.HandleFunc("GET /session", s.handleSession)
	mux.HandleFunc("POST /session", s.handleSession)
	mux.HandleFunc("GET /static/session.js", s.handleSessionJS)

	// UI shell (kgu.15). "GET /{$}" matches only the exact root so
	// unknown paths still 404 (not the Polaroid catch-all).
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /upload", s.handleUploadPage)
	mux.HandleFunc("GET /gallery", s.handleGalleryPage)
	mux.HandleFunc("GET /static/app.css", s.handleAppCSS)
	mux.HandleFunc("GET /static/polaroid.js", s.handlePolaroidJS)
	mux.HandleFunc("GET /static/upload.js", s.handleUploadJS)
	mux.HandleFunc("GET /static/gallery.js", s.handleGalleryJS)
	mux.HandleFunc("GET /api/uploads/mine", s.handleMyUploads)
	mux.HandleFunc("GET /api/photos", s.handlePhotos)

	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: s.logRequests(mux),
		// Bound slow-loris header reads; appliance is on a trusted LAN
		// but a stuck phone shouldn't tie up a connection forever.
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Run starts serving and blocks until ctx is cancelled, then drains
// in-flight requests within shutdownTimeout. It returns nil on a clean
// signalled shutdown.
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.httpSrv.Addr, "version", s.version)
		err := s.httpSrv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		s.log.Info("shutdown signalled, draining", "timeout", shutdownTimeout)
		sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.httpSrv.Shutdown(sctx); err != nil {
			return err
		}
		s.log.Info("http server stopped cleanly")
		return nil
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": s.version,
	})
}

// logRequests emits a minimal access line at debug level. Request
// logging stays off by default to keep the appliance quiet on disk
// during an all-day event.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Debug("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"dur", time.Since(start),
		)
	})
}
