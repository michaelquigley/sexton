package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Git struct {
	root       string
	sshCommand string
}

func New(root, sshKey string) *Git {
	g := &Git{root: root}
	if !g.IsRepo() {
		return nil
	}
	if sshKey != "" {
		g.sshCommand = buildSSHCommand(sshKey)
	}
	return g
}

// buildSSHCommand builds a GIT_SSH_COMMAND value that authenticates git with a
// specific private key and offers only that key, so git never falls back to a
// running ssh-agent. the key path is shell-quoted because git parses
// GIT_SSH_COMMAND with sh-style word splitting.
func buildSSHCommand(keyPath string) string {
	return fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes", shellQuote(keyPath))
}

// shellQuote wraps a value in single quotes for safe sh-style parsing, escaping
// any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (g *Git) IsRepo() bool {
	_, err := g.run("rev-parse", "--git-dir")
	return err == nil
}

func (g *Git) Status(ctx context.Context) (*Status, error) {
	out, err := g.runCtx(ctx, "status", "--porcelain", "-b", "-uall")
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

	before, beforeErr := g.head(ctx)
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

	if beforeErr == nil {
		after, err := g.head(ctx)
		if err != nil {
			return false, err
		}
		return before != after, nil
	}

	pulled = !isAlreadyUpToDateOutput(out)
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

func (g *Git) head(ctx context.Context) (string, error) {
	out, err := g.runCtx(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CommitTime returns the author timestamp of HEAD.
func (g *Git) CommitTime(ctx context.Context) (time.Time, error) {
	out, err := g.runCtx(ctx, "log", "-1", "--format=%aI")
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(out))
}

func (g *Git) run(args ...string) (string, error) {
	return g.runCtx(context.Background(), args...)
}

func (g *Git) runCtx(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.root
	if g.sshCommand != "" {
		cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+g.sshCommand)
	}

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

func isAlreadyUpToDateOutput(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "already up to date") ||
		(strings.Contains(lower, "current branch") && strings.Contains(lower, "is up to date"))
}
