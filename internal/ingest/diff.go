package ingest

const MaxDiffChars = 4000

// TruncateDiff trims a diff summary to at most maxChars characters.
func TruncateDiff(diff string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}

	runes := []rune(diff)
	if len(runes) <= maxChars {
		return diff
	}

	return string(runes[:maxChars])
}
