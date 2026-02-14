package ingest

import (
	"strings"
	"testing"
)

func TestBuildIssueContent(t *testing.T) {
	t.Helper()

	tests := []struct {
		name  string
		title string
		body  string
		want  string
	}{
		{
			name:  "title and body",
			title: "Login timeout",
			body:  "App hangs after 30s.",
			want:  "Issue: Login timeout\n\nApp hangs after 30s.",
		},
		{
			name:  "title only",
			title: "Login timeout",
			body:  "",
			want:  "Issue: Login timeout",
		},
		{
			name:  "body only",
			title: "",
			body:  "App hangs after 30s.",
			want:  "App hangs after 30s.",
		},
		{
			name:  "empty",
			title: "   ",
			body:  "",
			want:  "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := BuildIssueContent(tt.title, tt.body)
			if got != tt.want {
				t.Fatalf("BuildIssueContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateDiff(t *testing.T) {
	t.Helper()

	short := "abc"
	if got := TruncateDiff(short, MaxDiffChars); got != short {
		t.Fatalf("TruncateDiff(short) = %q, want %q", got, short)
	}

	long := strings.Repeat("x", MaxDiffChars+50)
	truncated := TruncateDiff(long, MaxDiffChars)
	if len([]rune(truncated)) != MaxDiffChars {
		t.Fatalf("TruncateDiff length = %d, want %d", len([]rune(truncated)), MaxDiffChars)
	}
	if truncated != long[:MaxDiffChars] {
		t.Fatalf("TruncateDiff prefix mismatch")
	}

	if got := TruncateDiff("abc", 0); got != "" {
		t.Fatalf("TruncateDiff(max=0) = %q, want empty", got)
	}
}

func TestBuildPRContent_Full(t *testing.T) {
	t.Helper()

	diff := strings.Repeat("d", MaxDiffChars+25)
	got := BuildPRContent(PRInput{
		Title: "Add login retries",
		Body:  "Retries when auth times out.",
		Files: []string{"src/auth/login.go", " README.md ", ""},
		Diff:  diff,
		Mode:  PRDiffModeInclude,
	})

	if !strings.Contains(got, "PR: Add login retries") {
		t.Fatalf("missing PR title section: %q", got)
	}
	if !strings.Contains(got, "Description: Retries when auth times out.") {
		t.Fatalf("missing description section: %q", got)
	}
	if !strings.Contains(got, "Files changed: src/auth/login.go, README.md") {
		t.Fatalf("missing files section: %q", got)
	}

	diffPrefix := "Diff summary: " + strings.Repeat("d", MaxDiffChars)
	if !strings.Contains(got, diffPrefix) {
		t.Fatalf("missing truncated diff section")
	}
	if strings.Contains(got, strings.Repeat("d", MaxDiffChars+1)) {
		t.Fatalf("diff was not truncated to %d chars", MaxDiffChars)
	}
}

func TestBuildPRContent_ModeFallbacks(t *testing.T) {
	t.Helper()

	withFilesNoDiff := BuildPRContent(PRInput{
		Title: "Add login retries",
		Body:  "Retries when auth times out.",
		Files: []string{"src/auth/login.go"},
		Diff:  "some diff",
		Mode:  PRDiffModeSkipDiffKeepFiles,
	})
	if !strings.Contains(withFilesNoDiff, "Files changed: src/auth/login.go") {
		t.Fatalf("expected files section in skip-diff mode")
	}
	if strings.Contains(withFilesNoDiff, "Diff summary:") {
		t.Fatalf("did not expect diff section in skip-diff mode")
	}

	titleBodyOnly := BuildPRContent(PRInput{
		Title: "Add login retries",
		Body:  "Retries when auth times out.",
		Files: []string{"src/auth/login.go"},
		Diff:  "some diff",
		Mode:  PRDiffModeTitleBodyOnly,
	})
	if strings.Contains(titleBodyOnly, "Files changed:") {
		t.Fatalf("did not expect files section in title/body-only mode")
	}
	if strings.Contains(titleBodyOnly, "Diff summary:") {
		t.Fatalf("did not expect diff section in title/body-only mode")
	}
}

func TestBuildPRContent_EmptyTitleBodyRules(t *testing.T) {
	t.Helper()

	titleOnly := BuildPRContent(PRInput{Title: "Only title", Body: ""})
	if titleOnly != "PR: Only title" {
		t.Fatalf("title-only result = %q", titleOnly)
	}

	bodyOnly := BuildPRContent(PRInput{Title: "", Body: "Only body", Files: []string{"a.go"}, Mode: PRDiffModeInclude})
	if bodyOnly != "Only body" {
		t.Fatalf("body-only result = %q", bodyOnly)
	}

	empty := BuildPRContent(PRInput{Title: "", Body: ""})
	if empty != "" {
		t.Fatalf("empty result = %q", empty)
	}
}
