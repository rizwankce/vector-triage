package embed

import (
	"context"
	"errors"
)

// MockEmbedder is a test double with deterministic outputs.
type MockEmbedder struct {
	Vectors [][]float32
	Err     error
	Dims    int
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	_ = ctx
	if m.Err != nil {
		return nil, m.Err
	}
	if len(m.Vectors) == 0 {
		return make([]float32, m.Dimensions()), nil
	}
	return append([]float32(nil), m.Vectors[0]...), nil
}

func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	if m.Err != nil {
		return nil, m.Err
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if len(m.Vectors) == 0 {
		out := make([][]float32, 0, len(texts))
		for range texts {
			out = append(out, make([]float32, m.Dimensions()))
		}
		return out, nil
	}
	if len(m.Vectors) < len(texts) {
		return nil, errors.New("mock vectors fewer than input texts")
	}
	out := make([][]float32, 0, len(texts))
	for i := range texts {
		out = append(out, append([]float32(nil), m.Vectors[i]...))
	}
	return out, nil
}

func (m *MockEmbedder) Dimensions() int {
	if m.Dims <= 0 {
		return DefaultEmbeddingDimensions
	}
	return m.Dims
}
