package embed

import "context"

const (
	DefaultEmbeddingModel      = "text-embedding-3-small"
	DefaultEmbeddingDimensions = 1536
	DefaultMaxInputChars       = 30000
)

// Embedder converts text into vector embeddings.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}
