package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

import sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

func TestIntegrationSearchVector_FileBackedNearest(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	s := openIntegrationStore(t, ctx)
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	seedIntegrationCorpus(t, ctx, s)
	requireNativeVectorQuery(t, ctx, s)

	results, err := s.SearchVector(ctx, makeVec1536(1, 0), "issue/1", 3)
	if err != nil {
		t.Fatalf("SearchVector() error = %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("SearchVector() len = %d, want at least 2", len(results))
	}
	if results[0].ID != "issue/2" {
		t.Fatalf("SearchVector()[0].ID = %s, want issue/2", results[0].ID)
	}
	if results[1].ID != "issue/3" {
		t.Fatalf("SearchVector()[1].ID = %s, want issue/3", results[1].ID)
	}
	if !(results[0].VecScore > results[1].VecScore) {
		t.Fatalf("expected first result score to be greater than second: %f <= %f", results[0].VecScore, results[1].VecScore)
	}
	for _, r := range results {
		if r.ID == "issue/1" {
			t.Fatalf("self-match should be excluded: %+v", r)
		}
		if r.VecScore < 0 || r.VecScore > 1 {
			t.Fatalf("VecScore out of range for %s: %f", r.ID, r.VecScore)
		}
	}
}

func TestIntegrationSearchVector_ClampsOppositeScore(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	s := openIntegrationStore(t, ctx)
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	seedIntegrationCorpus(t, ctx, s)
	requireNativeVectorQuery(t, ctx, s)

	results, err := s.SearchVector(ctx, makeVec1536(1, 0), "", 10)
	if err != nil {
		t.Fatalf("SearchVector() error = %v", err)
	}

	foundOpposite := false
	for _, r := range results {
		if r.ID != "issue/4" {
			continue
		}
		foundOpposite = true
		if r.VecScore != 0 {
			t.Fatalf("opposite vector VecScore = %f, want 0", r.VecScore)
		}
	}

	if !foundOpposite {
		t.Fatalf("opposite vector item issue/4 not present in result set")
	}
}

func TestIntegrationMigrations_FromV1Fixture(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fixture-v1.db")

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	fixturePath := filepath.Join("testdata", "v1_fixture.sql")
	fixtureSQL, err := os.ReadFile(fixturePath)
	if err != nil {
		_ = rawDB.Close()
		t.Fatalf("ReadFile(%s) error = %v", fixturePath, err)
	}

	if _, err := rawDB.ExecContext(ctx, string(fixtureSQL)); err != nil {
		_ = rawDB.Close()
		t.Fatalf("ExecContext(fixture) error = %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("rawDB.Close() error = %v", err)
	}

	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	requireObjectExists(t, s.DB(), "table", "items")
	requireObjectExists(t, s.DB(), "table", "items_fts")
	requireObjectExists(t, s.DB(), "table", "items_vec")
	requireObjectExists(t, s.DB(), "trigger", "items_fts_insert")
	requireObjectExists(t, s.DB(), "trigger", "items_fts_delete")
	requireObjectExists(t, s.DB(), "trigger", "items_fts_update")

	version, err := currentSchemaVersion(ctx, s.DB())
	if err != nil {
		t.Fatalf("currentSchemaVersion() error = %v", err)
	}
	if version != LatestSchemaVersion() {
		t.Fatalf("schema version = %d, want %d", version, LatestSchemaVersion())
	}
}

func openIntegrationStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "integration-index.db")
	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return s
}

func seedIntegrationCorpus(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()

	fixtures := []struct {
		id     string
		number int
		title  string
		vector []float32
	}{
		{id: "issue/1", number: 1, title: "self", vector: makeVec1536(1, 0)},
		{id: "issue/2", number: 2, title: "near", vector: makeVec1536(0.99, 0.01)},
		{id: "issue/3", number: 3, title: "far", vector: makeVec1536(0, 1)},
		{id: "issue/4", number: 4, title: "opposite", vector: makeVec1536(-1, 0)},
	}

	for _, fx := range fixtures {
		if err := s.UpsertItem(ctx, ItemRecord{
			ID:     fx.id,
			Type:   "issue",
			Number: fx.number,
			Title:  fx.title,
			Body:   fx.title,
			Author: "integration-test",
			State:  "open",
			URL:    "https://example.com/" + fx.id,
		}); err != nil {
			t.Fatalf("UpsertItem(%s) error = %v", fx.id, err)
		}
		if err := s.UpsertVector(ctx, fx.id, fx.vector); err != nil {
			t.Fatalf("UpsertVector(%s) error = %v", fx.id, err)
		}
	}
}

func requireNativeVectorQuery(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()

	serialized, err := sqlite_vec.SerializeFloat32(makeVec1536(1, 0))
	if err != nil {
		t.Fatalf("SerializeFloat32() error = %v", err)
	}

	rows, err := s.DB().QueryContext(ctx, `
SELECT id, distance
FROM items_vec
WHERE embedding MATCH ? AND k = ?;
`, serialized, 1)
	if err != nil {
		t.Skipf("skipping integration test: sqlite-vec native query unavailable (%v)", err)
		return
	}
	defer rows.Close()

	if err := rows.Err(); err != nil {
		t.Skipf("skipping integration test: sqlite-vec row iteration error (%v)", err)
	}
}
