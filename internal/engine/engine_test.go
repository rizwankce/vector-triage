package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vector-triage/internal/embed"
	gh "vector-triage/internal/github"
	"vector-triage/internal/store"
)

func TestHandle_SearchAndCommentFlow(t *testing.T) {
	t.Helper()

	mockStore := &mockSearchIndexer{
		vectorResults: []store.VectorResult{{ID: "issue/2", Number: 2, Title: "near", VecScore: 0.95}},
		ftsResults:    []store.FTSResult{{ID: "issue/2", Number: 2, Title: "near", FTSScore: 0.8}},
	}
	mockComments := &mockCommentManager{}
	eng := &Engine{
		Embedder: &embed.MockEmbedder{Vectors: [][]float32{{1, 0, 0}}, Dims: 3},
		Store:    mockStore,
		Comments: mockComments,
		Config: Config{
			SimilarityThreshold: 0.75,
			DuplicateThreshold:  0.92,
			MaxResults:          5,
		},
	}

	event := gh.Event{Type: "issue", Owner: "acme", Repo: "repo", Number: 1, Title: "login timeout", Body: "fails"}
	if err := eng.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if mockStore.lastVectorExcludeID != "issue/1" || mockStore.lastFTSExcludeID != "issue/1" {
		t.Fatalf("expected excludeID=issue/1, got vector=%q fts=%q", mockStore.lastVectorExcludeID, mockStore.lastFTSExcludeID)
	}
	if mockStore.upsertItem.ID != "issue/1" {
		t.Fatalf("upsert item id = %q, want issue/1", mockStore.upsertItem.ID)
	}
	if mockStore.upsertVectorID != "issue/1" {
		t.Fatalf("upsert vector id = %q, want issue/1", mockStore.upsertVectorID)
	}
	if mockComments.body == "" {
		t.Fatalf("expected non-empty triage comment body")
	}
}

func TestHandle_NoMatchesDeletesOrNoopsComment(t *testing.T) {
	t.Helper()

	mockStore := &mockSearchIndexer{}
	mockComments := &mockCommentManager{}
	eng := &Engine{
		Embedder: &embed.MockEmbedder{Vectors: [][]float32{{1, 0, 0}}, Dims: 3},
		Store:    mockStore,
		Comments: mockComments,
	}

	event := gh.Event{Type: "issue", Owner: "acme", Repo: "repo", Number: 3, Title: "x", Body: "y"}
	if err := eng.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if mockComments.body != "" {
		t.Fatalf("expected empty comment body for no matches, got %q", mockComments.body)
	}
}

func TestHandle_PropagatesEmbedError(t *testing.T) {
	t.Helper()

	eng := &Engine{
		Embedder: &embed.MockEmbedder{Err: errors.New("embed failed")},
		Store:    &mockSearchIndexer{},
		Comments: &mockCommentManager{},
	}
	event := gh.Event{Type: "issue", Owner: "acme", Repo: "repo", Number: 1, Title: "a", Body: "b"}
	if err := eng.Handle(context.Background(), event); err == nil {
		t.Fatalf("expected embed error")
	}
}

func TestBuildEmbeddableContentPRModes(t *testing.T) {
	t.Helper()

	prWithFilesNoDiff := gh.Event{Type: "pr", Title: "t", Body: "b", Files: []string{"a.go"}, Diff: ""}
	content := buildEmbeddableContent(prWithFilesNoDiff)
	if content == "" || !strings.Contains(content, "Files changed:") {
		t.Fatalf("expected files in PR content: %q", content)
	}
	if strings.Contains(content, "Diff summary:") {
		t.Fatalf("did not expect diff summary: %q", content)
	}
}

type mockSearchIndexer struct {
	vectorResults []store.VectorResult
	ftsResults    []store.FTSResult

	lastVectorExcludeID string
	lastFTSExcludeID    string

	upsertItem     store.ItemRecord
	upsertVectorID string
}

func (m *mockSearchIndexer) SearchVector(ctx context.Context, queryEmbedding []float32, excludeID string, limit int) ([]store.VectorResult, error) {
	_ = ctx
	_ = queryEmbedding
	_ = limit
	m.lastVectorExcludeID = excludeID
	return append([]store.VectorResult(nil), m.vectorResults...), nil
}

func (m *mockSearchIndexer) SearchFTS(ctx context.Context, query string, excludeID string, limit int) ([]store.FTSResult, error) {
	_ = ctx
	_ = query
	_ = limit
	m.lastFTSExcludeID = excludeID
	return append([]store.FTSResult(nil), m.ftsResults...), nil
}

func (m *mockSearchIndexer) UpsertItem(ctx context.Context, rec store.ItemRecord) error {
	_ = ctx
	m.upsertItem = rec
	return nil
}

func (m *mockSearchIndexer) UpsertVector(ctx context.Context, id string, embedding []float32) error {
	_ = ctx
	_ = embedding
	m.upsertVectorID = id
	return nil
}

type mockCommentManager struct {
	body string
}

func (m *mockCommentManager) UpsertTriageComment(ctx context.Context, owner, repo string, number int, body string) (gh.CommentAction, error) {
	_ = ctx
	_ = owner
	_ = repo
	_ = number
	m.body = body
	return gh.CommentActionNoop, nil
}
