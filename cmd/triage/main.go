package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"vector-triage/internal/embed"
	"vector-triage/internal/engine"
	gh "vector-triage/internal/github"
	"vector-triage/internal/respond"
	"vector-triage/internal/store"
)

type config struct {
	Token      string
	EventName  string
	EventPath  string
	Repository string

	SimilarityThreshold float64
	DuplicateThreshold  float64
	MaxResults          int
	IndexBranch         string
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Getenv); err != nil {
		logWarning(err)
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	cfg, err := parseConfigFromEnv(getenv)
	if err != nil {
		return err
	}

	owner, repo, err := gh.ParseRepository(cfg.Repository)
	if err != nil {
		return err
	}

	indexPath := filepath.Join(os.TempDir(), "triage-index.db")

	stateManager := gh.StateManager{
		Owner:  owner,
		Repo:   repo,
		Token:  cfg.Token,
		Branch: cfg.IndexBranch,
	}
	_, err = stateManager.Pull(ctx, indexPath)
	if err != nil {
		return fmt.Errorf("pull state: %w", err)
	}

	s, err := store.Open(ctx, indexPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	githubClient, err := gh.NewClient(cfg.Token, nil)
	if err != nil {
		return fmt.Errorf("create github client: %w", err)
	}

	event, err := gh.ParseEventFile(cfg.EventName, cfg.EventPath, cfg.Repository)
	if err != nil {
		return fmt.Errorf("parse event: %w", err)
	}
	if event.Type == "pr" {
		files, filesErr := githubClient.ListPullRequestFiles(ctx, owner, repo, event.Number)
		if filesErr != nil {
			logWarning(fmt.Errorf("fetch pr files: %w", filesErr))
		} else {
			event.Files = files
		}

		diff, diffErr := githubClient.GetPullRequestDiff(ctx, owner, repo, event.Number)
		if diffErr != nil {
			logWarning(fmt.Errorf("fetch pr diff: %w", diffErr))
		} else {
			event.Diff = diff
		}
	}

	embedder, err := embed.NewGitHubModelsEmbedder(embed.GitHubModelsConfig{
		Token:      cfg.Token,
		MaxRetries: 3,
		Dimensions: embed.DefaultEmbeddingDimensions,
		MaxChars:   embed.DefaultMaxInputChars,
	})
	if err != nil {
		return fmt.Errorf("create embedder: %w", err)
	}

	commentManager := gh.CommentManager{API: githubClient}
	eng := &engine.Engine{
		Embedder: embedder,
		Store:    s,
		Comments: commentManager,
		Formatter: respond.Formatter{
			SimilarityThreshold: cfg.SimilarityThreshold,
			DuplicateThreshold:  cfg.DuplicateThreshold,
		},
		Config: engine.Config{
			SimilarityThreshold: cfg.SimilarityThreshold,
			DuplicateThreshold:  cfg.DuplicateThreshold,
			MaxResults:          cfg.MaxResults,
		},
	}
	if err := eng.Handle(ctx, event); err != nil {
		return fmt.Errorf("engine handle: %w", err)
	}

	if err := stateManager.Push(ctx, indexPath); err != nil {
		return fmt.Errorf("push state: %w", err)
	}

	return nil
}

func parseConfigFromEnv(getenv func(string) string) (config, error) {
	required := []string{"GITHUB_TOKEN", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH", "GITHUB_REPOSITORY"}
	for _, key := range required {
		if strings.TrimSpace(getenv(key)) == "" {
			return config{}, fmt.Errorf("missing required env %s", key)
		}
	}

	similarity, err := parseFloatInput(getenv("INPUT_SIMILARITY_THRESHOLD"), 0.75)
	if err != nil {
		return config{}, fmt.Errorf("parse INPUT_SIMILARITY_THRESHOLD: %w", err)
	}
	duplicate, err := parseFloatInput(getenv("INPUT_DUPLICATE_THRESHOLD"), 0.92)
	if err != nil {
		return config{}, fmt.Errorf("parse INPUT_DUPLICATE_THRESHOLD: %w", err)
	}
	maxResults, err := parseIntInput(getenv("INPUT_MAX_RESULTS"), 5)
	if err != nil {
		return config{}, fmt.Errorf("parse INPUT_MAX_RESULTS: %w", err)
	}
	if maxResults < 1 || maxResults > 20 {
		return config{}, fmt.Errorf("INPUT_MAX_RESULTS must be between 1 and 20")
	}

	indexBranch := strings.TrimSpace(getenv("INPUT_INDEX_BRANCH"))
	if indexBranch == "" {
		indexBranch = "triage-index"
	}

	if similarity < 0 || similarity > 1 {
		return config{}, fmt.Errorf("INPUT_SIMILARITY_THRESHOLD must be between 0 and 1")
	}
	if duplicate < 0 || duplicate > 1 {
		return config{}, fmt.Errorf("INPUT_DUPLICATE_THRESHOLD must be between 0 and 1")
	}

	return config{
		Token:               getenv("GITHUB_TOKEN"),
		EventName:           getenv("GITHUB_EVENT_NAME"),
		EventPath:           getenv("GITHUB_EVENT_PATH"),
		Repository:          getenv("GITHUB_REPOSITORY"),
		SimilarityThreshold: similarity,
		DuplicateThreshold:  duplicate,
		MaxResults:          maxResults,
		IndexBranch:         indexBranch,
	}, nil
}

func parseFloatInput(raw string, fallback float64) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return strconv.ParseFloat(raw, 64)
}

func parseIntInput(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}

func logWarning(err error) {
	if err == nil {
		return
	}
	fmt.Printf("::warning::%s\n", strings.TrimSpace(err.Error()))
}
