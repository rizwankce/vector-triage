package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultEmbeddingEndpoint = "https://models.inference.ai.azure.com/embeddings"

type SleepFunc func(time.Duration)

type GitHubModelsConfig struct {
	Token      string
	Endpoint   string
	Model      string
	Timeout    time.Duration
	MaxRetries int
	MaxChars   int
	Dimensions int
	HTTPClient *http.Client
	Sleep      SleepFunc
}

type GitHubModelsEmbedder struct {
	token      string
	endpoint   string
	model      string
	timeout    time.Duration
	maxRetries int
	maxChars   int
	dimensions int
	client     *http.Client
	sleep      SleepFunc
}

func NewGitHubModelsEmbedder(cfg GitHubModelsConfig) (*GitHubModelsEmbedder, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("github token is required")
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEmbeddingEndpoint
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultEmbeddingModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultMaxInputChars
	}
	dimensions := cfg.Dimensions
	if dimensions <= 0 {
		dimensions = DefaultEmbeddingDimensions
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	} else if client.Timeout <= 0 {
		copyClient := *client
		copyClient.Timeout = timeout
		client = &copyClient
	}

	sleep := cfg.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	return &GitHubModelsEmbedder{
		token:      cfg.Token,
		endpoint:   endpoint,
		model:      model,
		timeout:    timeout,
		maxRetries: maxRetries,
		maxChars:   maxChars,
		dimensions: dimensions,
		client:     client,
		sleep:      sleep,
	}, nil
}

func (g *GitHubModelsEmbedder) Dimensions() int {
	return g.dimensions
}

func (g *GitHubModelsEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := g.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, errors.New("embed response contained no vectors")
	}
	return vectors[0], nil
}

func (g *GitHubModelsEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	truncated := make([]string, len(texts))
	for i, text := range texts {
		truncated[i] = truncateForEmbedding(text, g.maxChars)
	}

	payload := embeddingRequest{
		Input: truncated,
		Model: g.model,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= g.maxRetries; attempt++ {
		vectors, retryAfter, err := g.requestEmbeddings(ctx, bodyBytes)
		if err == nil {
			return vectors, nil
		}
		lastErr = err
		if attempt == g.maxRetries {
			break
		}

		wait := retryAfter
		if wait <= 0 {
			wait = backoffDuration(attempt)
		}
		g.sleep(wait)
	}

	return nil, fmt.Errorf("embed batch failed after %d attempts: %w", g.maxRetries+1, lastErr)
}

func (g *GitHubModelsEmbedder) requestEmbeddings(ctx context.Context, body []byte) ([][]float32, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		bodyText, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, retryAfter, fmt.Errorf("embedding request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyText)))
	}

	var out embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, 0, errors.New("embedding response data is empty")
	}

	vectors := make([][]float32, 0, len(out.Data))
	for _, item := range out.Data {
		vectors = append(vectors, append([]float32(nil), item.Embedding...))
	}

	return vectors, 0, nil
}

func truncateForEmbedding(text string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars])
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		d := time.Until(when)
		if d > 0 {
			return d
		}
	}
	return 0
}

func backoffDuration(attempt int) time.Duration {
	// attempt=0 -> 1s, attempt=1 -> 2s, attempt=2 -> 4s
	seconds := 1 << attempt
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embeddingResponse struct {
	Data []embeddingData `json:"data"`
}

type embeddingData struct {
	Embedding []float32 `json:"embedding"`
}
