// Package store owns the SQLite file and every write the service performs.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite handle so callers never touch database/sql directly.
type Store struct {
	db *sql.DB
}

// Open prepares the database file and the schema this service needs.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// A single connection serializes every transaction, so concurrent
	// registrations can neither reuse nor skip version numbers.
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

// Ping reports whether the storage layer is usable.
func (s *Store) Ping() error { return s.db.Ping() }

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// RegisterVersion stores schema as the next consecutive version for subject and
// returns the assigned version number. When the subject already has versions,
// validate is called with the raw schema of the latest one, inside the same
// transaction; returning an error rejects the registration without writing
// anything or consuming a version number.
func (s *Store) RegisterVersion(subject int64, schema, compatibility string, validate func(previous string) error) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin registration: %w", err)
	}
	defer tx.Rollback()

	var version int64
	var previous string
	err = tx.QueryRow(`SELECT version, schema FROM schema_versions WHERE subject = ? ORDER BY version DESC LIMIT 1`, subject).Scan(&version, &previous)
	if err == sql.ErrNoRows {
		version = 0
	} else if err != nil {
		return 0, fmt.Errorf("read latest version: %w", err)
	}
	if version > 0 && validate != nil {
		if err := validate(previous); err != nil {
			return 0, err
		}
	}

	version++
	if _, err := tx.Exec(`INSERT INTO schema_versions (subject, version, schema, compatibility) VALUES (?, ?, ?, ?)`, subject, version, schema, compatibility); err != nil {
		return 0, fmt.Errorf("insert version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit registration: %w", err)
	}
	return version, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS service_metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_versions (
	subject       INTEGER NOT NULL,
	version       INTEGER NOT NULL,
	schema        TEXT NOT NULL,
	compatibility TEXT NOT NULL,
	PRIMARY KEY (subject, version)
);
`
