package store

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// migrate applies every embedded migration whose version is greater
// than the database's current PRAGMA user_version, in ascending order,
// each in its own transaction. It is idempotent: a fully-migrated
// database is a no-op.
func (s *Store) migrate() error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}

	current, err := s.SchemaVersion()
	if err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	for _, m := range migs {
		if m.version <= current {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %04d (%s): %w", m.version, m.name, err)
		}
		// PRAGMA user_version cannot be parameterised; m.version is a
		// validated int parsed from the trusted embedded filename.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set user_version %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %04d: %w", m.version, err)
		}
	}
	return nil
}

// SchemaVersion returns the database's current schema version (0 on a
// fresh database).
func (s *Store) SchemaVersion() (int, error) {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// loadMigrations reads and orders the embedded NNNN_name.sql files,
// rejecting malformed names and duplicate versions so a packaging
// mistake fails loudly at startup rather than silently skipping schema.
func loadMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	var migs []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q: expected NNNN_name.sql", e.Name())
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %q: bad version prefix: %w", e.Name(), err)
		}
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		migs = append(migs, migration{version: v, name: e.Name(), sql: string(b)})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	for i := 1; i < len(migs); i++ {
		if migs[i].version == migs[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", migs[i].version)
		}
	}
	return migs, nil
}
