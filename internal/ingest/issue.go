package ingest

import "strings"

// BuildIssueContent converts issue title/body into embeddable text.
func BuildIssueContent(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)

	switch {
	case title != "" && body != "":
		return "Issue: " + title + "\n\n" + body
	case title != "":
		return "Issue: " + title
	case body != "":
		return body
	default:
		return ""
	}
}
