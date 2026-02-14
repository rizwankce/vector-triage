package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitHubModelsEmbedder_EmbedSuccess(t *testing.T) {
	t.Helper()

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
				t.Fatalf("authorization header = %q", got)
			}
			return jsonResponse(http.StatusOK, `{"data":[{"embedding":[0.1,0.2,0.3]}]}`), nil
		}),
	}

	emb, err := NewGitHubModelsEmbedder(GitHubModelsConfig{
		Token:      "token-123",
		Endpoint:   "https://example.test/embeddings",
		Model:      DefaultEmbeddingModel,
		Dimensions: 1536,
		MaxRetries: 3,
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("NewGitHubModelsEmbedder() error = %v", err)
	}

	vec, err := emb.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("vector length = %d, want 3", len(vec))
	}
}

func TestGitHubModelsEmbedder_RetriesWithRetryAfterAndBackoff(t *testing.T) {
	t.Helper()

	var calls int32
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			count := atomic.AddInt32(&calls, 1)
			switch count {
			case 1:
				return response(http.StatusTooManyRequests, "rate limited", map[string]string{"Retry-After": "2"}), nil
			case 2:
				return response(http.StatusInternalServerError, "server error", nil), nil
			default:
				return jsonResponse(http.StatusOK, `{"data":[{"embedding":[0.4,0.5]}]}`), nil
			}
		}),
	}

	sleeps := make([]time.Duration, 0)
	emb, err := NewGitHubModelsEmbedder(GitHubModelsConfig{
		Token:      "token-123",
		Endpoint:   "https://example.test/embeddings",
		MaxRetries: 3,
		HTTPClient: client,
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err != nil {
		t.Fatalf("NewGitHubModelsEmbedder() error = %v", err)
	}

	_, err = emb.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("call count = %d, want 3", got)
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleep calls = %d, want 2", len(sleeps))
	}
	if sleeps[0] != 2*time.Second {
		t.Fatalf("first sleep = %v, want 2s from Retry-After", sleeps[0])
	}
	if sleeps[1] != 2*time.Second {
		t.Fatalf("second sleep = %v, want 2s exponential backoff", sleeps[1])
	}
}

func TestGitHubModelsEmbedder_TruncatesInput(t *testing.T) {
	t.Helper()

	var capturedInput string
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			defer r.Body.Close()
			var req struct {
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if len(req.Input) != 1 {
				t.Fatalf("input count = %d, want 1", len(req.Input))
			}
			capturedInput = req.Input[0]
			return jsonResponse(http.StatusOK, `{"data":[{"embedding":[0.9]}]}`), nil
		}),
	}

	emb, err := NewGitHubModelsEmbedder(GitHubModelsConfig{
		Token:      "token-123",
		Endpoint:   "https://example.test/embeddings",
		MaxChars:   10,
		MaxRetries: 0,
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("NewGitHubModelsEmbedder() error = %v", err)
	}

	_, err = emb.Embed(context.Background(), strings.Repeat("x", 30))
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if got := len([]rune(capturedInput)); got != 10 {
		t.Fatalf("captured input rune count = %d, want 10", got)
	}
}

func TestGitHubModelsEmbedder_FailsAfterMaxRetries(t *testing.T) {
	t.Helper()

	var calls int32
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return response(http.StatusServiceUnavailable, "nope", nil), nil
		}),
	}

	emb, err := NewGitHubModelsEmbedder(GitHubModelsConfig{
		Token:      "token-123",
		Endpoint:   "https://example.test/embeddings",
		MaxRetries: 2,
		HTTPClient: client,
		Sleep:      func(time.Duration) {},
	})
	if err != nil {
		t.Fatalf("NewGitHubModelsEmbedder() error = %v", err)
	}

	_, err = emb.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatalf("Embed() expected error, got nil")
	}

	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("call count = %d, want 3 (initial + 2 retries)", got)
	}
}

func TestNewGitHubModelsEmbedder_RequiresToken(t *testing.T) {
	t.Helper()
	_, err := NewGitHubModelsEmbedder(GitHubModelsConfig{})
	if err == nil {
		t.Fatalf("expected token validation error")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func response(status int, body string, headers map[string]string) *http.Response {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func jsonResponse(status int, body string) *http.Response {
	return response(status, body, map[string]string{"Content-Type": "application/json"})
}
