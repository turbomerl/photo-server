package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/turbomerl/photo-server/internal/store"
)

// photoTile is the JSON shape the gallery / my-uploads feeds return.
type photoTile struct {
	Hash        string `json:"hash"`
	ThumbURL    string `json:"thumb_url"`
	DisplayName string `json:"display_name,omitempty"`
	UploadedAt  int64  `json:"uploaded_at"`
	HeartCount  int64  `json:"heart_count"`
	Hearted     bool   `json:"hearted"`
}

func toTiles(items []store.PhotoListItem) []photoTile {
	tiles := make([]photoTile, 0, len(items))
	for _, p := range items {
		tiles = append(tiles, photoTile{
			Hash:        p.Hash,
			ThumbURL:    "/thumb/" + p.Hash,
			DisplayName: p.DisplayName,
			UploadedAt:  p.UploadedAt,
			HeartCount:  p.HeartCount,
			Hearted:     p.Hearted,
		})
	}
	return tiles
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// clampLimit keeps page sizes sane regardless of client input.
func clampLimit(raw string, def, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// handlePhotos is the gallery infinite-scroll feed: visible photos
// newest-first, keyset-paginated. `?before=<id>` (0/absent = first
// page). Returns {photos, next_before} where next_before=0 means the
// end (kgu.17).
func (s *Server) handlePhotos(w http.ResponseWriter, r *http.Request) {
	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			before = n
		}
	}
	limit := clampLimit(r.URL.Query().Get("limit"), galleryPageSize, 100)
	// Resolve the viewer's session so each tile knows if they've hearted
	// it (sets the cookie on first contact, like the page handlers).
	viewer := ""
	if s.sessions != nil {
		if sess, err := s.sessions.Ensure(w, r); err == nil {
			viewer = sess.ID
		}
	}
	items, err := s.st.GalleryPhotos(viewer, before, limit)
	if err != nil {
		s.log.Error("gallery feed query", "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	var next int64
	if len(items) == limit {
		next = items[len(items)-1].ID
	}
	writeJSON(w, map[string]any{
		"photos":      toTiles(items),
		"next_before": next,
	})
}

// handleMyUploads returns the current session's own recent uploads,
// newest first (kgu.16 "your recent uploads" — server-backed so it
// survives reloads and tab backgrounding).
func (s *Server) handleMyUploads(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeJSON(w, map[string]any{"photos": []photoTile{}})
		return
	}
	sess, err := s.sessions.Ensure(w, r)
	if err != nil {
		s.log.Error("my-uploads session", "err", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 60, 200)
	items, err := s.st.SessionPhotos(sess.ID, limit)
	if err != nil {
		s.log.Error("my-uploads query", "err", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"photos": toTiles(items)})
}
