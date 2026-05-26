package store

import (
	"database/sql"
	"errors"
	"time"
)

// ToggleHeart flips the current session's heart on a visible photo
// (identified by content hash) and returns the photo's new heart count
// and whether this session now hearts it. A hidden or absent photo
// reports ok=false. One heart per session is enforced by the hearts PK;
// the denormalized photos.heart_count is updated in the same
// transaction so the count and the membership row never diverge.
func (s *Store) ToggleHeart(hash, sessionID string) (count int64, hearted, ok bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var photoID int64
	err = tx.QueryRow(
		`SELECT id FROM photos WHERE content_hash = ? AND hidden_at IS NULL`, hash,
	).Scan(&photoID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, err
	}

	res, err := tx.Exec(
		`DELETE FROM hearts WHERE photo_id = ? AND session_id = ?`, photoID, sessionID)
	if err != nil {
		return 0, false, false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		// Was hearted → now removed.
		if _, err := tx.Exec(
			`UPDATE photos SET heart_count = heart_count - 1 WHERE id = ?`, photoID); err != nil {
			return 0, false, false, err
		}
		hearted = false
	} else {
		if _, err := tx.Exec(
			`INSERT INTO hearts (photo_id, session_id, created_at) VALUES (?, ?, ?)`,
			photoID, sessionID, time.Now().UTC().Unix()); err != nil {
			return 0, false, false, err
		}
		if _, err := tx.Exec(
			`UPDATE photos SET heart_count = heart_count + 1 WHERE id = ?`, photoID); err != nil {
			return 0, false, false, err
		}
		hearted = true
	}

	if err := tx.QueryRow(
		`SELECT heart_count FROM photos WHERE id = ?`, photoID).Scan(&count); err != nil {
		return 0, false, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, false, err
	}
	return count, hearted, true, nil
}

// PhotoHeartState returns a visible photo's heart count and whether the
// viewer's session hearts it — for the server-rendered single-photo
// page (kgu.18 + kgu.23). A missing/hidden photo reports 0, false.
func (s *Store) PhotoHeartState(hash, viewerSessionID string) (count int64, hearted bool, err error) {
	var h int
	err = s.db.QueryRow(`
		SELECT p.heart_count, (x.session_id IS NOT NULL)
		FROM photos p
		LEFT JOIN hearts x ON x.photo_id = p.id AND x.session_id = ?
		WHERE p.content_hash = ? AND p.hidden_at IS NULL`,
		viewerSessionID, hash).Scan(&count, &h)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return count, h != 0, nil
}

// LovedCount returns how many visible photos have at least one heart —
// the counter on the "Most loved" gallery tab (kgu.23).
func (s *Store) LovedCount() (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM photos WHERE hidden_at IS NULL AND heart_count > 0`,
	).Scan(&n)
	return n, err
}

// TopPhotos returns the most-hearted visible photos (the "Most loved"
// leaderboard, kgu.23), hearts-desc then newest-first, capped at limit.
// Photos with zero hearts are excluded. viewerSessionID flags which
// tiles the requesting guest has already hearted. The view is a fixed
// top-N (no infinite scroll), so a live-changing sort can't skip or
// duplicate tiles mid-scroll.
func (s *Store) TopPhotos(viewerSessionID string, limit int) ([]PhotoListItem, error) {
	rows, err := s.db.Query(photoSelect+`
		WHERE p.hidden_at IS NULL AND p.heart_count > 0
		ORDER BY p.heart_count DESC, p.id DESC LIMIT ?`,
		viewerSessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPhotoList(rows)
}
