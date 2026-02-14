package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func TestSearchVector_ExcludesCurrentItemAndUsesTwoStepLookup(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vector-search.db")
	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	if err := insertItemFixture(ctx, s, "issue/1", "issue", 1, "self"); err != nil {
		t.Fatalf("insertItemFixture issue/1 error = %v", err)
	}
	if err := insertItemFixture(ctx, s, "issue/2", "issue", 2, "near"); err != nil {
		t.Fatalf("insertItemFixture issue/2 error = %v", err)
	}
	if err := insertItemFixture(ctx, s, "issue/3", "issue", 3, "far"); err != nil {
		t.Fatalf("insertItemFixture issue/3 error = %v", err)
	}
	if err := insertVectorFixture(ctx, s, "issue/1", makeVec1536(1, 0)); err != nil {
		t.Fatalf("insertVectorFixture issue/1 error = %v", err)
	}
	if err := insertVectorFixture(ctx, s, "issue/2", makeVec1536(0.99, 0.01)); err != nil {
		t.Fatalf("insertVectorFixture issue/2 error = %v", err)
	}
	if err := insertVectorFixture(ctx, s, "issue/3", makeVec1536(0, 1)); err != nil {
		t.Fatalf("insertVectorFixture issue/3 error = %v", err)
	}

	results, err := s.SearchVector(ctx, makeVec1536(1, 0), "issue/1", 2)
	if err != nil {
		t.Fatalf("SearchVector() error = %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("SearchVector() returned no results")
	}
	if results[0].ID == "issue/1" {
		t.Fatalf("self item returned as top result")
	}
	for _, r := range results {
		if r.ID == "issue/1" {
			t.Fatalf("self item should be excluded: %+v", r)
		}
	}
}

func TestSearchVector_ClampsVecScoreToUnitInterval(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vector-score.db")
	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	if err := insertItemFixture(ctx, s, "issue/neg", "issue", 10, "opposite"); err != nil {
		t.Fatalf("insertItemFixture issue/neg error = %v", err)
	}
	if err := insertVectorFixture(ctx, s, "issue/neg", makeVec1536(-1, 0)); err != nil {
		t.Fatalf("insertVectorFixture issue/neg error = %v", err)
	}

	results, err := s.SearchVector(ctx, makeVec1536(1, 0), "", 1)
	if err != nil {
		t.Fatalf("SearchVector() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchVector() len = %d, want 1", len(results))
	}
	if results[0].VecScore < 0 || results[0].VecScore > 1 {
		t.Fatalf("VecScore out of range: %f", results[0].VecScore)
	}
	if results[0].VecScore != 0 {
		t.Fatalf("VecScore = %f, want 0 for opposite vectors", results[0].VecScore)
	}
}

func TestSearchVector_SkipsVectorHitsMissingMetadata(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vector-missing-meta.db")
	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	if err := insertItemFixture(ctx, s, "issue/ok", "issue", 20, "kept"); err != nil {
		t.Fatalf("insertItemFixture issue/ok error = %v", err)
	}
	if err := insertVectorFixture(ctx, s, "issue/missing", makeVec1536(1, 0)); err != nil {
		t.Fatalf("insertVectorFixture issue/missing error = %v", err)
	}
	if err := insertVectorFixture(ctx, s, "issue/ok", makeVec1536(0.5, 0.5)); err != nil {
		t.Fatalf("insertVectorFixture issue/ok error = %v", err)
	}

	results, err := s.SearchVector(ctx, makeVec1536(1, 0), "", 2)
	if err != nil {
		t.Fatalf("SearchVector() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchVector() len = %d, want 1", len(results))
	}
	if results[0].ID != "issue/ok" {
		t.Fatalf("SearchVector() ID = %s, want issue/ok", results[0].ID)
	}
}

func insertItemFixture(ctx context.Context, s *Store, id, typ string, number int, title string) error {
	const stmt = `
INSERT INTO items(
    id, type, number, title, body, author, state, labels, files, url, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB().ExecContext(ctx, stmt,
		id, typ, number, title, "body", "tester", "open", "[]", "[]",
		"https://example.com/"+id, now, now,
	)
	return err
}

func insertVectorFixture(ctx context.Context, s *Store, id string, vector []float32) error {
	serialized, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return err
	}

	const stmt = `INSERT INTO items_vec(id, embedding) VALUES(?, ?);`
	_, err = s.DB().ExecContext(ctx, stmt, id, serialized)
	return err
}

func makeVec1536(values ...float32) []float32 {
	out := make([]float32, 1536)
	copy(out, values)
	return out
}
