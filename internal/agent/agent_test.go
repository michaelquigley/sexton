package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
	"github.com/michaelquigley/sexton/internal/llm"
)

type stubGit struct {
	branch         string
	branchErr      error
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
	stageCalls     int
	commitCalls    int
	pullCalls      int
	pushCalls      int
	shortHEADCalls int
	shortHEAD      string
	shortHEADErr   error
	commitTime     time.Time
	commitTimeErr  error
	diffStaged     string
	diffStagedErr  error
	diffStat       string
	diffStatErr    error
	onIsDirty      func(context.Context)
	onStageAll     func(context.Context)
	onCommit       func(context.Context)
	onPull         func(context.Context)
	onPush         func(context.Context)
	onShortHEAD    func(context.Context)
}

func (g *stubGit) Branch(context.Context) (string, error) { return g.branch, g.branchErr }
func (g *stubGit) IsDirty(ctx context.Context) (bool, error) {
	if g.onIsDirty != nil {
		g.onIsDirty(ctx)
	}
	return g.dirty, g.dirtyErr
}
func (g *stubGit) Status(context.Context) (*git.Status, error) { return g.status, g.statusErr }
func (g *stubGit) StageAll(ctx context.Context) error {
	g.stageCalls++
	if g.onStageAll != nil {
		g.onStageAll(ctx)
	}
	return g.stageErr
}
func (g *stubGit) Commit(ctx context.Context, _ string) error {
	g.commitCalls++
	if g.onCommit != nil {
		g.onCommit(ctx)
	}
	return g.commitErr
}
func (g *stubGit) Pull(ctx context.Context, _ string, _ string) (bool, error) {
	g.pullCalls++
	if g.onPull != nil {
		g.onPull(ctx)
	}
	return false, g.pullErr
}
func (g *stubGit) Push(ctx context.Context, _ string, _ string) error {
	g.pushCalls++
	if g.onPush != nil {
		g.onPush(ctx)
	}
	return g.pushErr
}
func (g *stubGit) RebaseAbort(context.Context) error { g.rebaseAborts++; return g.rebaseAbortErr }
func (g *stubGit) ShortHEAD(ctx context.Context) (string, error) {
	g.shortHEADCalls++
	if g.onShortHEAD != nil {
		g.onShortHEAD(ctx)
	}
	return g.shortHEAD, g.shortHEADErr
}
func (g *stubGit) CommitTime(context.Context) (time.Time, error) {
	return g.commitTime, g.commitTimeErr
}
func (g *stubGit) DiffStaged(context.Context) (string, error) { return g.diffStaged, g.diffStagedErr }
func (g *stubGit) DiffStat(context.Context) (string, error)   { return g.diffStat, g.diffStatErr }

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
	if sg, ok := g.(*stubGit); ok && sg.branch == "" && sg.branchErr == nil {
		sg.branch = "main"
	}
	runCtx, cancel := context.WithCancel(context.Background())

	return &Agent{
		cfg: &config.ResolvedRepo{
			Path:         "/tmp/test-repo",
			Name:         "test-repo",
			PollInterval: time.Second,
			Branch:       "main",
			Remote:       "origin",
			Hooks:        &config.ResolvedHooks{},
		},
		git:     g,
		alerter: alerter,
		state:   Watching,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		syncCh:  make(chan struct{}, 1),
		runCtx:  runCtx,
		cancel:  cancel,
		now:     time.Now,
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

func TestBranchReportsActualGitBranch(t *testing.T) {
	a := newAgentForTest(&stubGit{branch: "feature/refactor"}, nil)

	if got := a.Branch(); got != "feature/refactor" {
		t.Fatalf("expected actual git branch, got %q", got)
	}
}

func TestSyncFailsWhenCurrentBranchDoesNotMatchConfiguredBranch(t *testing.T) {
	g := &stubGit{branch: "feature/refactor"}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)

	a.sync()

	if got := a.State(); got != Error {
		t.Fatalf("expected error state, got %s", got)
	}
	if got := a.ErrorDetail(); got != `branch mismatch: configured branch "main", current branch "feature/refactor"` {
		t.Fatalf("unexpected error detail: %q", got)
	}
	if g.stageCalls != 0 || g.pullCalls != 0 || g.pushCalls != 0 {
		t.Fatalf("expected sync to stop before staging or network operations, got stage=%d pull=%d push=%d", g.stageCalls, g.pullCalls, g.pushCalls)
	}
	if len(alerts.events) != 1 {
		t.Fatalf("expected one alert for branch mismatch, got %d", len(alerts.events))
	}
}

func TestSyncFailsOnDetachedHead(t *testing.T) {
	g := &stubGit{branch: "HEAD"}
	a := newAgentForTest(g, nil)

	a.sync()

	if got := a.State(); got != Error {
		t.Fatalf("expected error state, got %s", got)
	}
	if got := a.ErrorDetail(); got != `branch mismatch: configured branch "main", current branch "HEAD"` {
		t.Fatalf("unexpected detached HEAD error detail: %q", got)
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
	a.snoozeUntil = time.Now().Add(time.Minute)
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

	message, err := a.Resume()
	if err != nil {
		t.Fatalf("expected resume to succeed, got %v", err)
	}
	if message != "resumed" {
		t.Fatalf("expected resume message, got %q", message)
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

func TestSnoozeDuringSyncWaitsForCheckpointAndStopsLaterPhases(t *testing.T) {
	reachedPull := make(chan struct{})
	releasePull := make(chan struct{})

	g := &stubGit{
		shortHEAD: "abc123",
		onPull: func(context.Context) {
			close(reachedPull)
			<-releasePull
		},
	}
	a := newAgentForTest(g, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.sync()
	}()

	<-reachedPull

	if _, err := a.Snooze(time.Minute); err != nil {
		t.Fatalf("expected snooze during sync to succeed, got %v", err)
	}
	if got := a.State(); got != Syncing {
		t.Fatalf("expected state to remain syncing until checkpoint, got %s", got)
	}

	close(releasePull)
	<-done

	if got := a.State(); got != Snoozed {
		t.Fatalf("expected snoozed state after checkpoint, got %s", got)
	}
	if g.pushCalls != 0 {
		t.Fatalf("expected push to be skipped after deferred snooze, got %d calls", g.pushCalls)
	}
	if g.shortHEADCalls != 0 {
		t.Fatalf("expected short HEAD lookup to be skipped after deferred snooze, got %d calls", g.shortHEADCalls)
	}
	if !a.LastSync().IsZero() {
		t.Fatal("expected last sync to remain unset when sync pauses mid-cycle")
	}
}

func TestSnoozeDropsQueuedSyncWhileSyncing(t *testing.T) {
	reachedPull := make(chan struct{})
	releasePull := make(chan struct{})

	g := &stubGit{
		onPull: func(context.Context) {
			close(reachedPull)
			<-releasePull
		},
	}
	a := newAgentForTest(g, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.sync()
	}()

	<-reachedPull

	if err := a.TriggerSync(); err != nil {
		t.Fatalf("expected queued trigger during sync, got %v", err)
	}
	if _, err := a.Snooze(time.Minute); err != nil {
		t.Fatalf("expected snooze during sync to succeed, got %v", err)
	}

	close(releasePull)
	<-done

	select {
	case <-a.syncCh:
		t.Fatal("expected queued sync to be dropped by snooze")
	default:
	}
}

func TestResumeClearsDeferredSnoozeAndQueuesRetry(t *testing.T) {
	reachedPull := make(chan struct{})
	releasePull := make(chan struct{})

	g := &stubGit{
		shortHEAD: "abc123",
		onPull: func(context.Context) {
			close(reachedPull)
			<-releasePull
		},
	}
	a := newAgentForTest(g, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.sync()
	}()

	<-reachedPull

	if _, err := a.Snooze(time.Minute); err != nil {
		t.Fatalf("expected snooze during sync to succeed, got %v", err)
	}
	message, err := a.Resume()
	if err != nil {
		t.Fatalf("expected resume to clear deferred snooze, got %v", err)
	}
	if message != "resumed" {
		t.Fatalf("expected resume message, got %q", message)
	}
	if got := a.State(); got != Syncing {
		t.Fatalf("expected sync to continue after resuming deferred snooze, got %s", got)
	}

	close(releasePull)
	<-done

	if got := a.State(); got != Watching {
		t.Fatalf("expected watching state after resumed sync completes, got %s", got)
	}
	select {
	case <-a.syncCh:
	default:
		t.Fatal("expected resume to queue a retry")
	}
	select {
	case <-a.syncCh:
		t.Fatal("expected exactly one queued retry after resume")
	default:
	}
}

func TestSyncSkipsActiveHoldout(t *testing.T) {
	now := time.Date(2026, time.April, 3, 10, 30, 0, 0, time.Local)
	g := &stubGit{}
	a := newAgentForTest(g, nil)
	a.now = func() time.Time { return now }
	a.cfg.HoldoutWindows = []*config.ResolvedHoldoutWindow{
		{StartMinute: 10 * 60, EndMinute: 11 * 60},
	}

	a.sync()

	if got := a.State(); got != Holdout {
		t.Fatalf("expected holdout state, got %s", got)
	}
	if g.pullCalls != 0 || g.pushCalls != 0 || g.stageCalls != 0 {
		t.Fatalf("expected no git activity during holdout, got pull=%d push=%d stage=%d", g.pullCalls, g.pushCalls, g.stageCalls)
	}
	if got := a.HoldoutRemaining(); got != 30*time.Minute {
		t.Fatalf("expected 30m holdout remaining, got %v", got)
	}
}

func TestTriggerSyncFailsDuringHoldout(t *testing.T) {
	now := time.Date(2026, time.April, 3, 10, 15, 0, 0, time.Local)
	a := newAgentForTest(&stubGit{}, nil)
	a.now = func() time.Time { return now }
	a.cfg.HoldoutWindows = []*config.ResolvedHoldoutWindow{
		{StartMinute: 10 * 60, EndMinute: 11 * 60},
	}

	err := a.TriggerSync()
	if err == nil {
		t.Fatal("expected holdout trigger failure")
	}
	if got := a.State(); got != Holdout {
		t.Fatalf("expected holdout state, got %s", got)
	}
}

func TestHoldoutDuringSyncWaitsForCheckpointAndStopsLaterPhases(t *testing.T) {
	now := time.Date(2026, time.April, 3, 9, 59, 0, 0, time.Local)
	g := &stubGit{
		shortHEAD: "abc123",
		onPull: func(context.Context) {
			now = time.Date(2026, time.April, 3, 10, 0, 0, 0, time.Local)
		},
	}
	a := newAgentForTest(g, nil)
	a.now = func() time.Time { return now }
	a.cfg.HoldoutWindows = []*config.ResolvedHoldoutWindow{
		{StartMinute: 10 * 60, EndMinute: 11 * 60},
	}

	a.sync()

	if got := a.State(); got != Holdout {
		t.Fatalf("expected holdout state after checkpoint, got %s", got)
	}
	if g.pushCalls != 0 {
		t.Fatalf("expected push to be skipped after holdout, got %d calls", g.pushCalls)
	}
	if g.shortHEADCalls != 0 {
		t.Fatalf("expected short HEAD lookup to be skipped after holdout, got %d calls", g.shortHEADCalls)
	}
	if !a.LastSync().IsZero() {
		t.Fatal("expected last sync to remain unset when holdout pauses mid-cycle")
	}
}

func TestHandleHoldoutTransitionLeavesManualSnoozeActive(t *testing.T) {
	now := time.Date(2026, time.April, 3, 11, 0, 0, 0, time.Local)
	a := newAgentForTest(&stubGit{}, nil)
	a.now = func() time.Time { return now }
	a.state = Holdout
	a.snoozeUntil = now.Add(30 * time.Minute)

	a.handleHoldoutTransition()

	if got := a.State(); got != Snoozed {
		t.Fatalf("expected snoozed state after holdout ends, got %s", got)
	}
}

func TestResumeDuringHoldoutClearsManualSnoozeWithoutBypassing(t *testing.T) {
	now := time.Date(2026, time.April, 3, 10, 15, 0, 0, time.Local)
	a := newAgentForTest(&stubGit{}, nil)
	a.now = func() time.Time { return now }
	a.cfg.HoldoutWindows = []*config.ResolvedHoldoutWindow{
		{StartMinute: 10 * 60, EndMinute: 11 * 60},
	}
	a.state = Holdout
	a.snoozeUntil = now.Add(30 * time.Minute)

	message, err := a.Resume()
	if err != nil {
		t.Fatalf("expected resume to clear manual snooze during holdout, got %v", err)
	}
	if !strings.Contains(message, "holdout remains active until 2026-04-03T11:00:00") {
		t.Fatalf("unexpected resume message %q", message)
	}
	if got := a.State(); got != Holdout {
		t.Fatalf("expected holdout state, got %s", got)
	}
	if !a.snoozeUntil.IsZero() {
		t.Fatalf("expected manual snooze to be cleared, got %s", a.snoozeUntil)
	}
	select {
	case <-a.syncCh:
		t.Fatal("expected no sync to be queued while holdout remains active")
	default:
	}
}

func TestStopCancelsSyncBlockedInDirtyCheck(t *testing.T) {
	reachedDirty := make(chan struct{})

	g := &stubGit{
		onIsDirty: func(ctx context.Context) {
			close(reachedDirty)
			<-ctx.Done()
		},
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)

	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-reachedDirty

	stopAgentAndWait(t, a)

	if got := a.ErrorDetail(); got != "" {
		t.Fatalf("ErrorDetail() = %q, want empty after shutdown cancellation", got)
	}
	if got := a.State(); got == Error {
		t.Fatalf("State() = %s, want non-error after shutdown cancellation", got)
	}
	if len(alerts.events) != 0 {
		t.Fatalf("expected no alerts on shutdown cancellation, got %d", len(alerts.events))
	}
}

func TestStopCancelsSyncBlockedInPull(t *testing.T) {
	reachedPull := make(chan struct{})

	g := &stubGit{
		onPull: func(ctx context.Context) {
			close(reachedPull)
			<-ctx.Done()
		},
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)

	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-reachedPull

	stopAgentAndWait(t, a)

	if got := a.ErrorDetail(); got != "" {
		t.Fatalf("ErrorDetail() = %q, want empty after shutdown cancellation", got)
	}
	if len(alerts.events) != 0 {
		t.Fatalf("expected no alerts on shutdown cancellation, got %d", len(alerts.events))
	}
}

func TestStopCancelsSyncBlockedInHook(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "hook-started")

	g := &stubGit{
		dirty:  true,
		status: &git.Status{},
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)
	a.cfg.Hooks.PreCommit = []*config.ResolvedHook{
		{
			Command: "touch " + marker + " && sleep 60",
			Dir:     dir,
			Timeout: time.Minute,
		},
	}

	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForFile(t, marker)

	stopAgentAndWait(t, a)

	if g.stageCalls != 0 {
		t.Fatalf("expected staging to be skipped after hook cancellation, got %d calls", g.stageCalls)
	}
	if got := a.ErrorDetail(); got != "" {
		t.Fatalf("ErrorDetail() = %q, want empty after shutdown cancellation", got)
	}
	if len(alerts.events) != 0 {
		t.Fatalf("expected no alerts on shutdown cancellation, got %d", len(alerts.events))
	}
}

func TestStopCancelsSyncBlockedInLLMRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseRequest
	}))
	defer server.Close()
	defer close(releaseRequest)

	g := &stubGit{
		dirty:      true,
		status:     &git.Status{},
		diffStaged: "diff",
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)
	a.llm = llm.NewClient(&config.LLMConfig{
		Endpoint: server.URL,
		Model:    "test-model",
	})

	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-requestStarted

	stopAgentAndWait(t, a)

	if g.commitCalls != 0 {
		t.Fatalf("expected commit to be skipped after LLM cancellation, got %d calls", g.commitCalls)
	}
	if g.pullCalls != 0 {
		t.Fatalf("expected pull to be skipped after LLM cancellation, got %d calls", g.pullCalls)
	}
	if got := a.ErrorDetail(); got != "" {
		t.Fatalf("ErrorDetail() = %q, want empty after shutdown cancellation", got)
	}
	if len(alerts.events) != 0 {
		t.Fatalf("expected no alerts on shutdown cancellation, got %d", len(alerts.events))
	}
}

func stopAgentAndWait(t *testing.T, a *Agent) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- a.Stop()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return after shutdown cancellation")
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", path)
}
