package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateManagerPull_FirstRunBranchMissing(t *testing.T) {
	t.Helper()

	runner := &fakeRunner{
		onRun: func(dir, name string, args ...string) (string, error) {
			if commandString(name, args...) == "git fetch origin triage-index --depth=1" {
				return "fatal: couldn't find remote ref triage-index", errors.New("missing branch")
			}
			return "", nil
		},
	}

	manager := StateManager{Owner: "acme", Repo: "repo", Token: "tkn", Branch: "triage-index", Runner: runner}
	found, err := manager.Pull(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if found {
		t.Fatalf("Pull() found = true, want false for first-run")
	}
}

func TestStateManagerPull_ExistingBranchCopiesIndex(t *testing.T) {
	t.Helper()

	runner := &fakeRunner{
		onRun: func(dir, name string, args ...string) (string, error) {
			if commandString(name, args...) == "git checkout FETCH_HEAD -- index.db" {
				return "", os.WriteFile(filepath.Join(dir, "index.db"), []byte("db-content"), 0o644)
			}
			return "", nil
		},
	}

	dst := filepath.Join(t.TempDir(), "index.db")
	manager := StateManager{Owner: "acme", Repo: "repo", Token: "tkn", Branch: "triage-index", Runner: runner}
	found, err := manager.Pull(context.Background(), dst)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if !found {
		t.Fatalf("Pull() found = false, want true")
	}
	bytes, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(bytes) != "db-content" {
		t.Fatalf("copied content mismatch: %q", string(bytes))
	}
}

func TestStateManagerPush_UsesExpectedGitFlow(t *testing.T) {
	t.Helper()

	runner := &fakeRunner{
		onRun: func(dir, name string, args ...string) (string, error) {
			cmd := commandString(name, args...)
			if cmd == "git rm -rf ." {
				return "nothing to remove", errors.New("no files")
			}
			return "", nil
		},
	}

	src := filepath.Join(t.TempDir(), "index.db")
	if err := os.WriteFile(src, []byte("db"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager := StateManager{Owner: "acme", Repo: "repo", Token: "tkn", Branch: "triage-index", Runner: runner}
	if err := manager.Push(context.Background(), src); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "git checkout --orphan triage-index") {
		t.Fatalf("missing orphan checkout command: %s", joined)
	}
	if !strings.Contains(joined, "git -c user.name=triage-bot -c user.email=triage-bot@users.noreply.github.com commit -m Update triage index [skip ci]") {
		t.Fatalf("missing commit command: %s", joined)
	}
	if !strings.Contains(joined, "git push origin triage-index --force") {
		t.Fatalf("missing push command: %s", joined)
	}
}

func TestStateManagerRequiresToken(t *testing.T) {
	t.Helper()
	manager := StateManager{Owner: "acme", Repo: "repo", Token: ""}
	_, err := manager.Pull(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err == nil {
		t.Fatalf("expected missing token error")
	}
}

type fakeRunner struct {
	calls []string
	onRun func(dir, name string, args ...string) (string, error)
}

func (f *fakeRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	_ = ctx
	cmd := commandString(name, args...)
	f.calls = append(f.calls, cmd)
	if f.onRun == nil {
		return "", nil
	}
	return f.onRun(dir, name, args...)
}

func commandString(name string, args ...string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}
