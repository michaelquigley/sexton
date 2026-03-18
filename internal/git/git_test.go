package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullUsesExplicitRemoteAndBranch(t *testing.T) {
	gitLog := filepath.Join(t.TempDir(), "git.log")
	installFakeGit(t, gitLog, `#!/bin/sh
printf "%s\n" "$*" >> "$GIT_LOG"
case "$*" in
  "status --porcelain")
    exit 0
    ;;
  "pull --rebase origin main")
    echo "Already up to date."
    exit 0
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 99
    ;;
esac
`)

	g := &Git{root: t.TempDir()}

	pulled, err := g.Pull(context.Background(), "origin", "main")
	if err != nil {
		t.Fatalf("expected pull to succeed, got %v", err)
	}
	if pulled {
		t.Fatal("expected already-up-to-date pull to report no changes")
	}

	logLines := readGitLog(t, gitLog)
	if len(logLines) != 2 {
		t.Fatalf("expected two git invocations, got %d", len(logLines))
	}
	if logLines[0] != "status --porcelain" {
		t.Fatalf("unexpected dirty-check args: %q", logLines[0])
	}
	if logLines[1] != "pull --rebase origin main" {
		t.Fatalf("unexpected pull args: %q", logLines[1])
	}
}

func TestPushUsesExplicitRemoteAndBranch(t *testing.T) {
	gitLog := filepath.Join(t.TempDir(), "git.log")
	installFakeGit(t, gitLog, `#!/bin/sh
printf "%s\n" "$*" >> "$GIT_LOG"
case "$*" in
  "push origin HEAD:main")
    exit 0
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 99
    ;;
esac
`)

	g := &Git{root: t.TempDir()}

	if err := g.Push(context.Background(), "origin", "main"); err != nil {
		t.Fatalf("expected push to succeed, got %v", err)
	}

	logLines := readGitLog(t, gitLog)
	if len(logLines) != 1 {
		t.Fatalf("expected one git invocation, got %d", len(logLines))
	}
	if logLines[0] != "push origin HEAD:main" {
		t.Fatalf("unexpected push args: %q", logLines[0])
	}
}

func TestPullUnknownRemoteReturnsErrNoRemote(t *testing.T) {
	gitLog := filepath.Join(t.TempDir(), "git.log")
	installFakeGit(t, gitLog, `#!/bin/sh
printf "%s\n" "$*" >> "$GIT_LOG"
case "$*" in
  "status --porcelain")
    exit 0
    ;;
  "pull --rebase origin main")
    echo "fatal: 'origin' does not appear to be a git repository" >&2
    exit 1
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 99
    ;;
esac
`)

	g := &Git{root: t.TempDir()}

	_, err := g.Pull(context.Background(), "origin", "main")
	if !errors.Is(err, ErrNoRemote) {
		t.Fatalf("expected ErrNoRemote, got %v", err)
	}
}

func TestPushMissingBranchSurfacesPushFailure(t *testing.T) {
	gitLog := filepath.Join(t.TempDir(), "git.log")
	installFakeGit(t, gitLog, `#!/bin/sh
printf "%s\n" "$*" >> "$GIT_LOG"
case "$*" in
  "push origin HEAD:main")
    echo "error: src refspec HEAD does not match any" >&2
    exit 1
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 99
    ;;
esac
`)

	g := &Git{root: t.TempDir()}

	err := g.Push(context.Background(), "origin", "main")
	if !errors.Is(err, ErrPushFailed) {
		t.Fatalf("expected ErrPushFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "does not match any") {
		t.Fatalf("expected push failure to include git output, got %v", err)
	}
}

func installFakeGit(t *testing.T, logPath string, script string) {
	t.Helper()

	dir := t.TempDir()
	gitPath := filepath.Join(dir, "git")
	if err := os.WriteFile(gitPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake git: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_LOG", logPath)
}

func readGitLog(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read git log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
