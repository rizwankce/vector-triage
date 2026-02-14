package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildItemID(t *testing.T) {
	t.Helper()

	tests := []struct {
		name   string
		kind   string
		number int
		want   string
	}{
		{name: "issue", kind: "issue", number: 1, want: "issue/1"},
		{name: "pr alias", kind: "pr", number: 2, want: "pr/2"},
		{name: "pull request", kind: "pull_request", number: 3, want: "pr/3"},
		{name: "pull request target", kind: "pull_request_target", number: 4, want: "pr/4"},
		{name: "fallback", kind: "discussion", number: 5, want: "discussion/5"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			got := BuildItemID(tt.kind, tt.number)
			if got != tt.want {
				t.Fatalf("BuildItemID(%q, %d) = %q, want %q", tt.kind, tt.number, got, tt.want)
			}
		})
	}
}

func TestUpsertItemValidation(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	rec := ItemRecord{ID: "issue/1", Type: "issue", Number: 1}

	var nilStore *Store
	if err := nilStore.UpsertItem(ctx, rec); err == nil {
		t.Fatalf("expected nil store validation error")
	}

	s := &Store{}
	if err := s.UpsertItem(ctx, rec); err == nil {
		t.Fatalf("expected uninitialized store validation error")
	}

	dbPath := filepath.Join(t.TempDir(), "item-validation.db")
	opened, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := opened.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	if err := opened.UpsertItem(ctx, ItemRecord{Type: "issue", Number: 1}); err == nil {
		t.Fatalf("expected missing ID validation error")
	}
	if err := opened.UpsertItem(ctx, ItemRecord{ID: "issue/1", Number: 1}); err == nil {
		t.Fatalf("expected missing type validation error")
	}
	if err := opened.UpsertItem(ctx, ItemRecord{ID: "issue/1", Type: "issue", Number: 0}); err == nil {
		t.Fatalf("expected non-positive number validation error")
	}
}

func TestUpsertItemInsertAndUpdate(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "item-upsert.db")
	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	if err := s.UpsertItem(ctx, ItemRecord{
		ID:        "issue/42",
		Type:      "issue",
		Number:    42,
		Title:     "Original title",
		Body:      "Original body",
		Author:    "alice",
		State:     "open",
		Labels:    []string{"bug"},
		Files:     []string{},
		URL:       "https://example.com/issue/42",
		CreatedAt: time.Date(2026, 1, 1, 1, 1, 1, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 1, 1, 2, 0, time.UTC),
	}); err != nil {
		t.Fatalf("first UpsertItem() error = %v", err)
	}

	if err := s.UpsertItem(ctx, ItemRecord{
		ID:        "issue/42",
		Type:      "issue",
		Number:    42,
		Title:     "Updated title",
		Body:      "Updated body",
		Author:    "bob",
		State:     "closed",
		Labels:    []string{"bug", "high"},
		Files:     []string{},
		URL:       "https://example.com/issue/42",
		UpdatedAt: time.Date(2026, 1, 1, 1, 2, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("second UpsertItem() error = %v", err)
	}

	var (
		title     string
		state     string
		createdAt string
		updatedAt string
	)
	if err := s.DB().QueryRowContext(ctx, `
SELECT title, state, created_at, updated_at
FROM items
WHERE id = ?;
`, "issue/42").Scan(&title, &state, &createdAt, &updatedAt); err != nil {
		t.Fatalf("query item row error = %v", err)
	}

	if title != "Updated title" {
		t.Fatalf("title = %q, want %q", title, "Updated title")
	}
	if state != "closed" {
		t.Fatalf("state = %q, want %q", state, "closed")
	}
	if createdAt != "2026-01-01T01:01:01Z" {
		t.Fatalf("created_at = %q, want first insert timestamp", createdAt)
	}
	if updatedAt != "2026-01-01T01:02:00Z" {
		t.Fatalf("updated_at = %q, want second upsert timestamp", updatedAt)
	}
}

func TestUpsertVectorValidationAndReplace(t *testing.T) {
	t.Helper()

	ctx := context.Background()

	var nilStore *Store
	if err := nilStore.UpsertVector(ctx, "issue/1", makeVec1536(1, 0)); err == nil {
		t.Fatalf("expected nil store validation error")
	}

	s := &Store{}
	if err := s.UpsertVector(ctx, "issue/1", makeVec1536(1, 0)); err == nil {
		t.Fatalf("expected uninitialized store validation error")
	}

	dbPath := filepath.Join(t.TempDir(), "vector-upsert.db")
	opened, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := opened.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	if err := opened.UpsertVector(ctx, "", makeVec1536(1, 0)); err == nil {
		t.Fatalf("expected missing id validation error")
	}
	if err := opened.UpsertVector(ctx, "issue/1", nil); err == nil {
		t.Fatalf("expected empty vector validation error")
	}

	first := makeVec1536(1, 0, 0)
	second := makeVec1536(0, 1, 0)
	if err := opened.UpsertVector(ctx, "issue/1", first); err != nil {
		t.Fatalf("first UpsertVector() error = %v", err)
	}
	if err := opened.UpsertVector(ctx, "issue/1", second); err != nil {
		t.Fatalf("second UpsertVector() error = %v", err)
	}

	var blob []byte
	if err := opened.DB().QueryRowContext(ctx, `SELECT embedding FROM items_vec WHERE id = ?;`, "issue/1").Scan(&blob); err != nil {
		t.Fatalf("query embedding error = %v", err)
	}
	decoded, err := decodeFloat32Vector(blob)
	if err != nil {
		t.Fatalf("decodeFloat32Vector() error = %v", err)
	}
	if len(decoded) != 1536 {
		t.Fatalf("decoded len = %d, want 1536", len(decoded))
	}
	if decoded[0] != 0 || decoded[1] != 1 {
		t.Fatalf("decoded vector not replaced, got first two values [%f, %f]", decoded[0], decoded[1])
	}
}
