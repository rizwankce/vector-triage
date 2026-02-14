package github

import (
	"context"
	"strings"
)

const CommentMarker = "<!-- triage-bot:v1 -->"

type CommentAction string

const (
	CommentActionNoop    CommentAction = "noop"
	CommentActionCreated CommentAction = "created"
	CommentActionUpdated CommentAction = "updated"
	CommentActionDeleted CommentAction = "deleted"
)

type CommentAPI interface {
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) (IssueComment, error)
	UpdateIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (IssueComment, error)
	DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error
}

type CommentManager struct {
	API CommentAPI
}

func (m CommentManager) UpsertTriageComment(ctx context.Context, owner, repo string, number int, body string) (CommentAction, error) {
	comments, err := m.API.ListIssueComments(ctx, owner, repo, number)
	if err != nil {
		return "", err
	}

	existing, found := FindTriageComment(comments)
	normalizedBody := normalizeCommentBody(body)

	if strings.TrimSpace(normalizedBody) == "" {
		if !found {
			return CommentActionNoop, nil
		}
		if err := m.API.DeleteIssueComment(ctx, owner, repo, existing.ID); err != nil {
			return "", err
		}
		return CommentActionDeleted, nil
	}

	if found {
		if strings.TrimSpace(existing.Body) == strings.TrimSpace(normalizedBody) {
			return CommentActionNoop, nil
		}
		if _, err := m.API.UpdateIssueComment(ctx, owner, repo, existing.ID, normalizedBody); err != nil {
			return "", err
		}
		return CommentActionUpdated, nil
	}

	if _, err := m.API.CreateIssueComment(ctx, owner, repo, number, normalizedBody); err != nil {
		return "", err
	}
	return CommentActionCreated, nil
}

func FindTriageComment(comments []IssueComment) (IssueComment, bool) {
	for _, comment := range comments {
		if strings.Contains(comment.Body, CommentMarker) {
			return comment, true
		}
	}
	return IssueComment{}, false
}

func normalizeCommentBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if strings.HasPrefix(body, CommentMarker) {
		return body
	}
	return CommentMarker + "\n" + body
}
