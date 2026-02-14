package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultIndexFileName = "index.db"

type CommandRunner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

type StateManager struct {
	Owner  string
	Repo   string
	Token  string
	Branch string
	Runner CommandRunner
}

func (m StateManager) branchName() string {
	if strings.TrimSpace(m.Branch) == "" {
		return "triage-index"
	}
	return m.Branch
}

func (m StateManager) runner() CommandRunner {
	if m.Runner == nil {
		return ExecRunner{}
	}
	return m.Runner
}

func (m StateManager) remoteURL() (string, error) {
	if strings.TrimSpace(m.Token) == "" {
		return "", errors.New("token is required for state manager")
	}
	if strings.TrimSpace(m.Owner) == "" || strings.TrimSpace(m.Repo) == "" {
		return "", errors.New("owner/repo is required for state manager")
	}
	return fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", m.Token, m.Owner, m.Repo), nil
}

// Pull downloads index.db from the configured orphan branch.
// found=false means the branch does not exist yet (first-run case).
func (m StateManager) Pull(ctx context.Context, dstPath string) (found bool, err error) {
	url, err := m.remoteURL()
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(dstPath) == "" {
		return false, errors.New("destination path is required")
	}

	tmpDir, err := os.MkdirTemp("", "triage-state-pull-*")
	if err != nil {
		return false, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	r := m.runner()
	if _, err := r.Run(ctx, tmpDir, "git", "init"); err != nil {
		return false, err
	}
	if _, err := r.Run(ctx, tmpDir, "git", "remote", "add", "origin", url); err != nil {
		return false, err
	}

	branch := m.branchName()
	if out, err := r.Run(ctx, tmpDir, "git", "fetch", "origin", branch, "--depth=1"); err != nil {
		if isMissingBranchError(out) || isMissingBranchError(err.Error()) {
			return false, nil
		}
		return false, err
	}

	if _, err := r.Run(ctx, tmpDir, "git", "checkout", "FETCH_HEAD", "--", defaultIndexFileName); err != nil {
		return false, err
	}

	src := filepath.Join(tmpDir, defaultIndexFileName)
	if _, err := os.Stat(src); err != nil {
		return false, fmt.Errorf("pulled index file missing: %w", err)
	}
	if err := copyFile(src, dstPath); err != nil {
		return false, fmt.Errorf("copy pulled index file: %w", err)
	}

	return true, nil
}

// Push uploads index.db to the configured orphan branch.
func (m StateManager) Push(ctx context.Context, srcPath string) error {
	url, err := m.remoteURL()
	if err != nil {
		return err
	}
	if strings.TrimSpace(srcPath) == "" {
		return errors.New("source path is required")
	}
	if _, err := os.Stat(srcPath); err != nil {
		return fmt.Errorf("source index file missing: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "triage-state-push-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	r := m.runner()
	if _, err := r.Run(ctx, tmpDir, "git", "init"); err != nil {
		return err
	}
	if _, err := r.Run(ctx, tmpDir, "git", "remote", "add", "origin", url); err != nil {
		return err
	}

	branch := m.branchName()
	if _, err := r.Run(ctx, tmpDir, "git", "checkout", "--orphan", branch); err != nil {
		return err
	}
	_, _ = r.Run(ctx, tmpDir, "git", "rm", "-rf", ".") // can fail on empty tree; safe to ignore

	dst := filepath.Join(tmpDir, defaultIndexFileName)
	if err := copyFile(srcPath, dst); err != nil {
		return fmt.Errorf("copy index file for push: %w", err)
	}

	if _, err := r.Run(ctx, tmpDir, "git", "add", defaultIndexFileName); err != nil {
		return err
	}
	if _, err := r.Run(ctx, tmpDir, "git", "-c", "user.name=triage-bot", "-c", "user.email=triage-bot@users.noreply.github.com", "commit", "-m", "Update triage index [skip ci]"); err != nil {
		return err
	}
	if _, err := r.Run(ctx, tmpDir, "git", "push", "origin", branch, "--force"); err != nil {
		return err
	}

	return nil
}

func isMissingBranchError(raw string) bool {
	raw = strings.ToLower(raw)
	return strings.Contains(raw, "couldn't find remote ref") || strings.Contains(raw, "unknown revision")
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
