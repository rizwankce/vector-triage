package respond

import (
	"strings"
	"testing"

	gh "vector-triage/internal/github"
	"vector-triage/internal/store"
)

func TestFormatter_NoResultsReturnsEmpty(t *testing.T) {
	t.Helper()
	f := Formatter{}
	got := f.Format(gh.Event{}, nil)
	if got != "" {
		t.Fatalf("Format() = %q, want empty", got)
	}
}

func TestFormatter_MarkerFirstLine(t *testing.T) {
	t.Helper()
	f := Formatter{}
	got := f.Format(gh.Event{}, []store.FusedResult{{Number: 1, Title: "Login", DisplaySimilarity: 0.82, State: "open"}})
	lines := strings.Split(got, "\n")
	if len(lines) == 0 || lines[0] != gh.CommentMarker {
		t.Fatalf("first line = %q, want marker %q", lines[0], gh.CommentMarker)
	}
}

func TestFormatter_DuplicateAndSimilarTable(t *testing.T) {
	t.Helper()
	f := Formatter{DuplicateThreshold: 0.92}
	got := f.Format(gh.Event{}, []store.FusedResult{
		{Number: 5, Title: "Fix login timeout", DisplaySimilarity: 0.95, State: "open"},
		{Number: 9, Title: "Retry auth", DisplaySimilarity: 0.81, State: "closed"},
	})

	if !strings.Contains(got, "Possible duplicate") {
		t.Fatalf("missing duplicate warning:\n%s", got)
	}
	if !strings.Contains(got, "#5") {
		t.Fatalf("missing duplicate reference:\n%s", got)
	}
	if !strings.Contains(got, "📋 Similar items found (2)") {
		t.Fatalf("missing similar table summary:\n%s", got)
	}
	if !strings.Contains(got, "95%") || !strings.Contains(got, "81%") {
		t.Fatalf("missing expected percentages:\n%s", got)
	}
	if !strings.Contains(got, "🟢 open") || !strings.Contains(got, "⚫ closed") {
		t.Fatalf("missing status icons:\n%s", got)
	}
}

func TestFormatter_RoundsPercentAndSupportsMerged(t *testing.T) {
	t.Helper()
	f := Formatter{DuplicateThreshold: 0.99}
	got := f.Format(gh.Event{}, []store.FusedResult{{Number: 7, Title: "Merge path", DisplaySimilarity: 0.9234, State: "merged"}})
	if !strings.Contains(got, "92%") {
		t.Fatalf("expected rounded percentage:\n%s", got)
	}
	if !strings.Contains(got, "🟣 merged") {
		t.Fatalf("expected merged icon:\n%s", got)
	}
}
