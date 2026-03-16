package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Git struct {
	root string
}

func New(root string) *Git {
	g := &Git{root: root}
	if !g.IsRepo() {
		return nil
	}
	return g
}

func (g *Git) IsRepo() bool {
	_, err := g.run("rev-parse", "--git-dir")
	return err == nil
}

func (g *Git) StageAll(ctx context.Context) error {
	_, err := g.runCtx(ctx, "add", "-A")
	return err
}

func (g *Git) Commit(ctx context.Context, message string) error {
	if _, err := g.runCtx(ctx, "add", "-A"); err != nil {
		return err
	}

	dirty, err := g.IsDirty()
	if err != nil {
		return err
	}
	if !dirty {
		return ErrNothingToCommit
	}

	_, err = g.runCtx(ctx, "commit", "-m", message)
	return err
}

func (g *Git) Pull(ctx context.Context) (pulled bool, err error) {
	dirty, err := g.IsDirty()
	if err != nil {
		return false, err
	}
	if dirty {
		return false, ErrDirtyWorkingTree
	}

	out, err := g.runCtx(ctx, "pull", "--rebase")
	if err != nil {
		if strings.Contains(out, "conflict") || strings.Contains(out, "CONFLICT") {
			return false, ErrConflict
		}
		if strings.Contains(out, "no remote") || strings.Contains(out, "No remote") {
			return false, ErrNoRemote
		}
		return false, fmt.Errorf("%w: %s", ErrPullFailed, strings.TrimSpace(out))
	}

	pulled = !strings.Contains(out, "Already up to date") && !strings.Contains(out, "Current branch") || strings.Contains(out, "rewinding")
	// simpler: if "Already up to date" not present, something happened
	pulled = !strings.Contains(out, "Already up to date")
	return pulled, nil
}

func (g *Git) Push(ctx context.Context) error {
	out, err := g.runCtx(ctx, "push")
	if err != nil {
		if strings.Contains(out, "no remote") || strings.Contains(out, "No configured push destination") {
			return ErrNoRemote
		}
		return fmt.Errorf("%w: %s", ErrPushFailed, strings.TrimSpace(out))
	}
	return nil
}

func (g *Git) RebaseAbort(ctx context.Context) error {
	_, err := g.runCtx(ctx, "rebase", "--abort")
	return err
}

func (g *Git) Status() (*Status, error) {
	out, err := g.run("status", "--porcelain", "-b")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

func (g *Git) Diff() (string, error) {
	return g.run("diff", "HEAD")
}

func (g *Git) DiffStaged() (string, error) {
	return g.run("diff", "--staged", "HEAD")
}

func (g *Git) DiffStat() (string, error) {
	return g.run("diff", "--stat", "HEAD")
}

func (g *Git) IsDirty() (bool, error) {
	out, err := g.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (g *Git) Branch() (string, error) {
	out, err := g.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *Git) ShortHEAD() (string, error) {
	out, err := g.run("rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *Git) run(args ...string) (string, error) {
	return g.runCtx(context.Background(), args...)
}

func (g *Git) runCtx(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.root

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return stderr.String(), err
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}
