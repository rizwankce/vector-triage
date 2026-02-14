package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Event is the normalized payload consumed by the triage engine.
type Event struct {
	Type   string
	Action string

	Owner string
	Repo  string

	Number int
	Title  string
	Body   string

	Author string
	Labels []string
	State  string
	URL    string

	Diff  string
	Files []string
}

func ParseRepository(repository string) (owner string, repo string, err error) {
	repository = strings.TrimSpace(repository)
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid repository format %q, expected owner/repo", repository)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func ParseEventFile(eventName, eventPath, repository string) (Event, error) {
	owner, repo, err := ParseRepository(repository)
	if err != nil {
		return Event{}, err
	}

	payload, err := os.ReadFile(eventPath)
	if err != nil {
		return Event{}, fmt.Errorf("read event payload: %w", err)
	}

	eventName = strings.TrimSpace(eventName)
	switch eventName {
	case "issues":
		return parseIssueEvent(payload, owner, repo)
	case "pull_request", "pull_request_target":
		return parsePullRequestEvent(payload, owner, repo)
	default:
		return Event{}, fmt.Errorf("unsupported event name %q", eventName)
	}
}

func parseIssueEvent(payload []byte, owner, repo string) (Event, error) {
	var in issueEventPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return Event{}, fmt.Errorf("decode issue event: %w", err)
	}

	if in.Issue.Number == 0 {
		return Event{}, errors.New("issue number missing in event payload")
	}

	labels := make([]string, 0, len(in.Issue.Labels))
	for _, label := range in.Issue.Labels {
		if strings.TrimSpace(label.Name) != "" {
			labels = append(labels, label.Name)
		}
	}

	return Event{
		Type:   "issue",
		Action: in.Action,
		Owner:  owner,
		Repo:   repo,
		Number: in.Issue.Number,
		Title:  in.Issue.Title,
		Body:   in.Issue.Body,
		Author: in.Issue.User.Login,
		Labels: labels,
		State:  in.Issue.State,
		URL:    in.Issue.HTMLURL,
	}, nil
}

func parsePullRequestEvent(payload []byte, owner, repo string) (Event, error) {
	var in pullRequestEventPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return Event{}, fmt.Errorf("decode pull request event: %w", err)
	}

	if in.PullRequest.Number == 0 {
		return Event{}, errors.New("pull request number missing in event payload")
	}

	state := in.PullRequest.State
	if in.PullRequest.Merged {
		state = "merged"
	}

	// PR metadata is treated as untrusted text and never executed.
	files := normalizeFilePaths(in.PullRequest.Files)
	return Event{
		Type:   "pr",
		Action: in.Action,
		Owner:  owner,
		Repo:   repo,
		Number: in.PullRequest.Number,
		Title:  in.PullRequest.Title,
		Body:   in.PullRequest.Body,
		Author: in.PullRequest.User.Login,
		State:  state,
		URL:    in.PullRequest.HTMLURL,
		Diff:   in.PullRequest.Diff,
		Files:  files,
	}, nil
}

func normalizeFilePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

type issueEventPayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
}

type pullRequestEventPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		Merged  bool   `json:"merged"`
		HTMLURL string `json:"html_url"`

		// Optional convenience fields used by tests and local fixtures.
		Diff  string   `json:"diff"`
		Files []string `json:"files"`

		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
}
