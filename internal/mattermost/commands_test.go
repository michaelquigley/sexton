package mattermost

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type mockHandler struct {
	statusFn func(repo string) ([]RepoStatus, error)
	syncFn   func(repo string) error
	snoozeFn func(repo string, d time.Duration) (time.Time, error)
	resumeFn func(repo string) (string, error)
}

func (m *mockHandler) Status(repo string) ([]RepoStatus, error) {
	if m.statusFn != nil {
		return m.statusFn(repo)
	}
	return nil, nil
}

func (m *mockHandler) Sync(repo string) error {
	if m.syncFn != nil {
		return m.syncFn(repo)
	}
	return nil
}

func (m *mockHandler) Snooze(repo string, d time.Duration) (time.Time, error) {
	if m.snoozeFn != nil {
		return m.snoozeFn(repo, d)
	}
	return time.Now().Add(d), nil
}

func (m *mockHandler) Resume(repo string) (string, error) {
	if m.resumeFn != nil {
		return m.resumeFn(repo)
	}
	return "resumed", nil
}

func TestDispatchEmpty(t *testing.T) {
	resp, ok := Dispatch("", &mockHandler{})
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(resp, "available commands") {
		t.Errorf("expected help text, got %q", resp)
	}
}

func TestDispatchHelp(t *testing.T) {
	resp, ok := Dispatch("help", &mockHandler{})
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(resp, "available commands") {
		t.Errorf("expected help text, got %q", resp)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	resp, ok := Dispatch("foo", &mockHandler{})
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(resp, "unknown command 'foo'") {
		t.Errorf("expected unknown command error, got %q", resp)
	}
	if !strings.Contains(resp, "available commands") {
		t.Errorf("expected help text appended, got %q", resp)
	}
}

func TestDispatchStatusAll(t *testing.T) {
	h := &mockHandler{
		statusFn: func(repo string) ([]RepoStatus, error) {
			if repo != "" {
				t.Errorf("expected empty repo for all status, got %q", repo)
			}
			return []RepoStatus{{Name: "notes", State: "watching", Branch: "main"}}, nil
		},
	}
	resp, ok := Dispatch("status", h)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(resp, "notes") {
		t.Errorf("expected notes in status, got %q", resp)
	}
}

func TestDispatchStatusSpecificRepo(t *testing.T) {
	h := &mockHandler{
		statusFn: func(repo string) ([]RepoStatus, error) {
			if repo != "notes" {
				t.Errorf("expected repo 'notes', got %q", repo)
			}
			return []RepoStatus{{Name: "notes", State: "watching", Branch: "main"}}, nil
		},
	}
	resp, _ := Dispatch("status notes", h)
	if !strings.Contains(resp, "notes") {
		t.Errorf("expected notes in status, got %q", resp)
	}
}

func TestDispatchStatusError(t *testing.T) {
	h := &mockHandler{
		statusFn: func(repo string) ([]RepoStatus, error) {
			return nil, errors.New("repo 'foo' not found")
		},
	}
	resp, _ := Dispatch("status foo", h)
	if !strings.Contains(resp, "error:") {
		t.Errorf("expected error response, got %q", resp)
	}
}

func TestDispatchSync(t *testing.T) {
	var syncedRepo string
	h := &mockHandler{
		syncFn: func(repo string) error {
			syncedRepo = repo
			return nil
		},
	}
	resp, _ := Dispatch("sync notes", h)
	if syncedRepo != "notes" {
		t.Errorf("expected sync for 'notes', got %q", syncedRepo)
	}
	if !strings.Contains(resp, "sync triggered for 'notes'") {
		t.Errorf("expected confirmation, got %q", resp)
	}
}

func TestDispatchSyncMissingRepo(t *testing.T) {
	resp, _ := Dispatch("sync", &mockHandler{})
	if !strings.Contains(resp, "sync requires a repo argument") {
		t.Errorf("expected missing arg error, got %q", resp)
	}
}

func TestDispatchSyncError(t *testing.T) {
	h := &mockHandler{
		syncFn: func(repo string) error {
			return errors.New("agent is snoozed")
		},
	}
	resp, _ := Dispatch("sync notes", h)
	if !strings.Contains(resp, "error:") {
		t.Errorf("expected error, got %q", resp)
	}
}

func TestDispatchSnooze(t *testing.T) {
	h := &mockHandler{
		snoozeFn: func(repo string, d time.Duration) (time.Time, error) {
			if repo != "notes" {
				t.Errorf("expected 'notes', got %q", repo)
			}
			if d != 30*time.Minute {
				t.Errorf("expected 30m, got %v", d)
			}
			return time.Date(2026, 3, 25, 14, 30, 0, 0, time.UTC), nil
		},
	}
	resp, _ := Dispatch("snooze notes 30m", h)
	if !strings.Contains(resp, "snoozed 'notes'") {
		t.Errorf("expected snooze confirmation, got %q", resp)
	}
}

func TestDispatchSnoozeMissingArgs(t *testing.T) {
	resp, _ := Dispatch("snooze", &mockHandler{})
	if !strings.Contains(resp, "snooze requires a repo and duration argument") {
		t.Errorf("expected missing args error, got %q", resp)
	}
}

func TestDispatchSnoozeMissingDuration(t *testing.T) {
	resp, _ := Dispatch("snooze notes", &mockHandler{})
	if !strings.Contains(resp, "snooze requires a repo and duration argument") {
		t.Errorf("expected missing args error, got %q", resp)
	}
}

func TestDispatchSnoozeInvalidDuration(t *testing.T) {
	resp, _ := Dispatch("snooze notes xyz", &mockHandler{})
	if !strings.Contains(resp, "invalid duration 'xyz'") {
		t.Errorf("expected invalid duration error, got %q", resp)
	}
}

func TestDispatchResume(t *testing.T) {
	var resumedRepo string
	h := &mockHandler{
		resumeFn: func(repo string) (string, error) {
			resumedRepo = repo
			return "resumed", nil
		},
	}
	resp, _ := Dispatch("resume notes", h)
	if resumedRepo != "notes" {
		t.Errorf("expected resume for 'notes', got %q", resumedRepo)
	}
	if !strings.Contains(resp, "resumed 'notes'") {
		t.Errorf("expected confirmation, got %q", resp)
	}
}

func TestDispatchResumeUsesCustomMessage(t *testing.T) {
	h := &mockHandler{
		resumeFn: func(repo string) (string, error) {
			return "holdout remains active until 2026-04-03T11:00:00-04:00", nil
		},
	}
	resp, _ := Dispatch("resume notes", h)
	if !strings.Contains(resp, "holdout remains active until") {
		t.Errorf("expected holdout message, got %q", resp)
	}
}

func TestDispatchResumeMissingRepo(t *testing.T) {
	resp, _ := Dispatch("resume", &mockHandler{})
	if !strings.Contains(resp, "resume requires a repo argument") {
		t.Errorf("expected missing arg error, got %q", resp)
	}
}

func TestDispatchCaseInsensitive(t *testing.T) {
	h := &mockHandler{
		statusFn: func(repo string) ([]RepoStatus, error) {
			return nil, nil
		},
	}
	resp, ok := Dispatch("STATUS", h)
	if !ok {
		t.Fatal("expected ok")
	}
	if strings.Contains(resp, "unknown command") {
		t.Errorf("expected case insensitive match, got %q", resp)
	}
}

func TestStripTriggerWordMatch(t *testing.T) {
	rest, ok := StripTriggerWord("sexton status", []string{"sexton"})
	if !ok {
		t.Fatal("expected match")
	}
	if rest != "status" {
		t.Errorf("expected 'status', got %q", rest)
	}
}

func TestStripTriggerWordCaseInsensitive(t *testing.T) {
	rest, ok := StripTriggerWord("SEXTON status", []string{"sexton"})
	if !ok {
		t.Fatal("expected match")
	}
	if rest != "status" {
		t.Errorf("expected 'status', got %q", rest)
	}
}

func TestStripTriggerWordBare(t *testing.T) {
	rest, ok := StripTriggerWord("sexton", []string{"sexton"})
	if !ok {
		t.Fatal("expected match")
	}
	if rest != "" {
		t.Errorf("expected empty, got %q", rest)
	}
}

func TestStripTriggerWordNoMatch(t *testing.T) {
	_, ok := StripTriggerWord("other status", []string{"sexton"})
	if ok {
		t.Fatal("expected no match")
	}
}

func TestStripTriggerWordPartialNoMatch(t *testing.T) {
	_, ok := StripTriggerWord("sextonbot status", []string{"sexton"})
	if ok {
		t.Fatal("expected no match for partial word")
	}
}

func TestStripTriggerWordMultiple(t *testing.T) {
	rest, ok := StripTriggerWord("bot sync notes", []string{"sexton", "bot"})
	if !ok {
		t.Fatal("expected match")
	}
	if rest != "sync notes" {
		t.Errorf("expected 'sync notes', got %q", rest)
	}
}
