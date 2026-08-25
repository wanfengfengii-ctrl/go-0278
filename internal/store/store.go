// Package store implements the transactional relational persistence layer for
// the rock-bolt pullout inspection service. It applies the embedded schema,
// enforces the documented uniqueness and version constraints in SQL, and gives
// the service layer atomic transactions so no partial snapshot, sample, lease,
// evidence or verdict can survive a failed write.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no cgo

	"rockbolt-pullout-zonal-closure/migrations"
)

// Sentinel errors surfaced by the store. Callers map these to stable HTTP
// error codes without leaking database internals.
var (
	ErrNotFound  = errors.New("store: not found")
	ErrConflict  = errors.New("store: version conflict")
	ErrDuplicate = errors.New("store: duplicate key")
)

// Store is a SQL-backed repository. A nil database path opens an in-memory
// database, which is useful for tests; a filesystem path gives durable,
// restartable persistence.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies the schema.
// An empty path selects an in-memory database.
func Open(path string) (*Store, error) {
	dsn := path
	if dsn == "" {
		dsn = ":memory:"
	} else {
		dsn = "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1) // single writer keeps SQLite transactions serializable
	if _, err := db.Exec(migrations.InitSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying handle for health checks and advanced queries.
func (s *Store) DB() *sql.DB { return s.db }

// tx runs fn inside a single transaction, committing on success and rolling
// back on any error. It translates sqlite constraint failures into the sentinel
// duplicate error.
func (s *Store) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return mapConstraint(err)
	}
	return tx.Commit()
}

// mapConstraint maps the sqlite unique-constraint error to ErrDuplicate while
// leaving sentinel errors and everything else untouched.
func mapConstraint(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrDuplicate) {
		return err
	}
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

func isUniqueViolation(err error) bool {
	return containsAny(err.Error(), "UNIQUE constraint failed", "constraint failed")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// requireFound maps a sql.ErrNoRows result to ErrNotFound.
func requireFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
