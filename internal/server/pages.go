package server

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"strings"

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

//go:embed assets/heart.js
var heartJS []byte

//go:embed assets/resize.js
var resizeJS []byte

//go:embed assets/img/*.jpeg
var imgFS embed.FS

//go:embed assets/fonts/*.woff2
var fontsFS embed.FS

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
	// ViewHearts/ViewHearted drive the heart control on the single-photo
	// page (kgu.23).
	ViewHearts  int64
	ViewHearted bool
	// Filter is the gallery mode: "" (All, newest-first) or "loved" (the
	// "Most loved" best-of showcase). Drives the filter tabs + the
	// single-column showcase layout, and disables infinite scroll for loved.
	// TotalCount / LovedCount feed the tab counters.
	Filter     string
	TotalCount int
	LovedCount int
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
// infinite-scroll fetch). topGalleryLimit caps the "Most loved"
// leaderboard (fixed top-N, no infinite scroll).
const (
	galleryPageSize = 30
	topGalleryLimit = 60
)

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
	id, name := s.ensureName(w, r)

	var (
		photos []store.PhotoListItem
		err    error
		next   int64
		filter string
	)
	if r.URL.Query().Get("filter") == "loved" {
		filter = "loved"
		photos, err = s.st.TopPhotos(id, topGalleryLimit) // fixed top-N best-of, no infinite scroll
	} else {
		photos, err = s.st.GalleryPhotos(id, 0, galleryPageSize)
		if err == nil {
			next = nextCursor(photos)
		}
	}
	if err != nil {
		s.log.Error("gallery page query", "err", err, "filter", filter)
		// Still render the shell; the grid just starts empty.
	}
	// Tab counters: total visible photos + how many have any love.
	total, _, cErr := s.st.PhotoCounts()
	loved, lErr := s.st.LovedCount()
	if cErr != nil || lErr != nil {
		s.log.Error("gallery counts", "countsErr", cErr, "lovedErr", lErr)
	}
	s.renderPage(w, tplGallery, pageData{
		Active:     "gallery",
		Name:       name,
		Photos:     photos,
		NextBefore: next,
		Filter:     filter,
		TotalCount: total,
		LovedCount: loved,
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
	id, name := s.ensureName(w, r)
	hearts, hearted, err := s.st.PhotoHeartState(hash, id)
	if err != nil {
		s.log.Error("photo page heart state", "err", err)
	}
	s.renderPage(w, tplPhoto, pageData{
		Active:      "gallery",
		Name:        name,
		ViewHash:    meta.Hash,
		ViewName:    meta.DisplayName,
		ViewHearts:  hearts,
		ViewHearted: hearted,
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

func (s *Server) handleHeartJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(heartJS)
}

func (s *Server) handleResizeJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(resizeJS)
}

// handleImg serves embedded hero/static images from /static/img/{name}.
// Only files baked into assets/img/ are reachable — nothing on disk.
func (s *Server) handleImg(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Reject path separators — defense in depth; the mux {name} pattern
	// already disallows slashes.
	if name == "" || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	b, err := imgFS.ReadFile("assets/img/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(b)
}

// handleFont serves embedded self-hosted woff2 fonts from
// /static/fonts/{name}. This route is exempt from the access gate so the
// gate page itself can use the fonts (see requireAccess).
func (s *Server) handleFont(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	b, err := fontsFS.ReadFile("assets/fonts/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(b)
}
