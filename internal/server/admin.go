// Admin surface (kgu.19). A hidden /admin page protected by HTTP
// Basic auth against PHOTO_SERVER_ADMIN_PASSWORD. If the password is
// empty the admin surface is disabled entirely (fail-closed, so a
// misconfigured deploy isn't open). No public link from the guest UI.
package server

import (
	"crypto/subtle"
	_ "embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/convert"
)

//go:embed assets/templates/admin.html
var adminTplBytes []byte

var tplAdmin = template.Must(template.New("admin").Funcs(template.FuncMap{
	"bytes":   formatBytes,
	"agoUnix": agoUnix,
}).Parse(string(adminTplBytes)))

type adminPage struct {
	Visible, Hidden int
	UsedBytes       int64
	FSTotal, FSFree int64
	Photos          []adminPhoto
}

type adminPhoto struct {
	Hash             string
	OriginalFilename string
	DisplayName      string
	UploadedAt       int64
	HiddenAt         *int64
}

// requireAdmin guards every /admin/* route. Constant-time password
// compare (cheap correctness; PRD N11 LAN has no adversary but the
// password protects against operator-LAN accidents — phones with the
// admin URL in history, etc.).
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.adminPassword == "" {
		http.NotFound(w, r) // surface disabled
		return false
	}
	_, pw, ok := r.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(pw), []byte(s.adminPassword)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="photo-server admin"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	vis, hid, err := s.st.PhotoCounts()
	if err != nil {
		s.log.Error("admin counts", "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	rows, err := s.st.AdminPhotos(50)
	if err != nil {
		s.log.Error("admin photos", "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	used := blobsUsedBytes(s.blobs.Root())
	total, free := fsBytes(s.blobs.Root())

	p := adminPage{
		Visible: vis, Hidden: hid,
		UsedBytes: used, FSTotal: total, FSFree: free,
	}
	for _, r := range rows {
		p.Photos = append(p.Photos, adminPhoto{
			Hash:             r.Hash,
			OriginalFilename: r.OriginalFilename,
			DisplayName:      r.DisplayName,
			UploadedAt:       r.UploadedAt,
			HiddenAt:         r.HiddenAt,
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tplAdmin.Execute(w, p); err != nil {
		s.log.Error("admin render", "err", err)
	}
}

func (s *Server) handleAdminHide(w http.ResponseWriter, r *http.Request) {
	s.adminSetHidden(w, r, true)
}
func (s *Server) handleAdminUnhide(w http.ResponseWriter, r *http.Request) {
	s.adminSetHidden(w, r, false)
}

func (s *Server) adminSetHidden(w http.ResponseWriter, r *http.Request, hidden bool) {
	if !s.requireAdmin(w, r) {
		return
	}
	hash := r.PathValue("hash")
	if !isHexSHA256(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	if err := s.st.SetHidden(hash, hidden); err != nil {
		s.log.Error("admin set hidden", "hash", hash, "hidden", hidden, "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	// PRG so the operator can refresh without re-posting.
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	hash := r.PathValue("hash")
	if !isHexSHA256(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	mime, ok, err := s.st.DeletePhoto(hash)
	if err != nil {
		s.log.Error("admin delete row", "hash", hash, "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Best-effort blob cleanup. DB+filesystem can't be made
	// transactional; orphan blobs (if any) are harmless and could be
	// reaped by a future janitor.
	ext := convert.ExtForMIME(mime)
	for _, k := range []blobstore.Kind{blobstore.Original, blobstore.Thumb, blobstore.Gallery} {
		if err := s.blobs.Remove(k, hash, ext); err != nil {
			s.log.Warn("admin blob remove", "kind", k, "hash", hash, "err", err)
		}
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// handleAdminShutdown initiates a clean shutdown via self-SIGTERM —
// the same path systemctl stop / SIGINT take, so the HTTP server
// drains in-flight requests and the SQLite WAL checkpoints (PRD F15).
func (s *Server) handleAdminShutdown(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset=utf-8>
<title>Shutting down…</title><body style="font:17px system-ui;padding:30px">
<p>Shutting down — it's safe to unplug the box once the lights settle.</p>`))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	// Defer the signal so this response is fully sent first; the
	// graceful shutdown path then drains any other in-flight requests.
	time.AfterFunc(200*time.Millisecond, shutdownSignal)
}

// shutdownSignal is the package-level hook the admin shutdown handler
// uses to trigger the existing SIGTERM path. Overridden in tests so
// unit runs can't actually kill the test binary.
var shutdownSignal = func() {
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
}

// --- storage helpers ---------------------------------------------------

func blobsUsedBytes(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable; best-effort
		}
		if d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

func fsBytes(path string) (total, free int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize)
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func agoUnix(secs int64) string {
	d := time.Since(time.Unix(secs, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d d ago", int(d.Hours()/24))
	}
}
