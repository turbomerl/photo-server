package server

import (
	"context"
	"net/http"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/convert"
)

// handleThumb serves the ~400px webp grid thumbnail for a photo
// (kgu.13). Thumbs are content-addressed by the original's hash, so
// they are immutable and aggressively cacheable. On a miss (crash,
// dropped queue item, not-yet-processed) it lazily regenerates the
// thumbnail synchronously, then serves it — misses are rare because
// the upload pool generates thumbs proactively.
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isHexSHA256(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}

	if !s.blobs.Exists(blobstore.Thumb, hash, "") {
		if !s.regenerateThumb(r.Context(), hash) {
			http.NotFound(w, r)
			return
		}
	}

	f, err := s.blobs.Open(blobstore.Thumb, hash, "")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "image/webp")
	// Content-addressed ⇒ never changes ⇒ cache forever.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+hash+`"`)
	// http.ServeContent handles If-None-Match (304) and Range using the
	// ETag above; zero modtime suppresses Last-Modified.
	http.ServeContent(w, r, "thumb.webp", time.Time{}, f)
}

// regenerateThumb produces a missing thumbnail on demand. Returns false
// if the photo is unknown, conversion is unavailable, or it fails.
func (s *Server) regenerateThumb(ctx context.Context, hash string) bool {
	if s.convr == nil {
		return false
	}
	ref, ok, err := s.st.PhotoByHash(hash)
	if err != nil || !ok {
		return false
	}
	ext := convert.ExtForMIME(ref.MIME)
	if ext == "" {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.convr.Thumbnail(cctx, hash, ext); err != nil {
		s.log.Error("lazy thumbnail regenerate failed", "hash", hash, "err", err)
		return false
	}
	return true
}

// isHexSHA256 reports whether s is a 64-char lowercase hex string.
func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
