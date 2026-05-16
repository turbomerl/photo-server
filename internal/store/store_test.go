package store

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "photo-server.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAppliesMigrations(t *testing.T) {
	s := openTemp(t)

	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("schema version = %d, want 1", v)
	}

	for _, table := range []string{"sessions", "photos"} {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo-server.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	_ = s1.Close()

	// Re-open the same file: migrations already applied, must be a
	// clean no-op at the same version.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer s2.Close()

	v, err := s2.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("schema version after re-open = %d, want 1", v)
	}
}

func TestAppliancePragmas(t *testing.T) {
	s := openTemp(t)

	var fk int
	if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1 (FK enforcement off)", fk)
	}

	var mode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestContentHashUnique(t *testing.T) {
	s := openTemp(t)

	insert := `INSERT INTO photos (content_hash, mime, uploaded_at) VALUES (?, 'image/jpeg', 1)`
	if _, err := s.DB().Exec(insert, "abc123"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := s.DB().Exec(insert, "abc123"); err == nil {
		t.Fatal("duplicate content_hash inserted; UNIQUE not enforced")
	}
}

func TestForeignKeyEnforced(t *testing.T) {
	s := openTemp(t)

	// Unknown session id must be rejected.
	_, err := s.DB().Exec(
		`INSERT INTO photos (content_hash, mime, uploaded_at, uploader_session_id)
		 VALUES ('h1', 'image/jpeg', 1, 'ghost')`)
	if err == nil {
		t.Fatal("photo with unknown uploader_session_id inserted; FK not enforced")
	}

	// With a real session it succeeds.
	if _, err := s.DB().Exec(
		`INSERT INTO sessions (id, created_at, last_seen_at) VALUES ('sess1', 1, 1)`); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO photos (content_hash, mime, uploaded_at, uploader_session_id)
		 VALUES ('h2', 'image/jpeg', 1, 'sess1')`); err != nil {
		t.Fatalf("insert photo with valid session: %v", err)
	}
}
