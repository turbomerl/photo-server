// Package store is photo-server's SQLite persistence layer: schema,
// versioned migrations, and the metadata that the upload, gallery,
// session and admin features attach to in later work.
//
// Pure-Go driver (modernc.org/sqlite, no cgo) to keep the appliance a
// single static binary with no libsqlite3 system dependency.
package store

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the SQLite database at path, applies
// the appliance pragmas, and runs all pending migrations. The returned
// Store is ready to use; call Close at shutdown.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// dsn builds a modernc.org/sqlite DSN that applies the appliance
// pragmas on every pooled connection:
//
//   - journal_mode=WAL  : gallery readers don't block the uploader (PRD N4)
//   - synchronous=FULL  : every COMMIT fsyncs the WAL, so an ack'd
//     upload survives a hard power-off (PRD N6 — durability beats raw
//     write throughput at this event's modest volume)
//   - foreign_keys=ON   : enforce photos.uploader_session_id -> sessions(id)
//   - busy_timeout=5000 : ride out brief write contention at upload bursts
func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "foreign_keys(ON)")
	return "file:" + path + "?" + q.Encode()
}

// DB exposes the underlying handle for the feature packages (upload,
// gallery, sessions, admin) built in later work.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
