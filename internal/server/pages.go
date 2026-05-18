package server

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
)

//go:embed assets/templates/*.html
var templatesFS embed.FS

//go:embed assets/app.css
var appCSS []byte

//go:embed assets/polaroid.js
var polaroidJS []byte

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
)

type pageData struct {
	Active  string
	Version string
}

func (s *Server) renderPage(w http.ResponseWriter, t *template.Template, active string) {
	// Render to a buffer first so a template error becomes a 500
	// instead of a half-written page.
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", pageData{Active: active, Version: s.version}); err != nil {
		s.log.Error("render page", "active", active, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// handleIndex serves the default landing mode: Polaroid.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, tplPolaroid, "polaroid")
}

// handleUploadPage serves the photostream-picker shell (kgu.16 fills).
func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, tplUpload, "upload")
}

// handleGalleryPage serves the gallery shell (kgu.17 fills).
func (s *Server) handleGalleryPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, tplGallery, "gallery")
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
