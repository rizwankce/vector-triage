package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vector-triage/internal/embed"
	gh "vector-triage/internal/github"
	"vector-triage/internal/ingest"
	"vector-triage/internal/store"
)

type SearchIndexer interface {
	SearchVector(ctx context.Context, queryEmbedding []float32, excludeID string, limit int) ([]store.VectorResult, error)
	SearchFTS(ctx context.Context, query string, excludeID string, limit int) ([]store.FTSResult, error)
	UpsertItem(ctx context.Context, rec store.ItemRecord) error
	UpsertVector(ctx context.Context, id string, embedding []float32) error
}

type CommentManager interface {
	UpsertTriageComment(ctx context.Context, owner, repo string, number int, body string) (gh.CommentAction, error)
}

type Formatter interface {
	Format(event gh.Event, results []store.FusedResult) string
}

type Config struct {
	SimilarityThreshold float64
	DuplicateThreshold  float64
	MaxResults          int
}

type Engine struct {
	Embedder  embed.Embedder
	Store     SearchIndexer
	Comments  CommentManager
	Formatter Formatter
	Config    Config
}

func (e *Engine) Handle(ctx context.Context, event gh.Event) error {
	if e == nil {
		return errors.New("nil engine")
	}
	if e.Store == nil {
		return errors.New("store dependency is required")
	}
	if e.Comments == nil {
		return errors.New("comment manager dependency is required")
	}

	currentID := store.BuildItemID(event.Type, event.Number)
	content := buildEmbeddableContent(event)

	var embedding []float32
	var vecResults []store.VectorResult
	var ftsResults []store.FTSResult

	if strings.TrimSpace(content) != "" {
		if e.Embedder == nil {
			return errors.New("embedder dependency is required when content is available")
		}

		vec, err := e.Embedder.Embed(ctx, content)
		if err != nil {
			return fmt.Errorf("embed content: %w", err)
		}
		embedding = vec

		limit := e.maxResults()
		vecResults, err = e.Store.SearchVector(ctx, embedding, currentID, limit)
		if err != nil {
			return fmt.Errorf("vector search: %w", err)
		}
		ftsResults, err = e.Store.SearchFTS(ctx, content, currentID, limit)
		if err != nil {
			return fmt.Errorf("fts search: %w", err)
		}
	}

	fused := store.FuseResults(vecResults, ftsResults, currentID, store.FuseConfig{
		SimilarityThreshold: e.similarityThreshold(),
		DuplicateThreshold:  e.duplicateThreshold(),
		MaxResults:          e.maxResults(),
	})

	item := buildItemRecord(event, currentID)
	if err := e.Store.UpsertItem(ctx, item); err != nil {
		return fmt.Errorf("upsert item: %w", err)
	}
	if len(embedding) > 0 {
		if err := e.Store.UpsertVector(ctx, currentID, embedding); err != nil {
			return fmt.Errorf("upsert vector: %w", err)
		}
	}

	commentBody := ""
	if len(fused) > 0 {
		if e.Formatter != nil {
			commentBody = e.Formatter.Format(event, fused)
		} else {
			commentBody = defaultReport(event, fused)
		}
	}

	if _, err := e.Comments.UpsertTriageComment(ctx, event.Owner, event.Repo, event.Number, commentBody); err != nil {
		return fmt.Errorf("upsert triage comment: %w", err)
	}

	return nil
}

func (e *Engine) similarityThreshold() float64 {
	if e.Config.SimilarityThreshold <= 0 {
		return 0.75
	}
	return e.Config.SimilarityThreshold
}

func (e *Engine) duplicateThreshold() float64 {
	if e.Config.DuplicateThreshold <= 0 {
		return 0.92
	}
	return e.Config.DuplicateThreshold
}

func (e *Engine) maxResults() int {
	if e.Config.MaxResults <= 0 {
		return 5
	}
	return e.Config.MaxResults
}

func buildEmbeddableContent(event gh.Event) string {
	switch event.Type {
	case "issue":
		return ingest.BuildIssueContent(event.Title, event.Body)
	case "pr":
		mode := ingest.PRDiffModeInclude
		switch {
		case len(event.Files) > 0 && strings.TrimSpace(event.Diff) == "":
			mode = ingest.PRDiffModeSkipDiffKeepFiles
		case len(event.Files) == 0 && strings.TrimSpace(event.Diff) == "":
			mode = ingest.PRDiffModeTitleBodyOnly
		}
		return ingest.BuildPRContent(ingest.PRInput{
			Title: event.Title,
			Body:  event.Body,
			Files: event.Files,
			Diff:  event.Diff,
			Mode:  mode,
		})
	default:
		return ""
	}
}

func buildItemRecord(event gh.Event, id string) store.ItemRecord {
	return store.ItemRecord{
		ID:     id,
		Type:   normalizeItemType(event.Type),
		Number: event.Number,
		Title:  event.Title,
		Body:   event.Body,
		Author: event.Author,
		State:  event.State,
		Labels: event.Labels,
		Files:  event.Files,
		URL:    event.URL,
	}
}

func normalizeItemType(kind string) string {
	if kind == "issue" {
		return "issue"
	}
	return "pr"
}

func defaultReport(event gh.Event, results []store.FusedResult) string {
	var b strings.Builder
	b.WriteString(gh.CommentMarker)
	b.WriteString("\n### Triage Report\n\n")
	for _, result := range results {
		percent := int(result.DisplaySimilarity * 100)
		b.WriteString(fmt.Sprintf("- #%d %s (%d%%)\n", result.Number, result.Title, percent))
	}
	return b.String()
}
