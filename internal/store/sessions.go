package store

import (
	"database/sql"
	"errors"
	"time"
)

// Session is a stored guest session.
type Session struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
	LastSeenAt  time.Time
}

// UpsertSession ensures a sessions row exists for id (so an uploaded
// photo can reference it via the foreign key) and bumps last_seen_at.
// A non-empty displayName overwrites the stored name; an empty one
// leaves any existing name intact (PRD: the name is set once and
// remembered). Secure token issuance and the cookie/localStorage
// mechanics are kgu.14's responsibility — this only persists what an
// upload presents.
func (s *Store) UpsertSession(id, displayName string) error {
	now := time.Now().UTC().Unix()
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, display_name, created_at, last_seen_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_seen_at = excluded.last_seen_at,
			display_name = CASE
				WHEN excluded.display_name <> '' THEN excluded.display_name
				ELSE sessions.display_name
			END`,
		id, displayName, now, now)
	return err
}

// GetSession returns the session row; ok is false if it does not exist.
func (s *Store) GetSession(id string) (sess Session, ok bool, err error) {
	var created, seen int64
	err = s.db.QueryRow(
		`SELECT id, display_name, created_at, last_seen_at FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.DisplayName, &created, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.LastSeenAt = time.Unix(seen, 0).UTC()
	return sess, true, nil
}
