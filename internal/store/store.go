// Package store owns the SQLite file and every write the service performs.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// Lookup failures reported by GetVersion.
var (
	ErrSubjectNotFound = errors.New("subject has no registered versions")
	ErrVersionNotFound = errors.New("subject does not have the requested version")
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

// ListSubjects returns every subject that has at least one registered
// version, in ascending numeric order and without duplicates. The result is
// never nil, so an empty registry marshals as an empty JSON array.
func (s *Store) ListSubjects() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT subject FROM schema_versions ORDER BY subject`)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	defer rows.Close()

	subjects := []int64{}
	for rows.Next() {
		var subject int64
		if err := rows.Scan(&subject); err != nil {
			return nil, fmt.Errorf("scan subject: %w", err)
		}
		subjects = append(subjects, subject)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	return subjects, nil
}

// GetVersion returns the raw schema and compatibility level stored for one
// version of a subject. It reports ErrSubjectNotFound when the subject has no
// versions at all and ErrVersionNotFound when the subject exists but the
// version does not.
func (s *Store) GetVersion(subject, version int64) (schema, compatibility string, err error) {
	err = s.db.QueryRow(`SELECT schema, compatibility FROM schema_versions WHERE subject = ? AND version = ?`, subject, version).Scan(&schema, &compatibility)
	if err == nil {
		return schema, compatibility, nil
	}
	if err != sql.ErrNoRows {
		return "", "", fmt.Errorf("read version: %w", err)
	}

	var exists int
	err = s.db.QueryRow(`SELECT 1 FROM schema_versions WHERE subject = ? LIMIT 1`, subject).Scan(&exists)
	if err == sql.ErrNoRows {
		return "", "", ErrSubjectNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("check subject: %w", err)
	}
	return "", "", ErrVersionNotFound
}

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
