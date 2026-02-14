package ingest

import "strings"

type PRDiffMode int

const (
	// PRDiffModeInclude keeps both files and truncated diff in the embedding payload.
	PRDiffModeInclude PRDiffMode = iota
	// PRDiffModeSkipDiffKeepFiles is used when diff is too large/unavailable but files are available.
	PRDiffModeSkipDiffKeepFiles
	// PRDiffModeTitleBodyOnly is used when diff API fails and only title/body should be embedded.
	PRDiffModeTitleBodyOnly
)

type PRInput struct {
	Title string
	Body  string
	Files []string
	Diff  string
	Mode  PRDiffMode
}

// BuildPRContent converts PR fields into embeddable text.
func BuildPRContent(in PRInput) string {
	title := strings.TrimSpace(in.Title)
	body := strings.TrimSpace(in.Body)

	// Empty title/body handling follows spec edge-case rules.
	switch {
	case title == "" && body == "":
		return ""
	case title == "":
		return body
	case body == "":
		return "PR: " + title
	}

	parts := []string{
		"PR: " + title,
		"Description: " + body,
	}

	if in.Mode != PRDiffModeTitleBodyOnly {
		files := normalizeFiles(in.Files)
		if len(files) > 0 {
			parts = append(parts, "Files changed: "+strings.Join(files, ", "))
		}
	}

	if in.Mode == PRDiffModeInclude {
		diff := strings.TrimSpace(in.Diff)
		if diff != "" {
			parts = append(parts, "Diff summary: "+TruncateDiff(diff, MaxDiffChars))
		}
	}

	return strings.Join(parts, "\n\n")
}

func normalizeFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		trimmed := strings.TrimSpace(file)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
