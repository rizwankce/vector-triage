package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type ItemRecord struct {
	ID     string
	Type   string
	Number int
	Title  string
	Body   string
	Author string
	State  string
	Labels []string
	Files  []string
	URL    string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func BuildItemID(kind string, number int) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	switch kind {
	case "issue":
		return fmt.Sprintf("issue/%d", number)
	case "pr", "pull_request", "pull_request_target":
		return fmt.Sprintf("pr/%d", number)
	default:
		return fmt.Sprintf("%s/%d", kind, number)
	}
}

func (s *Store) UpsertItem(ctx context.Context, rec ItemRecord) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}
	if strings.TrimSpace(rec.ID) == "" {
		return errors.New("item id is required")
	}
	if strings.TrimSpace(rec.Type) == "" {
		return errors.New("item type is required")
	}
	if rec.Number <= 0 {
		return errors.New("item number must be positive")
	}

	labelsJSON, err := json.Marshal(rec.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	filesJSON, err := json.Marshal(rec.Files)
	if err != nil {
		return fmt.Errorf("marshal files: %w", err)
	}

	now := time.Now().UTC()
	createdAt := rec.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := rec.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}

	const stmt = `
INSERT INTO items(
    id, type, number, title, body, author, state, labels, files, url, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    type=excluded.type,
    number=excluded.number,
    title=excluded.title,
    body=excluded.body,
    author=excluded.author,
    state=excluded.state,
    labels=excluded.labels,
    files=excluded.files,
    url=excluded.url,
    updated_at=excluded.updated_at;
`
	_, err = s.db.ExecContext(ctx, stmt,
		rec.ID,
		rec.Type,
		rec.Number,
		rec.Title,
		rec.Body,
		rec.Author,
		rec.State,
		string(labelsJSON),
		string(filesJSON),
		rec.URL,
		createdAt.Format(time.RFC3339Nano),
		updatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert item: %w", err)
	}
	return nil
}

func (s *Store) UpsertVector(ctx context.Context, id string, embedding []float32) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("item id is required")
	}
	if len(embedding) == 0 {
		return errors.New("embedding is required")
	}

	serialized, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("serialize embedding: %w", err)
	}

	const upsertStmt = `INSERT OR REPLACE INTO items_vec(id, embedding) VALUES(?, ?);`
	if _, err := s.db.ExecContext(ctx, upsertStmt, id, serialized); err == nil {
		return nil
	} else if !strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
		return fmt.Errorf("upsert vector: %w", err)
	}

	// Some sqlite-vec builds can reject INSERT OR REPLACE on existing IDs.
	// Fallback to delete + insert to keep updates deterministic.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM items_vec WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("upsert vector delete existing: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO items_vec(id, embedding) VALUES(?, ?);`, id, serialized); err != nil {
		return fmt.Errorf("upsert vector insert: %w", err)
	}
	return nil
}
