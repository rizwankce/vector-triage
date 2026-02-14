package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// VectorResult represents one vector similarity hit plus basic metadata.
type VectorResult struct {
	ID       string
	Type     string
	Number   int
	Title    string
	State    string
	URL      string
	Distance float64
	VecScore float64
}

type vectorHit struct {
	ID       string
	Distance float64
}

func (s *Store) SearchVector(ctx context.Context, queryEmbedding []float32, excludeID string, limit int) ([]VectorResult, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("store is not initialized")
	}

	if len(queryEmbedding) == 0 || limit <= 0 {
		return []VectorResult{}, nil
	}

	candidateLimit := limit * 3
	if candidateLimit < 1 {
		candidateLimit = 1
	}

	hits, err := s.vectorOnlySearch(ctx, queryEmbedding, candidateLimit)
	if err != nil {
		return nil, err
	}

	results := make([]VectorResult, 0, limit)
	for _, hit := range hits {
		if hit.ID == excludeID {
			continue
		}

		item, err := s.lookupItemMeta(ctx, hit.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}

		results = append(results, VectorResult{
			ID:       item.ID,
			Type:     item.Type,
			Number:   item.Number,
			Title:    item.Title,
			State:    item.State,
			URL:      item.URL,
			Distance: hit.Distance,
			VecScore: clamp01(1.0 - hit.Distance),
		})

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func (s *Store) vectorOnlySearch(ctx context.Context, queryEmbedding []float32, candidateLimit int) ([]vectorHit, error) {
	serialized, err := sqlite_vec.SerializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("serialize query embedding: %w", err)
	}

	const sqliteVecQuery = `
SELECT id, distance
FROM items_vec
WHERE embedding MATCH ? AND k = ?;
`

	rows, err := s.db.QueryContext(ctx, sqliteVecQuery, serialized, candidateLimit)
	if err == nil {
		defer rows.Close()
		hits, scanErr := scanDistanceRows(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		return hits, nil
	}

	if !shouldFallbackToBruteForce(err) {
		return nil, fmt.Errorf("vector query failed: %w", err)
	}

	return s.vectorOnlySearchBruteForce(ctx, queryEmbedding, candidateLimit)
}

func scanDistanceRows(rows *sql.Rows) ([]vectorHit, error) {
	hits := make([]vectorHit, 0)
	for rows.Next() {
		var hit vectorHit
		if err := rows.Scan(&hit.ID, &hit.Distance); err != nil {
			return nil, fmt.Errorf("scan vector row: %w", err)
		}
		hits = append(hits, hit)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vector rows: %w", err)
	}

	return hits, nil
}

func (s *Store) vectorOnlySearchBruteForce(ctx context.Context, queryEmbedding []float32, candidateLimit int) ([]vectorHit, error) {
	const query = `
SELECT id, embedding
FROM items_vec;
`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fallback vector query failed: %w", err)
	}
	defer rows.Close()

	hits := make([]vectorHit, 0)
	for rows.Next() {
		var id string
		var embeddingBlob []byte
		if err := rows.Scan(&id, &embeddingBlob); err != nil {
			return nil, fmt.Errorf("scan fallback vector row: %w", err)
		}

		candidate, err := decodeFloat32Vector(embeddingBlob)
		if err != nil {
			continue
		}

		hits = append(hits, vectorHit{
			ID:       id,
			Distance: cosineDistance(queryEmbedding, candidate),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fallback vector rows: %w", err)
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Distance == hits[j].Distance {
			return hits[i].ID < hits[j].ID
		}
		return hits[i].Distance < hits[j].Distance
	})

	if len(hits) > candidateLimit {
		hits = hits[:candidateLimit]
	}

	return hits, nil
}

type itemMeta struct {
	ID     string
	Type   string
	Number int
	Title  string
	State  string
	URL    string
}

func (s *Store) lookupItemMeta(ctx context.Context, id string) (itemMeta, error) {
	const query = `
SELECT id, type, number, title, state, url
FROM items
WHERE id = ?;
`

	var out itemMeta
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&out.ID,
		&out.Type,
		&out.Number,
		&out.Title,
		&out.State,
		&out.URL,
	)
	if err != nil {
		return itemMeta{}, err
	}

	return out, nil
}

func shouldFallbackToBruteForce(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	fallbackSignals := []string{
		"no such module: vec0",
		"no such column: distance",
		"no such column: k",
		"unable to use function match",
		"no such function: vec_distance",
	}
	for _, signal := range fallbackSignals {
		if strings.Contains(msg, signal) {
			return true
		}
	}
	return false
}

func decodeFloat32Vector(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("invalid float32 vector byte length: %d", len(blob))
	}

	out := make([]float32, len(blob)/4)
	for i := range out {
		bits := uint32(blob[i*4]) |
			uint32(blob[i*4+1])<<8 |
			uint32(blob[i*4+2])<<16 |
			uint32(blob[i*4+3])<<24
		out[i] = math.Float32frombits(bits)
	}

	return out, nil
}

func cosineDistance(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 2.0
	}

	var dot float64
	var normA float64
	var normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}

	if normA == 0 || normB == 0 {
		return 1.0
	}

	similarity := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if similarity > 1.0 {
		similarity = 1.0
	}
	if similarity < -1.0 {
		similarity = -1.0
	}

	return 1.0 - similarity
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
