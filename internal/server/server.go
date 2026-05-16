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
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	log     *slog.Logger
	version string
	httpSrv *http.Server
}

// New builds a Server listening on addr. version is reported by the
// health endpoint and in the startup log line.
func New(addr, version string, log *slog.Logger) *Server {
	s := &Server{log: log, version: version}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)

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
