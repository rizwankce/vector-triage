package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v67/github"
)

type IssueComment struct {
	ID     int64
	Body   string
	Author string
}

type authTransport struct {
	token string
	base  http.RoundTripper
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+a.token)
	return a.base.RoundTrip(clone)
}

// Client wraps go-github with triage-specific convenience methods.
type Client struct {
	api *gh.Client
}

func NewClient(token string, httpClient *http.Client) (*Client, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("github token is required")
	}

	if httpClient == nil {
		httpClient = &http.Client{Transport: http.DefaultTransport}
	}

	baseTransport := httpClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	wrapped := *httpClient
	wrapped.Transport = &authTransport{token: token, base: baseTransport}

	api := gh.NewClient(&wrapped)
	return &Client{api: api}, nil
}

func NewClientFromGoGitHub(api *gh.Client) *Client {
	return &Client{api: api}
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	opt := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	out := make([]IssueComment, 0)

	for {
		comments, resp, err := c.api.Issues.ListComments(ctx, owner, repo, number, opt)
		if err != nil {
			return nil, fmt.Errorf("list issue comments: %w", err)
		}

		for _, cm := range comments {
			comment := IssueComment{ID: cm.GetID(), Body: cm.GetBody()}
			if cm.User != nil {
				comment.Author = cm.User.GetLogin()
			}
			out = append(out, comment)
		}

		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return out, nil
}

func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) (IssueComment, error) {
	created, _, err := c.api.Issues.CreateComment(ctx, owner, repo, number, &gh.IssueComment{Body: gh.String(body)})
	if err != nil {
		return IssueComment{}, fmt.Errorf("create issue comment: %w", err)
	}
	return issueCommentFromAPI(created), nil
}

func (c *Client) UpdateIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (IssueComment, error) {
	updated, _, err := c.api.Issues.EditComment(ctx, owner, repo, commentID, &gh.IssueComment{Body: gh.String(body)})
	if err != nil {
		return IssueComment{}, fmt.Errorf("update issue comment: %w", err)
	}
	return issueCommentFromAPI(updated), nil
}

func (c *Client) DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error {
	_, err := c.api.Issues.DeleteComment(ctx, owner, repo, commentID)
	if err != nil {
		return fmt.Errorf("delete issue comment: %w", err)
	}
	return nil
}

func (c *Client) ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	opt := &gh.ListOptions{PerPage: 100}
	files := make([]string, 0)
	for {
		pageFiles, resp, err := c.api.PullRequests.ListFiles(ctx, owner, repo, number, opt)
		if err != nil {
			return nil, fmt.Errorf("list pull request files: %w", err)
		}
		for _, file := range pageFiles {
			if file.GetFilename() != "" {
				files = append(files, file.GetFilename())
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return files, nil
}

func (c *Client) GetPullRequestDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	diff, _, err := c.api.PullRequests.GetRaw(ctx, owner, repo, number, gh.RawOptions{Type: gh.Diff})
	if err != nil {
		return "", fmt.Errorf("get pull request diff: %w", err)
	}
	return diff, nil
}

func issueCommentFromAPI(cm *gh.IssueComment) IssueComment {
	out := IssueComment{}
	if cm == nil {
		return out
	}
	out.ID = cm.GetID()
	out.Body = cm.GetBody()
	if cm.User != nil {
		out.Author = cm.User.GetLogin()
	}
	return out
}
