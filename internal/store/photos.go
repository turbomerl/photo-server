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
