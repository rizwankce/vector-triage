package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// FTSResult represents one keyword search hit plus normalized relevance.
type FTSResult struct {
	ID       string
	Type     string
	Number   int
	Title    string
	State    string
	URL      string
	RawBM25  float64
	FTSScore float64
}

var ftsStopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {}, "for": {}, "from": {},
	"has": {}, "he": {}, "in": {}, "is": {}, "it": {}, "its": {}, "of": {}, "on": {}, "or": {}, "that": {},
	"the": {}, "to": {}, "was": {}, "were": {}, "will": {}, "with": {}, "this": {}, "these": {}, "those": {},
}

func (s *Store) SearchFTS(ctx context.Context, query string, excludeID string, limit int) ([]FTSResult, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("store is not initialized")
	}

	if limit <= 0 {
		return []FTSResult{}, nil
	}

	terms := tokenizeFTSQuery(query)
	if len(terms) == 0 {
		return []FTSResult{}, nil
	}

	ftsQuery := buildFTS5Query(query)
	results, err := s.searchFTSNative(ctx, ftsQuery, excludeID, limit)
	if err == nil {
		return results, nil
	}

	if !shouldFallbackFTS(err) {
		return nil, fmt.Errorf("fts query failed: %w", err)
	}

	return s.searchFTSFallback(ctx, terms, excludeID, limit)
}

func buildFTS5Query(input string) string {
	terms := tokenizeFTSQuery(input)
	if len(terms) == 0 {
		return ""
	}

	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		escaped := strings.ReplaceAll(term, `"`, `""`)
		parts = append(parts, `"`+escaped+`"`)
	}

	return strings.Join(parts, " AND ")
}

func normalizeBM25(rawBM25 float64) float64 {
	abs := math.Abs(rawBM25)
	return abs / (1.0 + abs)
}

func (s *Store) searchFTSNative(ctx context.Context, ftsQuery, excludeID string, limit int) ([]FTSResult, error) {
	const query = `
SELECT
    i.id, i.type, i.number, i.title, i.state, i.url,
    bm25(items_fts, 10.0, 1.0) AS score
FROM items_fts f
JOIN items i ON i.rowid = f.rowid
WHERE items_fts MATCH ?
  AND i.id != ?
ORDER BY score ASC
LIMIT ?;
`

	rows, err := s.db.QueryContext(ctx, query, ftsQuery, excludeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]FTSResult, 0, limit)
	for rows.Next() {
		var out FTSResult
		if err := rows.Scan(
			&out.ID,
			&out.Type,
			&out.Number,
			&out.Title,
			&out.State,
			&out.URL,
			&out.RawBM25,
		); err != nil {
			return nil, fmt.Errorf("scan fts row: %w", err)
		}

		out.FTSScore = normalizeBM25(out.RawBM25)
		results = append(results, out)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fts rows: %w", err)
	}

	return results, nil
}

type fallbackFTSRow struct {
	ID        string
	Type      string
	Number    int
	Title     string
	State     string
	URL       string
	LowerText string
}

func (s *Store) searchFTSFallback(ctx context.Context, terms []string, excludeID string, limit int) ([]FTSResult, error) {
	candidateLimit := limit * 3
	if candidateLimit < 1 {
		candidateLimit = 1
	}

	var b strings.Builder
	b.WriteString(`
SELECT id, type, number, title, state, url, lower(title || ' ' || body) as text_blob
FROM items
WHERE id != ?`)

	args := make([]any, 0, 1+len(terms)*2+1)
	args = append(args, excludeID)

	for _, term := range terms {
		b.WriteString(`
  AND (lower(title) LIKE ? OR lower(body) LIKE ?)`)
		pattern := "%" + term + "%"
		args = append(args, pattern, pattern)
	}

	b.WriteString(`
LIMIT ?;`)
	args = append(args, candidateLimit)

	rows, err := s.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("fallback fts query failed: %w", err)
	}
	defer rows.Close()

	rawRows := make([]fallbackFTSRow, 0, candidateLimit)
	for rows.Next() {
		var row fallbackFTSRow
		if err := rows.Scan(&row.ID, &row.Type, &row.Number, &row.Title, &row.State, &row.URL, &row.LowerText); err != nil {
			return nil, fmt.Errorf("scan fallback fts row: %w", err)
		}
		rawRows = append(rawRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fallback fts rows: %w", err)
	}

	sort.Slice(rawRows, func(i, j int) bool {
		iScore := fallbackTermFrequency(rawRows[i].LowerText, terms)
		jScore := fallbackTermFrequency(rawRows[j].LowerText, terms)
		if iScore == jScore {
			return rawRows[i].ID < rawRows[j].ID
		}
		return iScore > jScore
	})

	if len(rawRows) > limit {
		rawRows = rawRows[:limit]
	}

	results := make([]FTSResult, 0, len(rawRows))
	for _, row := range rawRows {
		raw := -float64(fallbackTermFrequency(row.LowerText, terms))
		results = append(results, FTSResult{
			ID:       row.ID,
			Type:     row.Type,
			Number:   row.Number,
			Title:    row.Title,
			State:    row.State,
			URL:      row.URL,
			RawBM25:  raw,
			FTSScore: normalizeBM25(raw),
		})
	}

	return results, nil
}

func tokenizeFTSQuery(input string) []string {
	words := splitWords(input)
	terms := make([]string, 0, len(words))
	for _, word := range words {
		if _, isStopWord := ftsStopWords[word]; isStopWord {
			continue
		}
		terms = append(terms, word)
	}
	return terms
}

func splitWords(input string) []string {
	var b strings.Builder
	words := make([]string, 0)

	flush := func() {
		if b.Len() == 0 {
			return
		}
		words = append(words, b.String())
		b.Reset()
	}

	for _, r := range strings.ToLower(input) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	return words
}

func fallbackTermFrequency(text string, terms []string) int {
	score := 0
	for _, term := range terms {
		score += strings.Count(text, term)
	}
	return score
}

func shouldFallbackFTS(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	fallbackSignals := []string{
		"no such function: bm25",
		"no such module: fts5",
		"unable to use function match",
		"no such table: items_fts",
		"no such column: items_fts",
		"no such column: f",
	}
	for _, signal := range fallbackSignals {
		if strings.Contains(msg, signal) {
			return true
		}
	}

	return false
}
