package github

import (
	"context"
	"errors"
	"testing"
)

func TestCommentManagerCreate(t *testing.T) {
	t.Helper()
	api := &fakeCommentAPI{comments: []IssueComment{}}
	mgr := CommentManager{API: api}

	action, err := mgr.UpsertTriageComment(context.Background(), "acme", "repo", 1, "### Report")
	if err != nil {
		t.Fatalf("UpsertTriageComment() error = %v", err)
	}
	if action != CommentActionCreated {
		t.Fatalf("action = %s, want %s", action, CommentActionCreated)
	}
	if api.created == 0 {
		t.Fatalf("expected create call")
	}
	if api.createdBody == "" || api.createdBody[:len(CommentMarker)] != CommentMarker {
		t.Fatalf("expected marker-prefixed body, got %q", api.createdBody)
	}
}

func TestCommentManagerUpdate(t *testing.T) {
	t.Helper()
	api := &fakeCommentAPI{comments: []IssueComment{{ID: 10, Body: CommentMarker + "\nold"}}}
	mgr := CommentManager{API: api}

	action, err := mgr.UpsertTriageComment(context.Background(), "acme", "repo", 1, "new")
	if err != nil {
		t.Fatalf("UpsertTriageComment() error = %v", err)
	}
	if action != CommentActionUpdated {
		t.Fatalf("action = %s, want %s", action, CommentActionUpdated)
	}
	if api.updatedID != 10 {
		t.Fatalf("updated ID = %d, want 10", api.updatedID)
	}
}

func TestCommentManagerNoopWhenUnchanged(t *testing.T) {
	t.Helper()
	body := CommentMarker + "\n### Report"
	api := &fakeCommentAPI{comments: []IssueComment{{ID: 10, Body: body}}}
	mgr := CommentManager{API: api}

	action, err := mgr.UpsertTriageComment(context.Background(), "acme", "repo", 1, body)
	if err != nil {
		t.Fatalf("UpsertTriageComment() error = %v", err)
	}
	if action != CommentActionNoop {
		t.Fatalf("action = %s, want %s", action, CommentActionNoop)
	}
	if api.updatedID != 0 || api.created != 0 || api.deletedID != 0 {
		t.Fatalf("expected no API mutation calls")
	}
}

func TestCommentManagerDeleteWhenBodyEmpty(t *testing.T) {
	t.Helper()
	api := &fakeCommentAPI{comments: []IssueComment{{ID: 55, Body: CommentMarker + "\nold"}}}
	mgr := CommentManager{API: api}

	action, err := mgr.UpsertTriageComment(context.Background(), "acme", "repo", 1, "")
	if err != nil {
		t.Fatalf("UpsertTriageComment() error = %v", err)
	}
	if action != CommentActionDeleted {
		t.Fatalf("action = %s, want %s", action, CommentActionDeleted)
	}
	if api.deletedID != 55 {
		t.Fatalf("deleted ID = %d, want 55", api.deletedID)
	}
}

func TestFindTriageComment(t *testing.T) {
	t.Helper()
	c, found := FindTriageComment([]IssueComment{
		{ID: 1, Body: "plain"},
		{ID: 2, Body: CommentMarker + "\nreport"},
	})
	if !found || c.ID != 2 {
		t.Fatalf("expected marker comment, got found=%v comment=%+v", found, c)
	}
}

func TestCommentManagerPropagatesAPIError(t *testing.T) {
	t.Helper()
	api := &fakeCommentAPI{listErr: errors.New("boom")}
	mgr := CommentManager{API: api}
	if _, err := mgr.UpsertTriageComment(context.Background(), "acme", "repo", 1, "body"); err == nil {
		t.Fatalf("expected list error")
	}
}

type fakeCommentAPI struct {
	comments []IssueComment
	listErr  error

	created     int
	createdBody string
	updatedID   int64
	deletedID   int64
}

func (f *fakeCommentAPI) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	_ = ctx
	_ = owner
	_ = repo
	_ = number
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]IssueComment(nil), f.comments...), nil
}

func (f *fakeCommentAPI) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) (IssueComment, error) {
	_ = ctx
	_ = owner
	_ = repo
	_ = number
	f.created++
	f.createdBody = body
	return IssueComment{ID: 999, Body: body}, nil
}

func (f *fakeCommentAPI) UpdateIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (IssueComment, error) {
	_ = ctx
	_ = owner
	_ = repo
	f.updatedID = commentID
	return IssueComment{ID: commentID, Body: body}, nil
}

func (f *fakeCommentAPI) DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error {
	_ = ctx
	_ = owner
	_ = repo
	f.deletedID = commentID
	return nil
}
