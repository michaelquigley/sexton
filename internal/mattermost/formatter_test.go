package mattermost

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/sexton/internal/agent"
)

func TestFormatAlertInfo(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "info",
		RepoPath: "my-notes",
		Message:  "sync complete (abc123)",
	}
	got := FormatAlert(event)
	if !strings.Contains(got, "**info**") {
		t.Errorf("expected info severity, got %q", got)
	}
	if !strings.Contains(got, "[my-notes]") {
		t.Errorf("expected repo path, got %q", got)
	}
	if !strings.Contains(got, "sync complete (abc123)") {
		t.Errorf("expected message, got %q", got)
	}
}

func TestFormatAlertWithFiles(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "info",
		RepoPath: "my-notes",
		Message:  "sync complete (abc123)",
		Files: &agent.AlertFiles{
			Modified: []string{"notes/todo.md", "notes/ideas.md"},
			Added:    []string{"notes/new.md"},
			Deleted:  []string{"notes/old.md"},
		},
	}
	got := FormatAlert(event)
	if !strings.Contains(got, "`notes/todo.md`") {
		t.Errorf("expected modified file, got %q", got)
	}
	if !strings.Contains(got, "- modified:") {
		t.Errorf("expected modified label, got %q", got)
	}
	if !strings.Contains(got, "- added: `notes/new.md`") {
		t.Errorf("expected added file, got %q", got)
	}
	if !strings.Contains(got, "- deleted: `notes/old.md`") {
		t.Errorf("expected deleted file, got %q", got)
	}
}

func TestFormatAlertWithFilesPartial(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "info",
		RepoPath: "my-notes",
		Message:  "sync complete (abc123)",
		Files: &agent.AlertFiles{
			Modified: []string{"notes/todo.md"},
		},
	}
	got := FormatAlert(event)
	if !strings.Contains(got, "- modified:") {
		t.Errorf("expected modified label, got %q", got)
	}
	if strings.Contains(got, "- added:") {
		t.Errorf("should not contain added section, got %q", got)
	}
	if strings.Contains(got, "- deleted:") {
		t.Errorf("should not contain deleted section, got %q", got)
	}
}

func TestFormatAlertError(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "error",
		RepoPath: "my-notes",
		Message:  "pull failed",
		Error:    errors.New("conflict detected"),
	}
	got := FormatAlert(event)
	if !strings.Contains(got, "**error**") {
		t.Errorf("expected error severity, got %q", got)
	}
	if !strings.Contains(got, "conflict detected") {
		t.Errorf("expected error detail, got %q", got)
	}
}

func TestFormatStatusEmpty(t *testing.T) {
	got := FormatStatus(nil)
	if got != "no repos configured" {
		t.Errorf("expected empty message, got %q", got)
	}
}

func TestFormatStatusTable(t *testing.T) {
	statuses := []RepoStatus{
		{
			Name:     "notes",
			State:    "watching",
			Branch:   "main",
			LastSync: time.Now().Add(-5 * time.Minute),
		},
		{
			Name:            "config",
			State:           "snoozed",
			Branch:          "main",
			SnoozeRemaining: 30 * time.Minute,
		},
	}
	got := FormatStatus(statuses)
	if !strings.Contains(got, "| notes |") {
		t.Errorf("expected notes row, got %q", got)
	}
	if !strings.Contains(got, "snoozed (30m0s left)") {
		t.Errorf("expected snooze remaining, got %q", got)
	}
	if !strings.Contains(got, "5m ago") {
		t.Errorf("expected human-friendly duration, got %q", got)
	}
}

func TestFormatAlertWithCommitMessage(t *testing.T) {
	event := agent.AlertEvent{
		Severity:      "info",
		RepoPath:      "my-notes",
		Message:        "sync complete (abc123)",
		CommitMessage: "add pane design spec and update project index",
	}
	got := FormatAlert(event)
	if !strings.Contains(got, "> add pane design spec and update project index") {
		t.Errorf("expected commit message in blockquote, got %q", got)
	}
}

func TestFormatSyncResponse(t *testing.T) {
	got := FormatSyncResponse("my-notes")
	if got != "sync triggered for 'my-notes'" {
		t.Errorf("got %q", got)
	}
}

func TestFormatSnoozeResponse(t *testing.T) {
	until := time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)
	got := FormatSnoozeResponse("my-notes", until)
	if !strings.Contains(got, "snoozed 'my-notes'") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "2026-03-25T14:00:00Z") {
		t.Errorf("expected RFC3339 time, got %q", got)
	}
}

func TestFormatResumeResponse(t *testing.T) {
	got := FormatResumeResponse("my-notes")
	if got != "resumed 'my-notes'" {
		t.Errorf("got %q", got)
	}
}

func TestFormatError(t *testing.T) {
	got := FormatError(errors.New("something broke"))
	if got != "error: something broke" {
		t.Errorf("got %q", got)
	}
}

func TestFormatHelp(t *testing.T) {
	got := FormatHelp()
	if !strings.Contains(got, "status") {
		t.Errorf("expected status command in help, got %q", got)
	}
	if !strings.Contains(got, "sync") {
		t.Errorf("expected sync command in help, got %q", got)
	}
}
