package main

import "testing"

func TestParseConfigFromEnv_Defaults(t *testing.T) {
	t.Helper()

	env := map[string]string{
		"GITHUB_TOKEN":      "tkn",
		"GITHUB_EVENT_NAME": "issues",
		"GITHUB_EVENT_PATH": "/tmp/event.json",
		"GITHUB_REPOSITORY": "acme/repo",
	}

	cfg, err := parseConfigFromEnv(mapEnv(env))
	if err != nil {
		t.Fatalf("parseConfigFromEnv() error = %v", err)
	}
	if cfg.SimilarityThreshold != 0.75 || cfg.DuplicateThreshold != 0.92 || cfg.MaxResults != 5 || cfg.IndexBranch != "triage-index" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestParseConfigFromEnv_CustomValues(t *testing.T) {
	t.Helper()

	env := map[string]string{
		"GITHUB_TOKEN":               "tkn",
		"GITHUB_EVENT_NAME":          "pull_request_target",
		"GITHUB_EVENT_PATH":          "/tmp/event.json",
		"GITHUB_REPOSITORY":          "acme/repo",
		"INPUT_SIMILARITY_THRESHOLD": "0.8",
		"INPUT_DUPLICATE_THRESHOLD":  "0.95",
		"INPUT_MAX_RESULTS":          "10",
		"INPUT_INDEX_BRANCH":         "my-index",
	}

	cfg, err := parseConfigFromEnv(mapEnv(env))
	if err != nil {
		t.Fatalf("parseConfigFromEnv() error = %v", err)
	}
	if cfg.SimilarityThreshold != 0.8 || cfg.DuplicateThreshold != 0.95 || cfg.MaxResults != 10 || cfg.IndexBranch != "my-index" {
		t.Fatalf("unexpected config values: %+v", cfg)
	}
}

func TestParseConfigFromEnv_InvalidValues(t *testing.T) {
	t.Helper()

	base := map[string]string{
		"GITHUB_TOKEN":      "tkn",
		"GITHUB_EVENT_NAME": "issues",
		"GITHUB_EVENT_PATH": "/tmp/event.json",
		"GITHUB_REPOSITORY": "acme/repo",
	}

	tests := []map[string]string{
		merge(base, map[string]string{"INPUT_SIMILARITY_THRESHOLD": "bad"}),
		merge(base, map[string]string{"INPUT_DUPLICATE_THRESHOLD": "2"}),
		merge(base, map[string]string{"INPUT_MAX_RESULTS": "0"}),
		merge(base, map[string]string{"GITHUB_TOKEN": ""}),
	}

	for i, tc := range tests {
		if _, err := parseConfigFromEnv(mapEnv(tc)); err == nil {
			t.Fatalf("test #%d expected error", i)
		}
	}
}

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func merge(base map[string]string, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
