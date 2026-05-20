package store

import (
	"database/sql"
	"errors"
	"time"
)

// Photo is a row to insert. Empty UploaderSessionID and zero TakenAt
// map to SQL NULL (both columns are nullable); zero Width/Height fall
// to the schema's 0 default (libvips fills real dimensions at
// kgu.12/13).
type Photo struct {
	ContentHash       string
	MIME              string
	OriginalFilename  string
	UploaderSessionID string
	DisplayName       string
	TakenAt           time.Time
	UploadedAt        time.Time
	Width             int
	Height            int
}

// InsertPhoto inserts p, deduplicating on content_hash. If a row with
// the same hash already exists it is left untouched and its id is
// returned with deduped=true — this is how repeated uploads of the
// same photo (different guests, retries) collapse to one record.
func (s *Store) InsertPhoto(p Photo) (id int64, deduped bool, err error) {
	var session any // NULL unless a session id is set
	if p.UploaderSessionID != "" {
		session = p.UploaderSessionID
	}
	var taken any // NULL unless a valid EXIF date was found
	if !p.TakenAt.IsZero() {
		taken = p.TakenAt.UTC().Unix()
	}

	res, err := s.db.Exec(`
		INSERT INTO photos
			(content_hash, mime, original_filename, uploader_session_id,
			 display_name, taken_at, uploaded_at, width, height)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(content_hash) DO NOTHING`,
		p.ContentHash, p.MIME, p.OriginalFilename, session,
		p.DisplayName, taken, p.UploadedAt.UTC().Unix(), p.Width, p.Height)
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		id, err = res.LastInsertId()
		return id, false, err
	}

	// Conflict: a photo with this content already exists.
	err = s.db.QueryRow(
		`SELECT id FROM photos WHERE content_hash = ?`, p.ContentHash,
	).Scan(&id)
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// PhotoRef is a minimal photo reference for the conversion backfill.
type PhotoRef struct {
	Hash string
	MIME string
}

// AllPhotos lists every photo (hash + mime) so the startup backfill
// can regenerate any thumbnail/gallery rendition missing after a crash
// or dropped queue item — the appliance self-heals (PRD N8).
func (s *Store) AllPhotos() ([]PhotoRef, error) {
	rows, err := s.db.Query(`SELECT content_hash, mime FROM photos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PhotoRef
	for rows.Next() {
		var r PhotoRef
		if err := rows.Scan(&r.Hash, &r.MIME); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PhotoListItem is one tile in the gallery / "my uploads" feeds.
type PhotoListItem struct {
	ID          int64  `json:"id"`
	Hash        string `json:"hash"`
	MIME        string `json:"mime"`
	DisplayName string `json:"display_name"`
	UploadedAt  int64  `json:"uploaded_at"` // unix seconds (UTC)
}

const scanCols = `id, content_hash, mime, display_name, uploaded_at`

func scanPhotoList(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]PhotoListItem, error) {
	var out []PhotoListItem
	for rows.Next() {
		var p PhotoListItem
		if err := rows.Scan(&p.ID, &p.Hash, &p.MIME, &p.DisplayName, &p.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SessionPhotos returns a session's own visible uploads, newest first
// (kgu.16 "your recent uploads"). limit is capped by the caller.
func (s *Store) SessionPhotos(sessionID string, limit int) ([]PhotoListItem, error) {
	rows, err := s.db.Query(
		`SELECT `+scanCols+` FROM photos
		 WHERE uploader_session_id = ? AND hidden_at IS NULL
		 ORDER BY id DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPhotoList(rows)
}

// GalleryPhotos returns visible photos newest-first for the gallery
// grid (kgu.17). Keyset pagination: pass beforeID=0 for the first
// page, then the smallest id from the previous page. Uses the
// idx_photos_visible_recent partial index.
func (s *Store) GalleryPhotos(beforeID int64, limit int) ([]PhotoListItem, error) {
	q := `SELECT ` + scanCols + ` FROM photos WHERE hidden_at IS NULL`
	args := []any{}
	if beforeID > 0 {
		q += ` AND id < ?`
		args = append(args, beforeID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPhotoList(rows)
}

// AdminPhotoRow is a row for the admin dashboard (kgu.19) — like
// PhotoListItem but also exposes hidden state and original filename.
type AdminPhotoRow struct {
	Hash             string
	MIME             string
	DisplayName      string
	OriginalFilename string
	UploadedAt       int64
	HiddenAt         *int64 // nil = visible
}

// AdminPhotos lists the most-recent photos (visible AND hidden) for
// the operator dashboard, newest first.
func (s *Store) AdminPhotos(limit int) ([]AdminPhotoRow, error) {
	rows, err := s.db.Query(
		`SELECT content_hash, mime, display_name, original_filename,
		        uploaded_at, hidden_at
		 FROM photos ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AdminPhotoRow
	for rows.Next() {
		var r AdminPhotoRow
		if err := rows.Scan(&r.Hash, &r.MIME, &r.DisplayName,
			&r.OriginalFilename, &r.UploadedAt, &r.HiddenAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PhotoCounts returns (visible, hidden) — for the admin header.
// COALESCE guards against SUM(...) returning NULL when the table is
// empty, which would fail to scan into int.
func (s *Store) PhotoCounts() (visible, hidden int, err error) {
	err = s.db.QueryRow(
		`SELECT
		   COALESCE(SUM(CASE WHEN hidden_at IS NULL THEN 1 ELSE 0 END), 0),
		   COALESCE(SUM(CASE WHEN hidden_at IS NOT NULL THEN 1 ELSE 0 END), 0)
		 FROM photos`).Scan(&visible, &hidden)
	return visible, hidden, err
}

// SetHidden marks a photo hidden (now) or visible (NULL). Hidden
// photos are excluded from /api/photos, /thumb, /photo, /original and
// /p — so a hide is immediate, no rebuild required.
func (s *Store) SetHidden(hash string, hidden bool) error {
	var args []any
	q := `UPDATE photos SET hidden_at = `
	if hidden {
		q += `?`
		args = append(args, time.Now().UTC().Unix())
	} else {
		q += `NULL`
	}
	q += ` WHERE content_hash = ?`
	args = append(args, hash)
	_, err := s.db.Exec(q, args...)
	return err
}

// DeletePhoto removes the row and returns the mime so the caller can
// clean up the on-disk blobs (best-effort). Missing row → ok=false.
func (s *Store) DeletePhoto(hash string) (mime string, ok bool, err error) {
	err = s.db.QueryRow(
		`SELECT mime FROM photos WHERE content_hash = ?`, hash,
	).Scan(&mime)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if _, err := s.db.Exec(
		`DELETE FROM photos WHERE content_hash = ?`, hash); err != nil {
		return "", false, err
	}
	return mime, true, nil
}

// PhotoMeta is what the full-size view / download routes need (kgu.18).
type PhotoMeta struct {
	Hash             string
	MIME             string
	OriginalFilename string
	DisplayName      string
}

// PhotoMeta looks up a *visible* photo by content hash. Hidden photos
// (admin-hidden, kgu.19) report ok=false so a direct /photo, /original
// or /p URL can't bypass moderation.
func (s *Store) PhotoMeta(hash string) (PhotoMeta, bool, error) {
	m := PhotoMeta{Hash: hash}
	err := s.db.QueryRow(
		`SELECT mime, original_filename, display_name FROM photos
		 WHERE content_hash = ? AND hidden_at IS NULL`, hash,
	).Scan(&m.MIME, &m.OriginalFilename, &m.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return PhotoMeta{}, false, nil
	}
	if err != nil {
		return PhotoMeta{}, false, err
	}
	return m, true, nil
}

// PhotoByHash looks up a photo's mime by content hash (ok=false if
// absent). Used by the lazy-regenerate-on-miss thumbnail route.
func (s *Store) PhotoByHash(hash string) (PhotoRef, bool, error) {
	var r PhotoRef
	r.Hash = hash
	err := s.db.QueryRow(
		`SELECT mime FROM photos WHERE content_hash = ?`, hash,
	).Scan(&r.MIME)
	if errors.Is(err, sql.ErrNoRows) {
		return PhotoRef{}, false, nil
	}
	if err != nil {
		return PhotoRef{}, false, err
	}
	return r, true, nil
}

// PhotoTakenAt returns the stored taken_at for a photo id (ok=false if
// the photo is absent or taken_at is NULL). Used by tests and later by
// the libvips backfill path.
func (s *Store) PhotoTakenAt(id int64) (t time.Time, ok bool, err error) {
	var unix *int64
	err = s.db.QueryRow(`SELECT taken_at FROM photos WHERE id = ?`, id).Scan(&unix)
	if err != nil || unix == nil {
		return time.Time{}, false, err
	}
	return time.Unix(*unix, 0).UTC(), true, nil
}
