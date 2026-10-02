// Package store owns the SQLite file and every write the service performs.
package store

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite handle so callers never touch database/sql directly.
type Store struct {
	db *sql.DB
	// mu serializes version registration so concurrent callers can never
	// observe the same latest version and allocate duplicate or skipped numbers.
	mu sync.Mutex
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
	return &Store{db: db}, nil
}

// Ping reports whether the storage layer is usable.
func (s *Store) Ping() error { return s.db.Ping() }

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Version is one registered structure version of a subject.
type Version struct {
	Subject       string
	Version       int
	Schema        string
	Compatibility string
}

// RegisterVersion appends a new version for subject inside a single transaction. Versions are
// handed out per subject starting at 1 with no gaps or duplicates, even under concurrent calls.
// check runs inside the transaction with the latest stored version (nil when the subject has
// none); a non-nil result aborts the insert and is returned unchanged, leaving stored data
// untouched.
func (s *Store) RegisterVersion(subject, schema, compatibility string, check func(prev *Version) error) (Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Version{}, fmt.Errorf("begin registration: %w", err)
	}
	defer tx.Rollback()

	var prev Version
	hasPrev := true
	row := tx.QueryRow(`SELECT version, schema, compatibility FROM schema_versions WHERE subject = ? ORDER BY version DESC LIMIT 1`, subject)
	switch err := row.Scan(&prev.Version, &prev.Schema, &prev.Compatibility); {
	case err == sql.ErrNoRows:
		hasPrev = false
	case err != nil:
		return Version{}, fmt.Errorf("read latest version: %w", err)
	}

	if check != nil && hasPrev {
		prev.Subject = subject
		if err := check(&prev); err != nil {
			return Version{}, err
		}
	}

	next := prev.Version + 1 // prev.Version is 0 when the subject has no versions yet
	if _, err := tx.Exec(`INSERT INTO schema_versions (subject, version, schema, compatibility) VALUES (?, ?, ?, ?)`, subject, next, schema, compatibility); err != nil {
		return Version{}, fmt.Errorf("insert version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Version{}, fmt.Errorf("commit registration: %w", err)
	}
	return Version{Subject: subject, Version: next, Schema: schema, Compatibility: compatibility}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS service_metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_versions (
	subject       TEXT NOT NULL,
	version       INTEGER NOT NULL,
	schema        TEXT NOT NULL,
	compatibility TEXT NOT NULL,
	PRIMARY KEY (subject, version)
);
`
