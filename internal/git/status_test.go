package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestStatusUsesNulDelimitedPorcelain(t *testing.T) {
	gitLog := filepath.Join(t.TempDir(), "git.log")
	installFakeGit(t, gitLog, `#!/bin/sh
printf "%s\n" "$*" >> "$GIT_LOG"
case "$*" in
  "status --porcelain=v1 -z -b -uall")
    printf '## main\000?? file.txt\000'
    exit 0
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 99
    ;;
esac
`)

	status, err := (&Git{root: t.TempDir()}).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Branch != "main" || !reflect.DeepEqual(status.Untracked, []string{"file.txt"}) {
		t.Fatalf("status = %#v, want main with one untracked file", status)
	}
	if got := readGitLog(t, gitLog); !reflect.DeepEqual(got, []string{"status --porcelain=v1 -z -b -uall"}) {
		t.Fatalf("git invocations = %#v", got)
	}
}

func TestParseStatusPreservesHostileFilenamesAndRenameEndpoints(t *testing.T) {
	output := "## main...origin/main [ahead 2, behind 1]\x00" +
		" M space name.md\x00" +
		"?? quote\"name.md\x00" +
		" M back\\slash.md\x00" +
		"?? line\nbreak.md\x00" +
		" M literal -> arrow.md\x00" +
		"R  journal/new.md\x00drafts/old.md\x00" +
		"R  drafts/new.md\x00journal/old.md\x00"

	status := parseStatus(output)
	if status.Branch != "main" || status.Ahead != 2 || status.Behind != 1 {
		t.Fatalf("branch status = %q ahead %d behind %d", status.Branch, status.Ahead, status.Behind)
	}
	wantEntries := []StatusEntry{
		{Path: "space name.md", X: ' ', Y: 'M'},
		{Path: "quote\"name.md", X: '?', Y: '?'},
		{Path: "back\\slash.md", X: ' ', Y: 'M'},
		{Path: "line\nbreak.md", X: '?', Y: '?'},
		{Path: "literal -> arrow.md", X: ' ', Y: 'M'},
		{Path: "journal/new.md", OldPath: "drafts/old.md", X: 'R', Y: ' '},
		{Path: "drafts/new.md", OldPath: "journal/old.md", X: 'R', Y: ' '},
	}
	if !reflect.DeepEqual(status.Entries, wantEntries) {
		t.Fatalf("entries = %#v, want %#v", status.Entries, wantEntries)
	}
	if !reflect.DeepEqual(status.Untracked, []string{"quote\"name.md", strconv.Quote("line\nbreak.md")}) {
		t.Fatalf("untracked = %#v", status.Untracked)
	}
	if !reflect.DeepEqual(status.Modified, []string{"space name.md", "back\\slash.md", "literal -> arrow.md", "journal/new.md", "drafts/new.md"}) {
		t.Fatalf("modified = %#v", status.Modified)
	}

	wantTracked := []string{
		"space name.md",
		"back\\slash.md",
		"literal -> arrow.md",
		"drafts/old.md", "journal/new.md",
		"journal/old.md", "drafts/new.md",
	}
	if got := status.TrackedPaths(); !reflect.DeepEqual(got, wantTracked) {
		t.Fatalf("TrackedPaths() = %#v, want %#v", got, wantTracked)
	}
}

func TestStatusReadsBothDirectionsOfStagedCrossBoundaryRenames(t *testing.T) {
	g, root := newGitTestRepo(t)
	writeGitTestFile(t, root, "journal/moving-out.md", "outward content\n")
	writeGitTestFile(t, root, "drafts/moving-in.md", "inward content\n")
	commitGitTestRepo(t, root, "base")
	if err := os.MkdirAll(filepath.Join(root, "journal", "incoming"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "drafts", "outgoing"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGitTest(t, root, "mv", "journal/moving-out.md", "drafts/outgoing/moving-out.md")
	runGitTest(t, root, "mv", "drafts/moving-in.md", "journal/incoming/moving-in.md")

	status, err := g.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	want := []StatusEntry{
		{Path: "drafts/outgoing/moving-out.md", OldPath: "journal/moving-out.md", X: 'R', Y: ' '},
		{Path: "journal/incoming/moving-in.md", OldPath: "drafts/moving-in.md", X: 'R', Y: ' '},
	}
	if !reflect.DeepEqual(status.Entries, want) {
		t.Fatalf("entries = %#v, want %#v", status.Entries, want)
	}
}

func TestStatusEntryClassifiers(t *testing.T) {
	for _, code := range []byte{'M', 'A', 'D', 'R', 'C', 'T'} {
		entry := StatusEntry{X: code, Y: ' '}
		if !entry.IsTracked() {
			t.Errorf("entry %c IsTracked() = false, want true", code)
		}
		if !entry.IsStaged() {
			t.Errorf("entry %c IsStaged() = false, want true", code)
		}
	}
	if (StatusEntry{X: ' ', Y: 'M'}).IsStaged() {
		t.Fatal("worktree-only modification reported staged")
	}
	if (StatusEntry{X: '?', Y: '?'}).IsTracked() {
		t.Fatal("untracked entry reported tracked")
	}
}

func TestParseNameStatusConsumesRenameAndCopyRecords(t *testing.T) {
	status, err := parseNameStatus("\nR100\x00old.md\x00renamed.md\x00C75\x00source.md\x00copy.md\x00M\x00changed.md\x00")
	if err != nil {
		t.Fatalf("parseNameStatus() error = %v", err)
	}
	wantEntries := []StatusEntry{
		{Path: "renamed.md", OldPath: "old.md", X: 'R', Y: ' '},
		{Path: "copy.md", OldPath: "source.md", X: 'C', Y: ' '},
		{Path: "changed.md", X: 'M', Y: ' '},
	}
	if !reflect.DeepEqual(status.Entries, wantEntries) {
		t.Fatalf("entries = %#v, want %#v", status.Entries, wantEntries)
	}
	if !reflect.DeepEqual(status.Modified, []string{"renamed.md", "copy.md", "changed.md"}) {
		t.Fatalf("modified destinations = %#v", status.Modified)
	}
}

func TestTrackedPathsIncludesWorktreeRenameOldPath(t *testing.T) {
	status := parseStatus("## main\x00 R renamed.md\x00old.md\x00")
	want := []string{"old.md", "renamed.md"}
	if got := status.TrackedPaths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedPaths() = %#v, want %#v", got, want)
	}
}
