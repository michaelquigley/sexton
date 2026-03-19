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

func (g *Git) Status(ctx context.Context) (*Status, error) {
	out, err := g.runCtx(ctx, "status", "--porcelain", "-b")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

func (g *Git) StageAll(ctx context.Context) error {
	_, err := g.runCtx(ctx, "add", "-A")
	return err
}

func (g *Git) Commit(ctx context.Context, message string) error {
	if _, err := g.runCtx(ctx, "add", "-A"); err != nil {
		return err
	}

	dirty, err := g.IsDirty(ctx)
	if err != nil {
		return err
	}
	if !dirty {
		return ErrNothingToCommit
	}

	_, err = g.runCtx(ctx, "commit", "-m", message)
	return err
}

func (g *Git) Pull(ctx context.Context, remote, branch string) (pulled bool, err error) {
	dirty, err := g.IsDirty(ctx)
	if err != nil {
		return false, err
	}
	if dirty {
		return false, ErrDirtyWorkingTree
	}

	out, err := g.runCtx(ctx, "pull", "--rebase", remote, branch)
	if err != nil {
		if isConflictOutput(out) {
			return false, ErrConflict
		}
		if isNoRemoteOutput(out) {
			return false, ErrNoRemote
		}
		return false, fmt.Errorf("%w: %s", ErrPullFailed, strings.TrimSpace(out))
	}

	pulled = !strings.Contains(out, "Already up to date")
	return pulled, nil
}

func (g *Git) Push(ctx context.Context, remote, branch string) error {
	out, err := g.runCtx(ctx, "push", remote, "HEAD:"+branch)
	if err != nil {
		if isNoRemoteOutput(out) {
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

func (g *Git) Diff() (string, error) {
	return g.run("diff", "HEAD")
}

func (g *Git) DiffStaged(ctx context.Context) (string, error) {
	return g.runCtx(ctx, "diff", "--staged", "HEAD")
}

func (g *Git) DiffStat(ctx context.Context) (string, error) {
	return g.runCtx(ctx, "diff", "--stat", "HEAD")
}

func (g *Git) IsDirty(ctx context.Context) (bool, error) {
	out, err := g.runCtx(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (g *Git) Branch(ctx context.Context) (string, error) {
	out, err := g.runCtx(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *Git) ShortHEAD(ctx context.Context) (string, error) {
	out, err := g.runCtx(ctx, "rev-parse", "--short", "HEAD")
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
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if stderr.Len() > 0 {
			return stderr.String(), err
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func isConflictOutput(out string) bool {
	return strings.Contains(out, "conflict") || strings.Contains(out, "CONFLICT")
}

func isNoRemoteOutput(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "no remote") ||
		strings.Contains(lower, "no configured push destination") ||
		strings.Contains(lower, "no such remote") ||
		strings.Contains(lower, "does not appear to be a git repository")
}
