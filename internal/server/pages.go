package server

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"net/url"

	"github.com/turbomerl/photo-server/internal/store"
)

//go:embed assets/templates/*.html
var templatesFS embed.FS

//go:embed assets/app.css
var appCSS []byte

//go:embed assets/polaroid.js
var polaroidJS []byte

//go:embed assets/upload.js
var uploadJS []byte

//go:embed assets/gallery.js
var galleryJS []byte

//go:embed assets/viewer.js
var viewerJS []byte

// One template set per page: base.html provides the shell + bottom
// nav; the page file overrides the title/main/scripts blocks.
func mustPage(page string) *template.Template {
	return template.Must(template.ParseFS(templatesFS,
		"assets/templates/base.html", "assets/templates/"+page))
}

var (
	tplPolaroid = mustPage("polaroid.html")
	tplUpload   = mustPage("upload.html")
	tplGallery  = mustPage("gallery.html")
	tplPhoto    = mustPage("photo.html")
)

type pageData struct {
	Active  string
	Version string
	// Name pre-fills the shared display-name field server-side so it
	// is correct even before session.js runs.
	Name string
	// Recent is the session's own uploads, rendered server-side on the
	// Upload page so it works with JS disabled (kgu.16).
	Recent []store.PhotoListItem
	// Photos is the gallery's first page, rendered server-side so the
	// Gallery works with JS disabled (kgu.17); NextBefore is the
	// keyset cursor for infinite scroll (0 = no more).
	Photos     []store.PhotoListItem
	NextBefore int64
	// ViewHash/ViewName drive the server-rendered single-photo page
	// (kgu.18, the no-JS fallback for the lightbox).
	ViewHash string
	ViewName string
}

// appHost is the bare host (no scheme/path) of the configured BaseURL,
// e.g. "photos.wedding" — printed on the QR card. Empty if unset.
func (s *Server) appHost() string {
	if s.baseURL == "" {
		return ""
	}
	if u, err := url.Parse(s.baseURL); err == nil {
		return u.Host
	}
	return ""
}

// galleryPageSize is the gallery page size (server first page + each
// infinite-scroll fetch).
const galleryPageSize = 30

func (s *Server) renderPage(w http.ResponseWriter, t *template.Template, d pageData) {
	d.Version = s.version
	// Render to a buffer first so a template error becomes a 500
	// instead of a half-written page.
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", d); err != nil {
		s.log.Error("render page", "active", d.Active, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// ensureName resolves (and on first contact issues) the guest session
// so the shared name field can be pre-filled server-side; "" if
// sessions are unavailable.
func (s *Server) ensureName(w http.ResponseWriter, r *http.Request) (id, name string) {
	if s.sessions == nil {
		return "", ""
	}
	sess, err := s.sessions.Ensure(w, r)
	if err != nil {
		s.log.Error("page session ensure", "err", err)
		return "", ""
	}
	return sess.ID, sess.DisplayName
}

// handleIndex serves the default landing mode: Polaroid.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	_, name := s.ensureName(w, r)
	s.renderPage(w, tplPolaroid, pageData{Active: "polaroid", Name: name})
}

// handleUploadPage serves the photostream-picker page (kgu.16). The
// session's own recent uploads are rendered server-side so the page is
// useful with JavaScript disabled.
func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	id, name := s.ensureName(w, r)
	var recent []store.PhotoListItem
	if id != "" {
		if items, err := s.st.SessionPhotos(id, 60); err != nil {
			s.log.Error("upload page recent", "err", err)
		} else {
			recent = items
		}
	}
	s.renderPage(w, tplUpload, pageData{Active: "upload", Name: name, Recent: recent})
}

// handleGalleryPage serves the reverse-chronological grid (kgu.17).
// The first page is rendered server-side so the Gallery is usable with
// JavaScript disabled (PRD §5a); gallery.js adds infinite scroll.
func (s *Server) handleGalleryPage(w http.ResponseWriter, r *http.Request) {
	_, name := s.ensureName(w, r)
	photos, err := s.st.GalleryPhotos(0, galleryPageSize)
	if err != nil {
		s.log.Error("gallery page query", "err", err)
		// Still render the shell; the grid just starts empty.
	}
	s.renderPage(w, tplGallery, pageData{
		Active:     "gallery",
		Name:       name,
		Photos:     photos,
		NextBefore: nextCursor(photos),
	})
}

// handlePhotoPage is the server-rendered single-photo view (kgu.18):
// the no-JS fallback that thumbnails link to. gallery.js upgrades this
// into an in-page lightbox when JavaScript is available.
func (s *Server) handlePhotoPage(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isHexSHA256(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	meta, ok, err := s.st.PhotoMeta(hash)
	if err != nil {
		s.log.Error("photo page meta", "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, name := s.ensureName(w, r)
	s.renderPage(w, tplPhoto, pageData{
		Active:   "gallery",
		Name:     name,
		ViewHash: meta.Hash,
		ViewName: meta.DisplayName,
	})
}

// nextCursor is the keyset cursor for the page after `photos`: the
// smallest id seen, or 0 when the page was not full (no more rows).
func nextCursor(photos []store.PhotoListItem) int64 {
	if len(photos) < galleryPageSize {
		return 0
	}
	return photos[len(photos)-1].ID
}

func (s *Server) handleAppCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(appCSS)
}

func (s *Server) handlePolaroidJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(polaroidJS)
}

func (s *Server) handleUploadJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(uploadJS)
}

func (s *Server) handleGalleryJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(galleryJS)
}

func (s *Server) handleViewerJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(viewerJS)
}
