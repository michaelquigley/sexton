package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
)

type stubGit struct {
	dirty          bool
	dirtyErr       error
	status         *git.Status
	statusErr      error
	stageErr       error
	commitErr      error
	pullErr        error
	pushErr        error
	rebaseAbortErr error
	rebaseAborts   int
	shortHEAD      string
	shortHEADErr   error
	diffStaged     string
	diffStagedErr  error
	diffStat       string
	diffStatErr    error
}

func (g *stubGit) IsDirty() (bool, error)               { return g.dirty, g.dirtyErr }
func (g *stubGit) Status() (*git.Status, error)         { return g.status, g.statusErr }
func (g *stubGit) StageAll(context.Context) error       { return g.stageErr }
func (g *stubGit) Commit(context.Context, string) error { return g.commitErr }
func (g *stubGit) Pull(context.Context) (bool, error)   { return false, g.pullErr }
func (g *stubGit) Push(context.Context) error           { return g.pushErr }
func (g *stubGit) RebaseAbort(context.Context) error    { g.rebaseAborts++; return g.rebaseAbortErr }
func (g *stubGit) ShortHEAD() (string, error)           { return g.shortHEAD, g.shortHEADErr }
func (g *stubGit) DiffStaged() (string, error)          { return g.diffStaged, g.diffStagedErr }
func (g *stubGit) DiffStat() (string, error)            { return g.diffStat, g.diffStatErr }

type recordingAlerter struct {
	events []AlertEvent
}

func (a *recordingAlerter) Alert(_ context.Context, event AlertEvent) error {
	a.events = append(a.events, event)
	return nil
}

func newAgentForTest(g gitClient, alerter Alerter) *Agent {
	if alerter == nil {
		alerter = &recordingAlerter{}
	}

	return &Agent{
		cfg: &config.ResolvedRepo{
			Path:         "/tmp/test-repo",
			Name:         "test-repo",
			PollInterval: time.Second,
			Hooks:        &config.ResolvedHooks{},
		},
		git:     g,
		alerter: alerter,
		state:   Watching,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		syncCh:  make(chan struct{}, 1),
	}
}

func TestSyncFailureSetsErrorAndSuccessClearsIt(t *testing.T) {
	g := &stubGit{
		shortHEAD: "abc123",
		pullErr:   errors.New("network down"),
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)

	a.sync()

	if got := a.State(); got != Error {
		t.Fatalf("expected error state, got %s", got)
	}
	if got := a.ErrorDetail(); got != "pull failed: network down" {
		t.Fatalf("expected stored error detail, got %q", got)
	}
	if len(alerts.events) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts.events))
	}

	g.pullErr = nil
	a.sync()

	if got := a.State(); got != Watching {
		t.Fatalf("expected watching state, got %s", got)
	}
	if got := a.ErrorDetail(); got != "" {
		t.Fatalf("expected cleared error detail, got %q", got)
	}
	if got := a.LastCommit(); got != "abc123" {
		t.Fatalf("expected last commit abc123, got %q", got)
	}
	if a.LastSync().IsZero() {
		t.Fatal("expected last sync to be recorded")
	}
	if len(alerts.events) != 1 {
		t.Fatalf("expected no extra alerts after recovery, got %d", len(alerts.events))
	}
}

func TestSyncRepeatedSameErrorDoesNotReAlert(t *testing.T) {
	g := &stubGit{pullErr: errors.New("network down")}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)

	a.sync()
	a.sync()

	if len(alerts.events) != 1 {
		t.Fatalf("expected one deduplicated alert, got %d", len(alerts.events))
	}
}

func TestTriggerSyncAllowsErrorButNotSnoozed(t *testing.T) {
	a := newAgentForTest(&stubGit{}, nil)
	a.state = Error

	if err := a.TriggerSync(); err != nil {
		t.Fatalf("expected sync trigger in error state, got %v", err)
	}
	select {
	case <-a.syncCh:
	default:
		t.Fatal("expected sync request to be queued")
	}

	a.state = Snoozed
	if err := a.TriggerSync(); err == nil {
		t.Fatal("expected snoozed trigger to fail")
	}
}

func TestSnoozeAllowsErrorAndPreservesErrorDetail(t *testing.T) {
	a := newAgentForTest(&stubGit{}, nil)
	a.state = Error
	a.errorDetail = "pull failed: network down"

	if _, err := a.Snooze(time.Minute); err != nil {
		t.Fatalf("expected snooze to succeed from error state, got %v", err)
	}
	if got := a.State(); got != Snoozed {
		t.Fatalf("expected snoozed state, got %s", got)
	}
	if got := a.ErrorDetail(); got != "pull failed: network down" {
		t.Fatalf("expected error detail to remain visible, got %q", got)
	}
}

func TestResumeClearsErrorAndQueuesRetry(t *testing.T) {
	a := newAgentForTest(&stubGit{}, nil)
	a.state = Error
	a.errorDetail = "push failed: rejected"

	if err := a.Resume(); err != nil {
		t.Fatalf("expected resume to succeed, got %v", err)
	}
	if got := a.State(); got != Watching {
		t.Fatalf("expected watching state, got %s", got)
	}
	if got := a.ErrorDetail(); got != "" {
		t.Fatalf("expected cleared error detail, got %q", got)
	}
	select {
	case <-a.syncCh:
	default:
		t.Fatal("expected resume to queue a retry")
	}
}

func TestSetErrorDeduplicatesUntilRecovery(t *testing.T) {
	alerts := &recordingAlerter{}
	a := newAgentForTest(&stubGit{}, alerts)

	errA := errors.New("network down")
	errB := errors.New("push rejected")

	a.setError("pull failed", errA)
	a.setError("pull failed", errA)
	a.setError("push failed", errB)

	if len(alerts.events) != 2 {
		t.Fatalf("expected alert on first error and changed error, got %d", len(alerts.events))
	}

	a.mu.Lock()
	a.state = Watching
	a.errorDetail = ""
	a.mu.Unlock()

	a.setError("pull failed", errA)

	if len(alerts.events) != 3 {
		t.Fatalf("expected alert after recovery, got %d", len(alerts.events))
	}
}
