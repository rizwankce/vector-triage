package github

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	gh "github.com/google/go-github/v67/github"
)

func TestParseEventFile_Issue(t *testing.T) {
	t.Helper()

	payload := `{
  "action": "opened",
  "issue": {
    "number": 12,
    "title": "Login timeout",
    "body": "App hangs after 30s",
    "state": "open",
    "html_url": "https://github.com/acme/repo/issues/12",
    "user": {"login": "alice"},
    "labels": [{"name":"bug"}, {"name":"auth"}]
  }
}`
	path := writeTempPayload(t, payload)

	event, err := ParseEventFile("issues", path, "acme/repo")
	if err != nil {
		t.Fatalf("ParseEventFile() error = %v", err)
	}

	if event.Type != "issue" || event.Number != 12 || event.Author != "alice" {
		t.Fatalf("unexpected issue event: %+v", event)
	}
	if len(event.Labels) != 2 {
		t.Fatalf("labels len = %d, want 2", len(event.Labels))
	}
}

func TestParseEventFile_PullRequestTarget(t *testing.T) {
	t.Helper()

	payload := `{
  "action": "synchronize",
  "pull_request": {
    "number": 7,
    "title": "Improve auth",
    "body": "Retries for auth",
    "state": "open",
    "merged": false,
    "html_url": "https://github.com/acme/repo/pull/7",
    "user": {"login": "bob"},
    "files": ["src/auth.go", " README.md "],
    "diff": "@@ -1 +1 @@"
  }
}`
	path := writeTempPayload(t, payload)

	event, err := ParseEventFile("pull_request_target", path, "acme/repo")
	if err != nil {
		t.Fatalf("ParseEventFile() error = %v", err)
	}

	if event.Type != "pr" || event.Number != 7 || event.Author != "bob" {
		t.Fatalf("unexpected pr event: %+v", event)
	}
	if event.State != "open" {
		t.Fatalf("state = %s, want open", event.State)
	}
	if len(event.Files) != 2 {
		t.Fatalf("files len = %d, want 2", len(event.Files))
	}
	if event.Diff == "" {
		t.Fatalf("expected diff in event")
	}
}

func TestParseRepository(t *testing.T) {
	t.Helper()

	owner, repo, err := ParseRepository("acme/repo")
	if err != nil {
		t.Fatalf("ParseRepository() error = %v", err)
	}
	if owner != "acme" || repo != "repo" {
		t.Fatalf("owner/repo mismatch: %s/%s", owner, repo)
	}

	if _, _, err := ParseRepository("bad-format"); err == nil {
		t.Fatalf("expected error for invalid repository format")
	}
}

func TestClient_CommentOperations(t *testing.T) {
	t.Helper()

	transport := &recordingTransport{
		handler: func(r *http.Request, body []byte) (*http.Response, error) {
			switch {
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/acme/repo/issues/10/comments"):
				return jsonResponse(200, `[{"id":1,"body":"a","user":{"login":"bot"}}]`), nil
			case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/issues/10/comments":
				if !strings.Contains(string(body), `"body":"new"`) {
					t.Fatalf("unexpected create payload: %s", string(body))
				}
				return jsonResponse(201, `{"id":2,"body":"new","user":{"login":"bot"}}`), nil
			case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/repo/issues/comments/2":
				if !strings.Contains(string(body), `"body":"updated"`) {
					t.Fatalf("unexpected edit payload: %s", string(body))
				}
				return jsonResponse(200, `{"id":2,"body":"updated","user":{"login":"bot"}}`), nil
			case r.Method == http.MethodDelete && r.URL.Path == "/repos/acme/repo/issues/comments/2":
				return response(204, "", nil), nil
			default:
				return jsonResponse(404, `{"message":"not found"}`), nil
			}
		},
	}

	client := NewClientFromGoGitHub(newGoGitHubClientWithTransport(transport))

	comments, err := client.ListIssueComments(context.Background(), "acme", "repo", 10)
	if err != nil {
		t.Fatalf("ListIssueComments() error = %v", err)
	}
	if len(comments) != 1 || comments[0].ID != 1 {
		t.Fatalf("unexpected comments: %+v", comments)
	}

	created, err := client.CreateIssueComment(context.Background(), "acme", "repo", 10, "new")
	if err != nil {
		t.Fatalf("CreateIssueComment() error = %v", err)
	}
	if created.ID != 2 {
		t.Fatalf("created ID = %d, want 2", created.ID)
	}

	updated, err := client.UpdateIssueComment(context.Background(), "acme", "repo", 2, "updated")
	if err != nil {
		t.Fatalf("UpdateIssueComment() error = %v", err)
	}
	if updated.Body != "updated" {
		t.Fatalf("updated body = %q", updated.Body)
	}

	if err := client.DeleteIssueComment(context.Background(), "acme", "repo", 2); err != nil {
		t.Fatalf("DeleteIssueComment() error = %v", err)
	}
}

func TestClient_PRFilesAndDiff(t *testing.T) {
	t.Helper()

	transport := &recordingTransport{
		handler: func(r *http.Request, body []byte) (*http.Response, error) {
			switch {
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/acme/repo/pulls/7/files"):
				return jsonResponse(200, `[{"filename":"a.go"},{"filename":"b.go"}]`), nil
			case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/pulls/7":
				if got := r.Header.Get("Accept"); !strings.Contains(strings.ToLower(got), "diff") {
					t.Fatalf("expected diff accept header, got %q", got)
				}
				return response(200, "@@ -1 +1 @@", map[string]string{"Content-Type": "text/plain"}), nil
			default:
				return jsonResponse(404, `{"message":"not found"}`), nil
			}
		},
	}

	client := NewClientFromGoGitHub(newGoGitHubClientWithTransport(transport))

	files, err := client.ListPullRequestFiles(context.Background(), "acme", "repo", 7)
	if err != nil {
		t.Fatalf("ListPullRequestFiles() error = %v", err)
	}
	if len(files) != 2 || files[0] != "a.go" || files[1] != "b.go" {
		t.Fatalf("unexpected files: %+v", files)
	}

	diff, err := client.GetPullRequestDiff(context.Background(), "acme", "repo", 7)
	if err != nil {
		t.Fatalf("GetPullRequestDiff() error = %v", err)
	}
	if !strings.Contains(diff, "@@") {
		t.Fatalf("unexpected diff: %q", diff)
	}
}

func TestNewClient_RequiresToken(t *testing.T) {
	t.Helper()
	if _, err := NewClient("", nil); err == nil {
		t.Fatalf("expected token validation error")
	}
}

func newGoGitHubClientWithTransport(transport http.RoundTripper) *gh.Client {
	httpClient := &http.Client{Transport: transport}
	api := gh.NewClient(httpClient)
	baseURL, err := url.Parse("https://api.github.com/")
	if err != nil {
		panic(err)
	}
	api.BaseURL = baseURL
	return api
}

// recordingTransport handles requests in-memory.
type recordingTransport struct {
	handler func(r *http.Request, body []byte) (*http.Response, error)
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	if rt.handler == nil {
		return jsonResponse(500, `{"message":"no handler"}`), nil
	}
	return rt.handler(r, body)
}

func writeTempPayload(t *testing.T, payload string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "event-*.json")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(payload); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	return f.Name()
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
