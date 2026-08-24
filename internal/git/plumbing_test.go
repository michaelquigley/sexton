package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestIsDirtyTrackedIgnoresUntrackedAndDetectsIndexOrWorktreeDirt(t *testing.T) {
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "tracked.md", "base\n")
	commitGitTestRepo(t, root, "base")

	writeGitTestFile(t, root, "untracked.md", "new\n")
	if dirty, err := g.IsDirtyTracked(context.Background()); err != nil || dirty {
		t.Fatalf("untracked-only IsDirtyTracked() = %v, %v; want false, nil", dirty, err)
	}
	if err := os.Remove(filepath.Join(root, "untracked.md")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	writeGitTestFile(t, root, "tracked.md", "worktree\n")
	if dirty, err := g.IsDirtyTracked(context.Background()); err != nil || !dirty {
		t.Fatalf("worktree IsDirtyTracked() = %v, %v; want true, nil", dirty, err)
	}

	writeGitTestFile(t, root, "tracked.md", "staged\n")
	runGitTest(t, root, "add", "tracked.md")
	if dirty, err := g.IsDirtyTracked(context.Background()); err != nil || !dirty {
		t.Fatalf("index IsDirtyTracked() = %v, %v; want true, nil", dirty, err)
	}
}

func TestPullRefusesTrackedWorktreeAndIndexDirt(t *testing.T) {
	for _, statusLine := range []string{" M notes.md", "M  notes.md"} {
		t.Run(strings.ReplaceAll(statusLine, " ", "_"), func(t *testing.T) {
			gitLog := filepath.Join(t.TempDir(), "git.log")
			script := `#!/bin/sh
printf "%s\n" "$*" >> "$GIT_LOG"
case "$*" in
  "status --porcelain -uno")
    printf '%s\n'
    exit 0
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 99
    ;;
esac
`
			script = strings.Replace(script, "printf '%s\\n'", "printf '%s\\n' '"+statusLine+"'", 1)
			installFakeGit(t, gitLog, script)

			_, err := (&Git{root: t.TempDir()}).Pull(context.Background(), "origin", "main")
			if !errors.Is(err, ErrDirtyWorkingTree) {
				t.Fatalf("Pull() error = %v, want ErrDirtyWorkingTree", err)
			}
			if got := readGitLog(t, gitLog); !reflect.DeepEqual(got, []string{"status --porcelain -uno"}) {
				t.Fatalf("git invocations = %#v, want only the tracked-dirt preflight", got)
			}
		})
	}
}

func TestStageRegionsStagesOnlyLiteralRegionContent(t *testing.T) {
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "journal/modified.md", "base\n")
	writeGitTestFile(t, root, "journal/deleted.md", "base\n")
	writeGitTestFile(t, root, "outside/modified.md", "base\n")
	commitGitTestRepo(t, root, "base")

	writeGitTestFile(t, root, "journal/modified.md", "changed\n")
	if err := os.Remove(filepath.Join(root, "journal/deleted.md")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	writeGitTestFile(t, root, "journal/added.md", "new\n")
	writeGitTestFile(t, root, "outside/modified.md", "changed\n")
	writeGitTestFile(t, root, "outside/added.md", "new\n")

	if err := g.StageRegions(context.Background(), []string{"journal/"}); err != nil {
		t.Fatalf("StageRegions() error = %v", err)
	}
	assertGitNames(t, root, []string{"journal/added.md", "journal/deleted.md", "journal/modified.md"}, "diff", "--cached", "--name-only")
	assertGitNames(t, root, []string{"outside/modified.md"}, "diff", "--name-only")
	assertGitNames(t, root, []string{"outside/added.md"}, "ls-files", "--others", "--exclude-standard")
}

func TestStageRegionsTreatsMetacharactersLiterally(t *testing.T) {
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "notes[1]/entry.md", "base\n")
	writeGitTestFile(t, root, "notes1/entry.md", "base\n")
	commitGitTestRepo(t, root, "base")
	writeGitTestFile(t, root, "notes[1]/entry.md", "literal\n")
	writeGitTestFile(t, root, "notes1/entry.md", "glob\n")

	if err := g.StageRegions(context.Background(), []string{"notes[1]/"}); err != nil {
		t.Fatalf("StageRegions() error = %v", err)
	}
	assertGitNames(t, root, []string{"notes[1]/entry.md"}, "diff", "--cached", "--name-only")
}

func TestCommitOnlyLeavesOutsideIndexEntriesUntouched(t *testing.T) {
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "journal/entry.md", "base\n")
	writeGitTestFile(t, root, "outside/work.md", "base\n")
	commitGitTestRepo(t, root, "base")
	writeGitTestFile(t, root, "journal/entry.md", "journal change\n")
	writeGitTestFile(t, root, "journal/new.md", "new journal content\n")
	writeGitTestFile(t, root, "outside/work.md", "outside change\n")
	runGitTest(t, root, "add", "-A")

	sha, err := g.CommitOnly(context.Background(), "partial", []string{"journal/"})
	if err != nil {
		t.Fatalf("CommitOnly() error = %v", err)
	}
	if head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD")); sha != head {
		t.Fatalf("CommitOnly() sha = %q, HEAD = %q", sha, head)
	}
	assertGitNames(t, root, []string{"journal/entry.md", "journal/new.md"}, "show", "--format=", "--name-only", sha)
	assertGitNames(t, root, []string{"outside/work.md"}, "diff", "--cached", "--name-only")
	if got := runGitTest(t, root, "show", "HEAD:outside/work.md"); got != "base\n" {
		t.Fatalf("outside file in commit = %q, want base content", got)
	}
}

func TestCommitOnlySplitsStagedCrossBoundaryRenameByTreeEndpoint(t *testing.T) {
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "journal/move.md", "move me\n")
	writeGitTestFile(t, root, "journal/other.md", "base\n")
	commitGitTestRepo(t, root, "base")
	if err := os.MkdirAll(filepath.Join(root, "drafts"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGitTest(t, root, "mv", "journal/move.md", "drafts/move.md")
	writeGitTestFile(t, root, "journal/other.md", "changed\n")

	if err := g.StageRegions(context.Background(), []string{"journal/"}); err != nil {
		t.Fatalf("StageRegions() error = %v", err)
	}
	sha, err := g.CommitOnly(context.Background(), "region change", []string{"journal/"})
	if err != nil {
		t.Fatalf("CommitOnly() error = %v", err)
	}
	assertGitNames(t, root, []string{"journal/move.md", "journal/other.md"}, "show", "--format=", "--name-only", sha)
	assertGitNames(t, root, []string{"drafts/move.md"}, "diff", "--cached", "--name-only")
	assertGitNames(t, root, []string{"drafts/move.md", "journal/other.md"}, "ls-files")
}

func TestShowReadsMergeAgainstFirstParent(t *testing.T) {
	g, root, mergeSHA := newMergeGitTestRepo(t)

	patch, err := g.Show(context.Background(), mergeSHA)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if !strings.Contains(patch, "-base") || !strings.Contains(patch, "+topic") {
		t.Fatalf("Show() patch does not describe the first-parent change:\n%s", patch)
	}
	if strings.Contains(patch, "merge topic") {
		t.Fatalf("Show() included the commit header:\n%s", patch)
	}

	stat, err := g.ShowStat(context.Background(), mergeSHA)
	if err != nil {
		t.Fatalf("ShowStat() error = %v", err)
	}
	if !strings.Contains(stat, "story.md") {
		t.Fatalf("ShowStat() = %q, want story.md", stat)
	}

	status, err := g.ShowNameStatus(context.Background(), mergeSHA)
	if err != nil {
		t.Fatalf("ShowNameStatus() error = %v", err)
	}
	if !reflect.DeepEqual(status.Modified, []string{"story.md"}) {
		t.Fatalf("ShowNameStatus() modified = %#v, want story.md", status.Modified)
	}
	_ = root
}

func TestShowNameStatusPreservesHostileCommittedFilename(t *testing.T) {
	g, root := newGitTestRepo(t)
	name := "odd -> quote\" back\\slash\nname.md"
	writeGitTestFile(t, root, name, "content\n")
	if err := g.StageAll(context.Background()); err != nil {
		t.Fatalf("StageAll() error = %v", err)
	}
	sha, err := g.Commit(context.Background(), "hostile path")
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	status, err := g.ShowNameStatus(context.Background(), sha)
	if err != nil {
		t.Fatalf("ShowNameStatus() error = %v", err)
	}
	if !reflect.DeepEqual(status.Added, []string{strconv.Quote(name)}) {
		t.Fatalf("added = %#v, want escaped filename %#v", status.Added, []string{strconv.Quote(name)})
	}
	if len(status.Entries) != 1 || status.Entries[0].Path != name {
		t.Fatalf("entries = %#v, want exact raw filename", status.Entries)
	}
}

func TestRewordCommitRefusesWhenBranchMoved(t *testing.T) {
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "first.md", "first\n")
	if err := g.StageAll(context.Background()); err != nil {
		t.Fatalf("StageAll() error = %v", err)
	}
	oldSHA, err := g.Commit(context.Background(), "placeholder")
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	writeGitTestFile(t, root, "operator.md", "operator\n")
	commitGitTestRepo(t, root, "operator commit")
	operatorSHA := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	if err := g.RewordCommit(context.Background(), "main", oldSHA, "final message"); err == nil {
		t.Fatal("RewordCommit() error = nil, want compare-and-swap refusal")
	}
	if head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD")); head != operatorSHA {
		t.Fatalf("HEAD = %q, want operator commit %q", head, operatorSHA)
	}
	if message := strings.TrimSpace(runGitTest(t, root, "show", "-s", "--format=%B", oldSHA)); message != "placeholder" {
		t.Fatalf("old commit message = %q, want placeholder", message)
	}
	if message := strings.TrimSpace(runGitTest(t, root, "show", "-s", "--format=%B", operatorSHA)); message != "operator commit" {
		t.Fatalf("operator commit message = %q", message)
	}
}

func TestRewordCommitPreservesTreeParentsAuthorAndIndex(t *testing.T) {
	g, root, oldSHA := newMergeGitTestRepo(t)
	writeGitTestFile(t, root, "outside.md", "staged later\n")
	runGitTest(t, root, "add", "outside.md")
	oldMetadata := strings.TrimSpace(runGitTest(t, root, "show", "-s", "--format=%T%x00%P%x00%an%x00%ae%x00%at%x00%ai", oldSHA))

	if err := g.RewordCommit(context.Background(), "main", oldSHA, "final merge message"); err != nil {
		t.Fatalf("RewordCommit() error = %v", err)
	}
	newSHA := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	if newSHA == oldSHA {
		t.Fatal("RewordCommit() left HEAD at the old object")
	}
	newMetadata := strings.TrimSpace(runGitTest(t, root, "show", "-s", "--format=%T%x00%P%x00%an%x00%ae%x00%at%x00%ai", newSHA))
	if newMetadata != oldMetadata {
		t.Fatalf("replacement metadata changed\nold: %q\nnew: %q", oldMetadata, newMetadata)
	}
	if message := strings.TrimSpace(runGitTest(t, root, "show", "-s", "--format=%B", newSHA)); message != "final merge message" {
		t.Fatalf("replacement message = %q", message)
	}
	assertGitNames(t, root, []string{"outside.md"}, "diff", "--cached", "--name-only")
}

func newGitTestRepo(t *testing.T) (*Git, string) {
	t.Helper()
	root := t.TempDir()
	runGitTest(t, root, "init", "-q", "-b", "main")
	runGitTest(t, root, "config", "user.name", "Test User")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "commit.gpgSign", "false")
	return &Git{root: root}, root
}

func newMergeGitTestRepo(t *testing.T) (*Git, string, string) {
	t.Helper()
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "story.md", "base\n")
	commitGitTestRepo(t, root, "base")
	runGitTest(t, root, "checkout", "-q", "-b", "topic")
	writeGitTestFile(t, root, "story.md", "topic\n")
	commitGitTestRepo(t, root, "topic")
	runGitTest(t, root, "checkout", "-q", "main")
	runGitTestEnv(t, root, []string{
		"GIT_AUTHOR_NAME=Original Author",
		"GIT_AUTHOR_EMAIL=original@example.com",
		"GIT_AUTHOR_DATE=2001-02-03T04:05:06-0700",
	}, "merge", "--no-ff", "topic", "-m", "merge topic")
	return g, root, strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
}

func commitGitTestRepo(t *testing.T, root, message string) {
	t.Helper()
	runGitTest(t, root, "add", "-A")
	runGitTest(t, root, "commit", "-q", "-m", message)
}

func writeGitTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func runGitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	return runGitTestEnv(t, root, nil, args...)
}

func runGitTestEnv(t *testing.T, root string, extraEnv []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s error = %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func assertGitNames(t *testing.T, root string, want []string, args ...string) {
	t.Helper()
	output := strings.TrimSpace(runGitTest(t, root, args...))
	var got []string
	if output != "" {
		got = strings.Split(output, "\n")
	}
	sort.Strings(got)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("git %s names = %#v, want %#v", strings.Join(args, " "), got, want)
	}
}
