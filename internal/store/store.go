package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// Store wraps the database handle used for triage indexing.
type Store struct {
	db *sql.DB
}

var sqliteVecAutoOnce sync.Once

func Open(ctx context.Context, dbPath string) (*Store, error) {
	if dbPath == "" {
		return nil, errors.New("db path is required")
	}

	sqliteVecAutoOnce.Do(func() {
		sqlite_vec.Auto()
	})

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if err := configureDatabase(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite db: %w", err)
	}

	if err := ApplyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return &Store{db: db}, nil
}

func OpenInMemory(ctx context.Context) (*Store, error) {
	return Open(ctx, ":memory:")
}

func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}

	return s.db
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

func configureDatabase(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000;`); err != nil {
		return err
	}

	return db.PingContext(ctx)
}
