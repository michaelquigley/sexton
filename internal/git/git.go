package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	out, err := g.runCtx(ctx, "status", "--porcelain=v1", "-z", "-b", "-uall")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

func (g *Git) StageAll(ctx context.Context) error {
	_, err := g.runCtx(ctx, "add", "-A")
	return err
}

func (g *Git) StageRegions(ctx context.Context, regions []string) error {
	if len(regions) == 0 {
		return fmt.Errorf("commit regions must not be empty")
	}
	args := append([]string{"add", "-A", "--"}, literalPathspecs(regions)...)
	_, err := g.runCtx(ctx, args...)
	return err
}

func (g *Git) Commit(ctx context.Context, message string) (string, error) {
	dirty, err := g.IsDirty(ctx)
	if err != nil {
		return "", err
	}
	if !dirty {
		return "", ErrNothingToCommit
	}

	return g.commit(ctx, message, nil)
}

func (g *Git) CommitOnly(ctx context.Context, message string, regions []string) (string, error) {
	if len(regions) == 0 {
		return "", fmt.Errorf("commit regions must not be empty")
	}
	dirty, err := g.IsDirty(ctx)
	if err != nil {
		return "", err
	}
	if !dirty {
		return "", ErrNothingToCommit
	}

	return g.commit(ctx, message, literalPathspecs(regions))
}

func (g *Git) Pull(ctx context.Context, remote, branch string) (pulled bool, err error) {
	dirty, err := g.IsDirtyTracked(ctx)
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

func (g *Git) Show(ctx context.Context, sha string) (string, error) {
	return g.runCtx(ctx, "show", "--format=", "--diff-merges=first-parent", "--patch", sha)
}

func (g *Git) ShowStat(ctx context.Context, sha string) (string, error) {
	return g.runCtx(ctx, "show", "--format=", "--diff-merges=first-parent", "--stat", sha)
}

func (g *Git) ShowNameStatus(ctx context.Context, sha string) (*Status, error) {
	out, err := g.runCtx(ctx, "show", "--format=", "--diff-merges=first-parent", "--name-status", "-z", sha)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out)
}

func (g *Git) IsDirty(ctx context.Context) (bool, error) {
	out, err := g.runCtx(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (g *Git) IsDirtyTracked(ctx context.Context) (bool, error) {
	out, err := g.runCtx(ctx, "status", "--porcelain", "-uno")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Unmerged returns the paths carrying unresolved conflict markers. a merge or
// cherry-pick conflict leaves the repo on its branch, so the configured-branch
// check does not catch it the way it catches an interrupted rebase.
func (g *Git) Unmerged(ctx context.Context) ([]string, error) {
	out, err := g.runCtx(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
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

func (g *Git) RewordCommit(ctx context.Context, branch, oldSHA, message string) error {
	metadata, err := g.readCommitMetadata(ctx, oldSHA)
	if err != nil {
		return err
	}

	args := []string{"commit-tree", metadata.tree}
	for _, parent := range metadata.parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)

	out, err := g.runCtxEnv(ctx, []string{
		"GIT_AUTHOR_NAME=" + metadata.authorName,
		"GIT_AUTHOR_EMAIL=" + metadata.authorEmail,
		"GIT_AUTHOR_DATE=" + metadata.authorDate,
	}, args...)
	if err != nil {
		return fmt.Errorf("build replacement commit: %w: %s", err, strings.TrimSpace(out))
	}
	newSHA := strings.TrimSpace(out)
	if newSHA == "" {
		return fmt.Errorf("build replacement commit returned an empty object id")
	}

	ref := "refs/heads/" + branch
	out, err = g.runCtx(ctx, "update-ref", ref, newSHA, oldSHA)
	if err != nil {
		return fmt.Errorf("replace commit on %q: %w: %s", branch, err, strings.TrimSpace(out))
	}
	return nil
}

var commitSummaryRegex = regexp.MustCompile(`(?m)^\[[^]\n]* ([0-9a-fA-F]{4,64})\]`)

func (g *Git) commit(ctx context.Context, message string, pathspecs []string) (string, error) {
	args := []string{"-c", "color.ui=false", "commit", "-m", message}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}

	out, err := g.runCtx(ctx, args...)
	if err != nil {
		return "", err
	}
	matches := commitSummaryRegex.FindAllStringSubmatch(out, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("git commit succeeded without reporting the created object id")
	}
	abbreviated := matches[len(matches)-1][1]

	full, err := g.runCtx(ctx, "rev-parse", "--verify", abbreviated+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve created commit %q: %w", abbreviated, err)
	}
	sha := strings.TrimSpace(full)

	committedMessage, err := g.runCtx(ctx, "show", "-s", "--format=%B", sha)
	if err != nil {
		return "", fmt.Errorf("verify created commit %q: %w", sha, err)
	}
	if strings.TrimSpace(committedMessage) != strings.TrimSpace(message) {
		return "", fmt.Errorf("created commit %q has an unexpected message", sha)
	}
	return sha, nil
}

func literalPathspecs(regions []string) []string {
	pathspecs := make([]string, len(regions))
	for i, region := range regions {
		pathspecs[i] = ":(literal)" + region
	}
	return pathspecs
}

type commitMetadata struct {
	tree        string
	parents     []string
	authorName  string
	authorEmail string
	authorDate  string
}

func (g *Git) readCommitMetadata(ctx context.Context, sha string) (*commitMetadata, error) {
	out, err := g.runCtx(ctx, "show", "-s", "--format=%T%x00%P%x00%an%x00%ae%x00%at%x00%ai", sha)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, "\x00")
	if len(parts) != 6 {
		return nil, fmt.Errorf("read commit metadata for %q: expected 6 fields, got %d", sha, len(parts))
	}

	authorTime := strings.Fields(strings.TrimSpace(parts[5]))
	if len(authorTime) == 0 {
		return nil, fmt.Errorf("read commit metadata for %q: author timezone is empty", sha)
	}
	timezone := authorTime[len(authorTime)-1]

	return &commitMetadata{
		tree:        parts[0],
		parents:     strings.Fields(parts[1]),
		authorName:  parts[2],
		authorEmail: parts[3],
		authorDate:  "@" + parts[4] + " " + timezone,
	}, nil
}

func (g *Git) run(args ...string) (string, error) {
	return g.runCtx(context.Background(), args...)
}

func (g *Git) runCtx(ctx context.Context, args ...string) (string, error) {
	return g.runCtxEnv(ctx, nil, args...)
}

func (g *Git) runCtxEnv(ctx context.Context, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.root
	if len(extraEnv) > 0 || g.sshCommand != "" {
		cmd.Env = append(os.Environ(), extraEnv...)
		if g.sshCommand != "" {
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+g.sshCommand)
		}
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
