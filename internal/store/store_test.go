package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesSchemaOnFreshDB(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	db := s.DB()

	requireObjectExists(t, db, "table", "schema_version")
	requireObjectExists(t, db, "table", "items")
	requireObjectExists(t, db, "table", "items_vec")
	requireObjectExists(t, db, "table", "items_fts")

	requireObjectExists(t, db, "trigger", "items_fts_insert")
	requireObjectExists(t, db, "trigger", "items_fts_delete")
	requireObjectExists(t, db, "trigger", "items_fts_update")

	version, err := currentSchemaVersion(ctx, db)
	if err != nil {
		t.Fatalf("currentSchemaVersion() error = %v", err)
	}
	if version != LatestSchemaVersion() {
		t.Fatalf("schema version = %d, want %d", version, LatestSchemaVersion())
	}
}

func TestApplyMigrationsFromVersionOne(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "upgrade.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Fatalf("db.Close() error = %v", cerr)
		}
	}()

	if err := ensureSchemaVersionTable(ctx, db); err != nil {
		t.Fatalf("ensureSchemaVersionTable() error = %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	if err := migrations[0].up(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("migrations[0].up() error = %v", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, ?);`,
		migrations[0].version,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert version row error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("ApplyMigrations() error = %v", err)
	}

	requireObjectExists(t, db, "table", "items_fts")
	requireObjectExists(t, db, "table", "items_vec")
	requireObjectExists(t, db, "trigger", "items_fts_insert")

	version, err := currentSchemaVersion(ctx, db)
	if err != nil {
		t.Fatalf("currentSchemaVersion() error = %v", err)
	}
	if version != LatestSchemaVersion() {
		t.Fatalf("schema version = %d, want %d", version, LatestSchemaVersion())
	}
}

func TestApplyMigrationsIsIdempotent(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "idempotent.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Fatalf("db.Close() error = %v", cerr)
		}
	}()

	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("first ApplyMigrations() error = %v", err)
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("second ApplyMigrations() error = %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_version;`).Scan(&count); err != nil {
		t.Fatalf("count schema_version rows error = %v", err)
	}

	if count != LatestSchemaVersion() {
		t.Fatalf("schema_version row count = %d, want %d", count, LatestSchemaVersion())
	}
}

func requireObjectExists(t *testing.T, db *sql.DB, objectType, name string) {
	t.Helper()

	const stmt = `
SELECT COUNT(*)
FROM sqlite_master
WHERE type = ? AND name = ?;
`

	var count int
	if err := db.QueryRow(stmt, objectType, name).Scan(&count); err != nil {
		t.Fatalf("query sqlite_master (%s, %s) error = %v", objectType, name, err)
	}

	if count != 1 {
		t.Fatalf("sqlite object (%s, %s) not found", objectType, name)
	}
}
