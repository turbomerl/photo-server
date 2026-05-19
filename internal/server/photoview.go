package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/convert"
)

// handlePhotoView serves the browser-viewable full-size image for the
// lightbox (kgu.18). For HEIC/HEIF that is the gallery JPEG produced by
// kgu.12 (lazily regenerated on a miss); JPEG/PNG originals are already
// viewable, so the original is served directly. Content-addressed →
// immutable.
func (s *Server) handlePhotoView(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isHexSHA256(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	meta, ok, err := s.st.PhotoMeta(hash)
	if err != nil {
		s.log.Error("photo view meta", "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r) // unknown or hidden
		return
	}

	var (
		f          *os.File
		oerr       error
		cType      string
		serverName string
	)
	if meta.MIME == "image/heic" || meta.MIME == "image/heif" {
		if !s.blobs.Exists(blobstore.Gallery, hash, "") {
			if !s.regenerateGallery(r.Context(), hash, meta.MIME) {
				http.NotFound(w, r)
				return
			}
		}
		f, oerr = s.blobs.Open(blobstore.Gallery, hash, "")
		cType, serverName = "image/jpeg", "photo.jpg"
	} else {
		f, oerr = s.blobs.Open(blobstore.Original, hash, convert.ExtForMIME(meta.MIME))
		cType, serverName = meta.MIME, "photo"
	}
	if oerr != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", cType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"v-`+hash+`"`)
	http.ServeContent(w, r, serverName, time.Time{}, f)
}

// handleOriginalDownload serves the untouched original as an
// attachment (kgu.18 "Download original"). The original is always
// preserved (kgu.12).
func (s *Server) handleOriginalDownload(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isHexSHA256(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	meta, ok, err := s.st.PhotoMeta(hash)
	if err != nil {
		s.log.Error("download meta", "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	ext := convert.ExtForMIME(meta.MIME)
	f, oerr := s.blobs.Open(blobstore.Original, hash, ext)
	if oerr != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", meta.MIME)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"o-`+hash+`"`)
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+downloadName(meta.OriginalFilename, hash, ext)+`"`)
	http.ServeContent(w, r, "original"+ext, time.Time{}, f)
}

// downloadName picks a safe attachment filename: the uploader's
// original name when present and sane, else <hash><ext>.
func downloadName(orig, hash, ext string) string {
	orig = strings.TrimSpace(orig)
	// Strip anything that could break the Content-Disposition header
	// or escape the filename (it was already filepath.Base'd at
	// upload, but be defensive).
	if orig != "" && !strings.ContainsAny(orig, "\"\\/\r\n") && len(orig) <= 128 {
		return orig
	}
	return hash + ext
}

// regenerateGallery produces a missing gallery JPEG on demand (same
// self-heal idea as the thumbnail route). false if unavailable.
func (s *Server) regenerateGallery(ctx context.Context, hash, mime string) bool {
	if s.convr == nil {
		return false
	}
	ext := convert.ExtForMIME(mime)
	if ext == "" {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.convr.GalleryJPEG(cctx, hash, ext); err != nil {
		s.log.Error("lazy gallery regenerate", "hash", hash, "err", err)
		return false
	}
	return true
}
