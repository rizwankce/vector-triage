package store

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

func TestBuildFTS5Query(t *testing.T) {
	t.Helper()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "basic terms",
			input: "fix login timeout",
			want:  `"fix" AND "login" AND "timeout"`,
		},
		{
			name:  "stop words removed",
			input: "the fix in the app",
			want:  `"fix" AND "app"`,
		},
		{
			name:  "special chars stripped",
			input: `fix (login)* timeout:"api"`,
			want:  `"fix" AND "login" AND "timeout" AND "api"`,
		},
		{
			name:  "empty after cleanup",
			input: "the in and to",
			want:  "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			got := buildFTS5Query(tt.input)
			if got != tt.want {
				t.Fatalf("buildFTS5Query(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeBM25(t *testing.T) {
	t.Helper()

	strong := normalizeBM25(-10.0)
	medium := normalizeBM25(-2.0)
	weak := normalizeBM25(-0.5)
	none := normalizeBM25(0.0)

	assertAlmostEqual(t, strong, 10.0/11.0, 1e-9)
	assertAlmostEqual(t, medium, 2.0/3.0, 1e-9)
	assertAlmostEqual(t, weak, 1.0/3.0, 1e-9)
	assertAlmostEqual(t, none, 0.0, 1e-9)

	if !(strong > medium && medium > weak && weak > none) {
		t.Fatalf("normalizeBM25 monotonicity violated: strong=%f medium=%f weak=%f none=%f", strong, medium, weak, none)
	}
}

func TestSearchFTS_ExcludesCurrentItem(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fts-exclude.db")
	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	if err := insertItemFixture(ctx, s, "issue/1", "issue", 1, "Fix login timeout"); err != nil {
		t.Fatalf("insert item issue/1 error = %v", err)
	}
	if err := insertItemFixture(ctx, s, "issue/2", "issue", 2, "Fix login timeout on mobile"); err != nil {
		t.Fatalf("insert item issue/2 error = %v", err)
	}
	if err := insertItemFixture(ctx, s, "issue/3", "issue", 3, "Completely unrelated text"); err != nil {
		t.Fatalf("insert item issue/3 error = %v", err)
	}

	results, err := s.SearchFTS(ctx, "fix login timeout", "issue/1", 5)
	if err != nil {
		t.Fatalf("SearchFTS() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("SearchFTS() returned no results")
	}
	for _, r := range results {
		if r.ID == "issue/1" {
			t.Fatalf("SearchFTS() returned excluded ID: %+v", r)
		}
		if r.FTSScore < 0 || r.FTSScore > 1 {
			t.Fatalf("FTSScore out of range: %+v", r)
		}
	}
}

func TestSearchFTS_EmptyQueryReturnsNoResults(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fts-empty.db")
	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
	}()

	results, err := s.SearchFTS(ctx, "the and in to", "", 5)
	if err != nil {
		t.Fatalf("SearchFTS() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("SearchFTS() len = %d, want 0", len(results))
	}
}

func assertAlmostEqual(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("value mismatch: got=%f want=%f tolerance=%f", got, want, tolerance)
	}
}
