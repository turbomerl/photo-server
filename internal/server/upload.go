package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
	"github.com/turbomerl/photo-server/internal/exif"
	"github.com/turbomerl/photo-server/internal/store"
)

// uploadResult is the per-file outcome reported back to the client.
type uploadResult struct {
	Filename    string `json:"filename"`
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	Hash        string `json:"hash,omitempty"`
	MIME        string `json:"mime,omitempty"`
	Deduped     bool   `json:"deduped,omitempty"`
	PhotoID     int64  `json:"photo_id,omitempty"`
	ThumbURL    string `json:"thumb_url,omitempty"`
	OriginalURL string `json:"original_url,omitempty"`
}

// acceptedTypes maps a detected/declared image type to its canonical
// MIME and stored extension. Per PRD F5 (JPEG/PNG/HEIC at minimum).
var acceptedTypes = map[string]struct {
	mime string
	ext  string
}{
	"jpeg": {"image/jpeg", ".jpg"},
	"png":  {"image/png", ".png"},
	"heic": {"image/heic", ".heic"},
	"heif": {"image/heif", ".heif"},
}

// handleUpload accepts a multipart POST of one or more photos. Each
// file is hashed + stored (dedup), EXIF-dated, optionally tagged to the
// uploader's session, and recorded. The session id comes from the
// `ps_session` cookie or a `session_id` query param; the display name
// from a `display_name` query param or form field (kgu.14 owns secure
// issuance). Parts are streamed — no whole-file buffering (PRD N4).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart/form-data", http.StatusBadRequest)
		return
	}

	sessionID := s.requestSessionID(r)
	displayName := strings.TrimSpace(r.URL.Query().Get("display_name"))

	var results []uploadResult
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "malformed multipart body", http.StatusBadRequest)
			return
		}

		// Non-file parts carry the optional name/session form fields.
		if part.FileName() == "" {
			val := readFormValue(part)
			switch part.FormName() {
			case "display_name":
				if displayName == "" {
					displayName = strings.TrimSpace(val)
				}
			case "session_id":
				if sessionID == "" {
					sessionID = strings.TrimSpace(val)
				}
			}
			_ = part.Close()
			continue
		}

		results = append(results, s.storeOnePart(part, sessionID, displayName))
		_ = part.Close()
	}

	if len(results) == 0 {
		http.Error(w, "no files in upload", http.StatusBadRequest)
		return
	}

	status := http.StatusOK
	anyOK := false
	for _, r := range results {
		if r.OK {
			anyOK = true
			break
		}
	}
	if !anyOK {
		status = http.StatusUnsupportedMediaType
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count":    len(results),
		"uploaded": results,
	})
}

// storeOnePart stores a single file part and returns its result. A
// failure on one file never aborts the others.
func (s *Server) storeOnePart(part *multipart.Part, sessionID, displayName string) uploadResult {
	filename := part.FileName()
	res := uploadResult{Filename: filename}

	// Sniff a header to identify the type without trusting the
	// extension/Content-Type a phone sends.
	head := make([]byte, 512)
	n, _ := io.ReadFull(io.LimitReader(part, 512), head)
	head = head[:n]
	kind := detectImage(head, filename)
	at, ok := acceptedTypes[kind]
	if !ok {
		res.Error = "unsupported file type"
		// Drain so the multipart reader can advance cleanly.
		_, _ = io.Copy(io.Discard, part)
		return res
	}

	// Re-join the sniffed head with the rest, bounded by maxBody.
	body := io.MultiReader(bytes.NewReader(head), part)
	limited := &limitedReader{r: body, remaining: s.maxBody + 1}

	hash, size, err := s.blobs.PutOriginal(limited, at.ext)
	if err != nil {
		if limited.exceeded {
			res.Error = "file too large"
		} else {
			res.Error = "store failed"
			s.log.Error("blob store failed", "file", filename, "err", err)
		}
		return res
	}

	// EXIF taken_at from the stored original (off the network path).
	var takenAt time.Time
	if f, err := s.blobs.Open(blobstore.Original, hash, at.ext); err == nil {
		if t, ok := exif.DateTimeOriginal(f, size); ok {
			takenAt = t
		}
		_ = f.Close()
	}

	if sessionID != "" {
		if err := s.st.UpsertSession(sessionID, displayName); err != nil {
			res.Error = "session error"
			s.log.Error("upsert session", "err", err)
			return res
		}
	}

	id, deduped, err := s.st.InsertPhoto(store.Photo{
		ContentHash:       hash,
		MIME:              at.mime,
		OriginalFilename:  filepath.Base(filename),
		UploaderSessionID: sessionID,
		DisplayName:       displayName,
		TakenAt:           takenAt,
		UploadedAt:        time.Now().UTC(),
	})
	if err != nil {
		res.Error = "db error"
		s.log.Error("insert photo", "err", err)
		return res
	}

	// HEIC/HEIF needs a browser-viewable JPEG. Queue it off the hot
	// path (PRD R4/R5); the gallery JPEG appears shortly after upload.
	if s.conv != nil && (at.mime == "image/heic" || at.mime == "image/heif") {
		s.conv.Enqueue(hash, at.ext)
	}

	res.OK = true
	res.Hash = hash
	res.MIME = at.mime
	res.Deduped = deduped
	res.PhotoID = id
	res.ThumbURL = "/thumb/" + hash    // served by kgu.13
	res.OriginalURL = "/photo/" + hash // served by kgu.17/18
	return res
}

// requestSessionID prefers the persistent cookie (kgu.14), then a
// query param fallback for clients that can't yet set it.
func (s *Server) requestSessionID(r *http.Request) string {
	if c, err := r.Cookie("ps_session"); err == nil && c.Value != "" {
		return c.Value
	}
	return strings.TrimSpace(r.URL.Query().Get("session_id"))
}

func readFormValue(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 4096))
	return string(b)
}

// detectImage returns "jpeg"/"png"/"heic"/"heif"/"" using content
// sniffing first, then the filename extension as a fallback.
func detectImage(head []byte, filename string) string {
	switch http.DetectContentType(head) {
	case "image/jpeg":
		return "jpeg"
	case "image/png":
		return "png"
	}
	// HEIF/HEIC: ISO-BMFF `ftyp` brand at bytes 4..12.
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		switch string(head[8:12]) {
		case "heic", "heix", "hevc", "heim", "heis":
			return "heic"
		case "mif1", "msf1", "heif":
			return "heif"
		}
	}
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), ".")) {
	case "jpg", "jpeg":
		return "jpeg"
	case "png":
		return "png"
	case "heic":
		return "heic"
	case "heif":
		return "heif"
	}
	return ""
}

// limitedReader fails the read once more than the configured cap has
// been consumed, so a pathological upload can't fill the disk.
type limitedReader struct {
	r         io.Reader
	remaining int64
	exceeded  bool
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		l.exceeded = true
		return 0, errors.New("upload exceeds size limit")
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	return n, err
}
