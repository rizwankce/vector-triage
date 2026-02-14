package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const latestSchemaVersion = 2

type migration struct {
	version int
	name    string
	up      func(context.Context, *sql.Tx) error
}

var migrations = []migration{
	{version: 1, name: "create_items", up: migrateV1},
	{version: 2, name: "create_search_tables", up: migrateV2},
}

func LatestSchemaVersion() int {
	return latestSchemaVersion
}

func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("nil db")
	}

	if err := ensureSchemaVersionTable(ctx, db); err != nil {
		return fmt.Errorf("ensure schema_version table: %w", err)
	}

	current, err := currentSchemaVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("read current schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}

		if err := applyMigration(ctx, db, m); err != nil {
			return fmt.Errorf("apply migration v%d (%s): %w", m.version, m.name, err)
		}
	}

	return nil
}

func ensureSchemaVersionTable(ctx context.Context, db *sql.DB) error {
	const stmt = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);
`

	_, err := db.ExecContext(ctx, stmt)
	return err
}

func currentSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	const stmt = `SELECT COALESCE(MAX(version), 0) FROM schema_version;`

	var version int
	if err := db.QueryRowContext(ctx, stmt).Scan(&version); err != nil {
		return 0, err
	}

	return version, nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = m.up(ctx, tx); err != nil {
		return err
	}

	const insertVersion = `
INSERT INTO schema_version (version, applied_at)
VALUES (?, ?);
`

	appliedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, insertVersion, m.version, appliedAt); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func migrateV1(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`
CREATE TABLE IF NOT EXISTS items (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    number INTEGER NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'open',
    labels TEXT NOT NULL DEFAULT '[]',
    files TEXT NOT NULL DEFAULT '[]',
    url TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
`,
		`CREATE INDEX IF NOT EXISTS idx_items_type ON items(type);`,
		`CREATE INDEX IF NOT EXISTS idx_items_number ON items(number);`,
		`CREATE INDEX IF NOT EXISTS idx_items_state ON items(state);`,
	}

	return execStatements(ctx, tx, stmts)
}

func migrateV2(ctx context.Context, tx *sql.Tx) error {
	if err := ensureFTSTable(ctx, tx); err != nil {
		return err
	}

	stmts := []string{
		`
CREATE TRIGGER IF NOT EXISTS items_fts_insert AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
`,
		`
CREATE TRIGGER IF NOT EXISTS items_fts_delete AFTER DELETE ON items BEGIN
    DELETE FROM items_fts WHERE rowid = old.rowid;
END;
`,
		`
CREATE TRIGGER IF NOT EXISTS items_fts_update AFTER UPDATE ON items BEGIN
    DELETE FROM items_fts WHERE rowid = old.rowid;
    INSERT INTO items_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
`,
	}

	if err := execStatements(ctx, tx, stmts); err != nil {
		return err
	}

	return ensureVectorTable(ctx, tx)
}

func ensureFTSTable(ctx context.Context, tx *sql.Tx) error {
	const ftsVirtualTable = `
CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
    title,
    body,
    content='items',
    content_rowid='rowid',
    tokenize='porter unicode61'
);
`

	if _, err := tx.ExecContext(ctx, ftsVirtualTable); err == nil {
		return nil
	} else if !isModuleUnavailable(err, "fts5") {
		return err
	}

	// Development fallback when FTS5 is unavailable in the local SQLite build.
	const ftsFallbackTable = `
CREATE TABLE IF NOT EXISTS items_fts (
    rowid INTEGER PRIMARY KEY,
    title TEXT,
    body TEXT
);
`

	_, err := tx.ExecContext(ctx, ftsFallbackTable)
	return err
}

func ensureVectorTable(ctx context.Context, tx *sql.Tx) error {
	const vectorVirtualTable = `
CREATE VIRTUAL TABLE IF NOT EXISTS items_vec USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[1536] distance_metric=cosine
);
`

	if _, err := tx.ExecContext(ctx, vectorVirtualTable); err == nil {
		return nil
	} else if !isModuleUnavailable(err, "vec0") {
		return err
	}

	// Development fallback until sqlite-vec is wired in WP-002.
	const vectorFallbackTable = `
CREATE TABLE IF NOT EXISTS items_vec (
    id TEXT PRIMARY KEY,
    embedding BLOB NOT NULL
);
`

	_, err := tx.ExecContext(ctx, vectorFallbackTable)
	return err
}

func execStatements(ctx context.Context, tx *sql.Tx, stmts []string) error {
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	return nil
}

func isModuleUnavailable(err error, module string) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	needle := "no such module: " + strings.ToLower(module)
	return strings.Contains(msg, needle)
}
