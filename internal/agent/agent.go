package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
	"github.com/michaelquigley/sexton/internal/llm"
)

type Agent struct {
	cfg     *config.ResolvedRepo
	git     gitClient
	llm     *llm.Client
	alerter Alerter
	now     func() time.Time

	mu    sync.Mutex
	state State

	stopCh   chan struct{}
	doneCh   chan struct{}
	syncCh   chan struct{}
	runCtx   context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once

	snoozeTimer     *time.Timer
	snoozeUntil     time.Time
	snoozePending   bool
	holdoutUntil    time.Time
	errorDetail     string
	attentionDetail string

	lastSync   time.Time
	lastCommit string
	lastChange time.Time
}

type gitClient interface {
	Branch(ctx context.Context) (string, error)
	IsDirtyTracked(ctx context.Context) (bool, error)
	Unmerged(ctx context.Context) ([]string, error)
	Status(ctx context.Context) (*git.Status, error)
	StageAll(ctx context.Context) error
	StageRegions(ctx context.Context, regions []string) error
	Commit(ctx context.Context, message string) (string, error)
	CommitOnly(ctx context.Context, message string, regions []string) (string, error)
	Pull(ctx context.Context, remote, branch string) (bool, error)
	Push(ctx context.Context, remote, branch string) error
	RebaseAbort(ctx context.Context) error
	RewordCommit(ctx context.Context, branch, oldSHA, message string) error
	ShortHEAD(ctx context.Context) (string, error)
	CommitTime(ctx context.Context) (time.Time, error)
	Show(ctx context.Context, sha string) (string, error)
	ShowStat(ctx context.Context, sha string) (string, error)
	ShowNameStatus(ctx context.Context, sha string) (*git.Status, error)
}

func NewAgent(cfg *config.ResolvedRepo, g *git.Git) *Agent {
	runCtx, cancel := context.WithCancel(context.Background())
	return &Agent{
		cfg:    cfg,
		git:    g,
		state:  Watching,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		syncCh: make(chan struct{}, 1),
		runCtx: runCtx,
		cancel: cancel,
		now:    time.Now,
	}
}

func (a *Agent) Wire(c *Container) error {
	a.llm = c.LLM
	a.alerter = c.Alerter
	return nil
}

func (a *Agent) Start() error {
	switch {
	case a.cfg.LocalConfigError != nil:
		a.alert("warning", fmt.Sprintf("repo-local config malformed (%v); commit policy forced to 'none'", a.cfg.LocalConfigError), nil)
	case a.cfg.PolicyDefaulted:
		a.alert("warning", "no commit_policy configured; defaulting to 'none'", nil)
	}
	if len(a.cfg.HoldoutWindows) > 0 {
		go a.runHoldoutScheduler()
	}
	go a.run()
	return nil
}

func (a *Agent) Stop() error {
	a.stopOnce.Do(func() {
		a.cancel()
		close(a.stopCh)
	})
	<-a.doneCh
	return nil
}

func (a *Agent) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *Agent) Path() string {
	return a.cfg.Path
}

func (a *Agent) Name() string {
	return a.cfg.Name
}

func (a *Agent) Branch() string {
	branch, err := a.git.Branch(context.Background())
	if err != nil {
		return "unknown"
	}
	return branch
}

func (a *Agent) LastSync() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastSync
}

func (a *Agent) LastCommit() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastCommit
}

func (a *Agent) LastChange() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastChange
}

func (a *Agent) ErrorDetail() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.errorDetail
}

func (a *Agent) AttentionDetail() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attentionDetail
}

func (a *Agent) SnoozeRemaining() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != Snoozed {
		return 0
	}
	remaining := a.snoozeUntil.Sub(a.now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (a *Agent) HoldoutRemaining() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != Holdout {
		return 0
	}
	remaining := a.holdoutUntil.Sub(a.now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// TriggerSync requests an immediate sync cycle. errors if the agent is paused.
func (a *Agent) TriggerSync() error {
	a.mu.Lock()
	now := a.now()
	if activeHoldout, holdoutUntil := a.holdoutStatusAt(now); activeHoldout {
		a.holdoutUntil = holdoutUntil
		if a.state != Syncing {
			a.state = Holdout
		}
		a.mu.Unlock()
		return fmt.Errorf("agent is in holdout until '%s'", holdoutUntil.Format(time.RFC3339))
	}
	if a.state != Syncing {
		a.restingStateLocked()
	}
	if a.state == Snoozed {
		a.mu.Unlock()
		return fmt.Errorf("agent is snoozed")
	}
	select {
	case a.syncCh <- struct{}{}:
	default:
		// sync already pending
	}
	a.mu.Unlock()
	return nil
}

// Snooze pauses the agent for the given duration.
func (a *Agent) Snooze(d time.Duration) (time.Time, error) {
	a.mu.Lock()
	until := a.startSnoozeLocked(d)
	if a.state == Syncing {
		a.snoozePending = true
	} else {
		a.restingStateLocked()
	}
	a.mu.Unlock()

	dl.Infof("snoozed '%s' until %s", a.cfg.Name, until.Format(time.RFC3339))
	a.alert("info", "snoozed", nil)
	return until, nil
}

// Resume clears an error or manual snooze and optionally triggers an immediate sync.
func (a *Agent) Resume() (string, error) {
	a.mu.Lock()
	now := a.now()
	activeHoldout, holdoutUntil := a.holdoutStatusAt(now)
	manualSnoozed := a.state == Snoozed || a.snoozePending || a.manualSnoozeActiveLocked(now)
	canClearError := a.errorDetail != ""
	if !manualSnoozed && !canClearError && !activeHoldout {
		state := a.state
		a.mu.Unlock()
		return "", fmt.Errorf("agent is not errored or snoozed (state: '%s')", state)
	}
	if activeHoldout && !manualSnoozed && !canClearError {
		a.holdoutUntil = holdoutUntil
		if a.state != Syncing {
			a.state = Holdout
		}
		a.mu.Unlock()
		return "", fmt.Errorf("agent is in holdout until '%s'", holdoutUntil.Format(time.RFC3339))
	}
	a.clearSnoozeLocked()
	a.errorDetail = ""

	message := "resumed"
	queueSync := false
	switch {
	case activeHoldout:
		a.holdoutUntil = holdoutUntil
		if a.state != Syncing {
			a.restingStateLocked()
		}
		message = fmt.Sprintf("holdout remains active until %s", holdoutUntil.Format(time.RFC3339))
	case a.state == Syncing:
		queueSync = true
	default:
		a.restingStateLocked()
		queueSync = true
	}
	a.mu.Unlock()
	dl.Infof("resumed '%s'", a.cfg.Name)
	a.alert("info", "resumed", nil)
	if queueSync {
		select {
		case a.syncCh <- struct{}{}:
		default:
		}
	}
	return message, nil
}

func (a *Agent) run() {
	defer close(a.doneCh)

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	// run one sync immediately on start
	a.sync()

	for {
		// build a snooze channel that blocks forever when not snoozed
		a.mu.Lock()
		var snoozeCh <-chan time.Time
		if a.snoozeTimer != nil {
			snoozeCh = a.snoozeTimer.C
		}
		a.mu.Unlock()

		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.sync()
		case <-a.syncCh:
			a.sync()
		case <-snoozeCh:
			a.handleSnoozeExpiry()
		}
	}
}

func (a *Agent) sync() {
	ctx, cancel := context.WithCancel(a.runCtx)
	defer cancel()
	if a.syncCanceled(ctx, nil) {
		return
	}

	a.mu.Lock()
	now := a.now()
	if activeHoldout, holdoutUntil := a.holdoutStatusAt(now); activeHoldout {
		a.holdoutUntil = holdoutUntil
		a.restingStateLocked()
		a.mu.Unlock()
		return
	}
	if a.manualSnoozeActiveLocked(now) {
		a.restingStateLocked()
		a.mu.Unlock()
		return
	}
	if a.snoozePending || !a.snoozeUntil.IsZero() {
		a.restingStateLocked()
	}
	hadError := a.errorDetail != ""
	a.holdoutUntil = time.Time{}
	a.state = Syncing
	a.mu.Unlock()
	defer a.clearCanceledSyncState(ctx)

	dl.Debugf("sync started for '%s'", a.cfg.Name)

	if err := a.validateBranch(ctx); err != nil {
		if a.syncCanceled(ctx, err) {
			return
		}
		a.setError("branch mismatch", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	status, err := a.git.Status(ctx)
	if err != nil {
		if a.syncCanceled(ctx, err) {
			return
		}
		a.setError("failed to read git status", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}
	if status == nil {
		status = git.NewStatus()
	}
	partition := partitionStatus(status, a.cfg.CommitPolicy, a.cfg.CommitRegions)

	if status.HasChanges() {
		// refuse to stage a tree carrying unresolved conflict markers. a merge or
		// cherry-pick conflict leaves the repo on its branch, so validateBranch
		// above does not catch it, and staging would otherwise commit the markers.
		unmerged, err := a.git.Unmerged(ctx)
		if err != nil {
			if a.syncCanceled(ctx, err) {
				return
			}
			a.setError("failed to check for unmerged paths", err)
			return
		}
		if len(unmerged) > 0 {
			a.setError("unresolved conflict", fmt.Errorf("%d unmerged path(s): %s", len(unmerged), strings.Join(unmerged, ", ")))
			return
		}
		if a.shouldAbortSync(ctx) {
			return
		}
	}

	var committedStatus *git.Status
	var commitMsg string
	createdChange := false
	if partition.hasSelectedChanges {
		if err := a.runHooks(ctx, "pre_commit", a.cfg.Hooks.PreCommit); err != nil {
			if a.syncCanceled(ctx, err) {
				return
			}
			a.setError("pre_commit hook failed", err)
			return
		}
		if a.shouldAbortSync(ctx) {
			return
		}

		var stageErr error
		switch a.cfg.CommitPolicy {
		case config.PolicyAll:
			stageErr = a.git.StageAll(ctx)
		case config.PolicyRegions:
			stageErr = a.git.StageRegions(ctx, partition.regions)
		}
		if stageErr != nil {
			if a.syncCanceled(ctx, stageErr) {
				return
			}
			a.setError("staging failed", stageErr)
			return
		}
		if a.shouldAbortSync(ctx) {
			return
		}

		var createdSHA string
		switch a.cfg.CommitPolicy {
		case config.PolicyAll:
			createdSHA, err = a.git.Commit(ctx, placeholderCommitMessage)
		case config.PolicyRegions:
			createdSHA, err = a.git.CommitOnly(ctx, placeholderCommitMessage, partition.regions)
		}
		if err != nil && !errors.Is(err, git.ErrNothingToCommit) {
			if a.syncCanceled(ctx, err) {
				return
			}
			a.setError("commit failed", err)
			return
		}
		if errors.Is(err, git.ErrNothingToCommit) {
			err = nil
		}
		if createdSHA != "" {
			createdChange = true
			committedStatus, err = a.git.ShowNameStatus(ctx, createdSHA)
			if err != nil {
				if a.syncCanceled(ctx, err) {
					return
				}
				a.setError("failed to describe commit", err)
				return
			}

			dl.Infof("generating commit message for '%s'", a.cfg.Name)
			commitMsg, err = a.generateCommitMessage(ctx, createdSHA, committedStatus)
			if err != nil {
				if a.syncCanceled(ctx, err) {
					return
				}
				a.setError("commit message generation failed", err)
				return
			}

			if err := a.git.RewordCommit(ctx, a.cfg.Branch, createdSHA, commitMsg); err != nil {
				if a.syncCanceled(ctx, err) {
					return
				}
				a.setError("commit reword failed", err)
				return
			}
			if a.shouldAbortSync(ctx) {
				return
			}

			if err := a.runHooks(ctx, "post_commit", a.cfg.Hooks.PostCommit); err != nil {
				if a.syncCanceled(ctx, err) {
					return
				}
				a.setError("post_commit hook failed", err)
				return
			}
			if a.shouldAbortSync(ctx) {
				return
			}
		}
	}

	if len(partition.unselectedPaths) > 0 {
		a.setAttention(formatAttentionDetail(partition.unselectedPaths, partition.hasTrackedUnselected))
	} else if a.clearAttention() {
		a.alert("info", "local changes resolved", nil)
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	pulled := false
	if !partition.hasTrackedUnselected {
		pulled, err = a.git.Pull(ctx, a.cfg.Remote, a.cfg.Branch)
	}
	if err != nil {
		// a conflict is handled ahead of the cancellation check: shutdown must not
		// leave the repo mid-rebase, and the abort runs on its own context because
		// the sync's may already be canceled.
		if errors.Is(err, git.ErrConflict) {
			abortErr := a.abortRebase()
			if abortErr != nil {
				a.setError("rebase conflict; abort failed", fmt.Errorf("%w (abort: %v)", err, abortErr))
				return
			}
			a.setError("rebase conflict", err)
			return
		}
		if a.syncCanceled(ctx, err) {
			// a canceled pull cannot report a conflict: killing git discards the
			// output the conflict is recognized from, and the kill can itself leave
			// a rebase in progress. abort unconditionally on the way out — with no
			// rebase in progress it fails harmlessly.
			_ = a.abortRebase()
			return
		}
		if errors.Is(err, git.ErrNoRemote) {
			if a.shouldAbortSync(ctx) {
				return
			}
			a.finishSuccessfulSync("", time.Time{}, hadError, createdChange, committedStatus, commitMsg)
			return
		}
		if errors.Is(err, git.ErrDirtyWorkingTree) {
			if a.shouldAbortSync(ctx) {
				return
			}
			// tracked dirt appeared after the partition read. the next poll will
			// re-partition it; this cycle ends without manufacturing an error.
			a.restAfterIncompleteSync()
			return
		}
		a.setError("pull failed", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	if pulled {
		if err := a.runHooks(ctx, "post_pull", a.cfg.Hooks.PostPull); err != nil {
			if a.syncCanceled(ctx, err) {
				return
			}
			a.setError("post_pull hook failed", err)
			return
		}
		if a.shouldAbortSync(ctx) {
			return
		}
	}

	if err := a.runHooks(ctx, "pre_push", a.cfg.Hooks.PrePush); err != nil {
		if a.syncCanceled(ctx, err) {
			return
		}
		a.setError("pre_push hook failed", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	if err := a.git.Push(ctx, a.cfg.Remote, a.cfg.Branch); err != nil {
		if a.syncCanceled(ctx, err) {
			return
		}
		if errors.Is(err, git.ErrNoRemote) {
			if a.shouldAbortSync(ctx) {
				return
			}
			a.finishSuccessfulSync("", time.Time{}, hadError, createdChange, committedStatus, commitMsg)
			return
		}
		a.setError("push failed", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	if err := a.runHooks(ctx, "post_sync", a.cfg.Hooks.PostSync); err != nil {
		if a.syncCanceled(ctx, err) {
			return
		}
		a.setError("post_sync hook failed", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	sha, err := a.git.ShortHEAD(ctx)
	if err != nil {
		if a.syncCanceled(ctx, err) {
			return
		}
		a.setError("failed to read HEAD", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	commitTime, err := a.git.CommitTime(ctx)
	if err != nil {
		if a.syncCanceled(ctx, err) {
			return
		}
		a.setError("failed to read commit time", err)
		return
	}
	if a.shouldAbortSync(ctx) {
		return
	}

	a.finishSuccessfulSync(sha, commitTime, hadError, createdChange, committedStatus, commitMsg)
}

const placeholderCommitMessage = "sexton: pending summary"

type policyPartition struct {
	hasSelectedChanges   bool
	regions              []string
	unselectedPaths      []string
	hasTrackedUnselected bool
}

func partitionStatus(status *git.Status, policy string, regions []string) policyPartition {
	if status == nil {
		return policyPartition{}
	}

	var partition policyPartition
	selectedRegions := make(map[string]bool)
	unselectedPaths := make(map[string]bool)
	for _, entry := range status.Entries {
		endpoints := []string{entry.Path}
		if (entry.X == 'R' || entry.Y == 'R') && entry.OldPath != "" {
			endpoints = append(endpoints, entry.OldPath)
		}
		for _, path := range endpoints {
			inside := policy == config.PolicyAll
			if policy == config.PolicyRegions {
				for _, region := range regions {
					if strings.HasPrefix(path, region) {
						inside = true
						selectedRegions[region] = true
					}
				}
			}
			if inside {
				partition.hasSelectedChanges = true
				continue
			}
			unselectedPaths[path] = true
			if entry.IsTracked() {
				partition.hasTrackedUnselected = true
			}
		}
	}

	for _, region := range regions {
		if selectedRegions[region] {
			partition.regions = append(partition.regions, region)
		}
	}
	for path := range unselectedPaths {
		partition.unselectedPaths = append(partition.unselectedPaths, path)
	}
	sort.Strings(partition.unselectedPaths)
	return partition
}

func formatAttentionDetail(paths []string, hasTrackedUnselected bool) string {
	unique := make(map[string]bool, len(paths))
	for _, path := range paths {
		unique[path] = true
	}
	sorted := make([]string, 0, len(unique))
	for path := range unique {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)

	const pathLimit = 10
	visible := sorted
	if len(visible) > pathLimit {
		visible = visible[:pathLimit]
	}
	quoted := make([]string, 0, len(visible))
	for _, path := range visible {
		quoted = append(quoted, strconv.Quote(path))
	}
	detail := strings.Join(quoted, ", ")
	if hidden := len(sorted) - len(visible); hidden > 0 {
		detail += fmt.Sprintf(", +%d more", hidden)
	}
	if hasTrackedUnselected {
		detail += " (pulls paused)"
	}
	return detail
}

func (a *Agent) startSnoozeLocked(d time.Duration) time.Time {
	until := a.now().Add(d)
	a.snoozeUntil = until
	a.resetSnoozeTimerLocked(d)
	a.drainSyncRequestsLocked()
	return until
}

func (a *Agent) resetSnoozeTimerLocked(d time.Duration) {
	if a.snoozeTimer != nil {
		if !a.snoozeTimer.Stop() {
			select {
			case <-a.snoozeTimer.C:
			default:
			}
		}
	}
	a.snoozeTimer = time.NewTimer(d)
}

func (a *Agent) clearSnoozeLocked() {
	if a.snoozeTimer != nil {
		if !a.snoozeTimer.Stop() {
			select {
			case <-a.snoozeTimer.C:
			default:
			}
		}
		a.snoozeTimer = nil
	}
	a.snoozeUntil = time.Time{}
	a.snoozePending = false
}

func (a *Agent) clearExpiredSnoozeLocked() {
	if a.snoozeTimer != nil {
		if !a.snoozeTimer.Stop() {
			select {
			case <-a.snoozeTimer.C:
			default:
			}
		}
	}
	a.snoozeTimer = nil
	a.snoozeUntil = time.Time{}
	a.snoozePending = false
}

func (a *Agent) drainSyncRequestsLocked() {
	for {
		select {
		case <-a.syncCh:
		default:
			return
		}
	}
}

func (a *Agent) pauseIfRequested() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if activeHoldout, holdoutUntil := a.holdoutStatusAt(now); activeHoldout {
		a.holdoutUntil = holdoutUntil
		a.state = Holdout
		a.snoozePending = false
		a.drainSyncRequestsLocked()
		return true
	}
	if !a.snoozePending {
		return false
	}
	if !a.manualSnoozeActiveLocked(now) {
		a.clearExpiredSnoozeLocked()
		return false
	}
	a.restingStateLocked()
	a.drainSyncRequestsLocked()
	return true
}

func (a *Agent) shouldAbortSync(ctx context.Context) bool {
	if a.syncCanceled(ctx, nil) {
		return true
	}
	return a.pauseIfRequested()
}

func (a *Agent) completeSync(sha string, commitTime time.Time) {
	a.mu.Lock()
	a.lastSync = a.now()
	if sha != "" {
		a.lastCommit = sha
	}
	if !commitTime.IsZero() {
		a.lastChange = commitTime
	}
	a.errorDetail = ""
	a.restingStateLocked()
	a.mu.Unlock()
}

func (a *Agent) finishSuccessfulSync(sha string, commitTime time.Time, hadError, createdChange bool, status *git.Status, commitMessage string) {
	a.completeSync(sha, commitTime)
	dl.Debugf("sync complete for '%s'", a.cfg.Name)
	if hadError {
		a.alert("info", "recovered from error", nil)
	}
	if createdChange {
		a.alertWithFiles("info", "sync complete", status, commitMessage)
	}
}

func (a *Agent) restAfterIncompleteSync() {
	a.mu.Lock()
	a.restingStateLocked()
	a.mu.Unlock()
}

// restingStateLocked is the sole chooser for a terminal, non-syncing state.
// the caller must hold a.mu.
func (a *Agent) restingStateLocked() State {
	now := a.now()
	if activeHoldout, holdoutUntil := a.holdoutStatusAt(now); activeHoldout {
		a.holdoutUntil = holdoutUntil
		a.state = Holdout
		return a.state
	}
	a.holdoutUntil = time.Time{}

	if a.manualSnoozeActiveLocked(now) {
		a.snoozePending = false
		a.state = Snoozed
		return a.state
	}
	if a.snoozePending || !a.snoozeUntil.IsZero() {
		a.clearExpiredSnoozeLocked()
	}

	switch {
	case a.errorDetail != "":
		a.state = Error
	case a.attentionDetail != "":
		a.state = Attention
	default:
		a.state = Watching
	}
	return a.state
}

func (a *Agent) validateBranch(ctx context.Context) error {
	current, err := a.git.Branch(ctx)
	if err != nil {
		return fmt.Errorf("failed to determine current branch: %w", err)
	}
	if current == a.cfg.Branch {
		return nil
	}
	return fmt.Errorf("configured branch %q, current branch %q", a.cfg.Branch, current)
}

func (a *Agent) setError(message string, err error) {
	detail := formatErrorDetail(message, err)

	a.mu.Lock()
	shouldAlert := a.errorDetail != detail
	a.errorDetail = detail
	a.restingStateLocked()
	a.mu.Unlock()

	if shouldAlert {
		a.alert("error", message, err)
	}
}

func (a *Agent) setAttention(detail string) {
	a.mu.Lock()
	shouldAlert := a.attentionDetail != detail
	a.attentionDetail = detail
	a.mu.Unlock()

	if shouldAlert {
		a.alert("attention", detail, nil)
	}
}

func (a *Agent) clearAttention() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.attentionDetail == "" {
		return false
	}
	a.clearAttentionLocked()
	return true
}

func (a *Agent) clearAttentionLocked() {
	a.attentionDetail = ""
}

func formatErrorDetail(message string, err error) string {
	if err == nil {
		return message
	}
	if message == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s: %v", message, err)
}

func (a *Agent) alert(severity, message string, err error) {
	event := AlertEvent{
		Severity:  severity,
		RepoName:  a.cfg.Name,
		Message:   message,
		Error:     err,
		Timestamp: a.now(),
	}
	if a.alerter == nil {
		dl.Warnf("failed to deliver alert for '%s': '%s': no alerter configured", a.cfg.Name, message)
		return
	}
	if alertErr := a.alerter.Alert(context.Background(), event); alertErr != nil {
		dl.Warnf("failed to deliver alert for '%s': '%s': %v", a.cfg.Name, message, alertErr)
	}
}

func (a *Agent) alertWithFiles(severity, message string, status *git.Status, commitMessage string) {
	var files *AlertFiles
	if status != nil {
		files = &AlertFiles{
			Modified: append([]string(nil), status.Modified...),
			Added:    append(append([]string(nil), status.Added...), status.Untracked...),
			Deleted:  append([]string(nil), status.Deleted...),
		}
	}
	event := AlertEvent{
		Severity:      severity,
		RepoName:      a.cfg.Name,
		Message:       message,
		Timestamp:     a.now(),
		Files:         files,
		CommitMessage: commitMessage,
	}
	if a.alerter == nil {
		dl.Warnf("failed to deliver alert for '%s': '%s': no alerter configured", a.cfg.Name, message)
		return
	}
	if alertErr := a.alerter.Alert(context.Background(), event); alertErr != nil {
		dl.Warnf("failed to deliver alert for '%s': '%s': %v", a.cfg.Name, message, alertErr)
	}
}

// abortRebase runs 'git rebase --abort' on its own bounded context, deliberately
// not the sync's, which may already be canceled by the shutdown that made the
// cleanup necessary.
func (a *Agent) abortRebase() error {
	abortCtx, cancel := context.WithTimeout(context.Background(), rebaseAbortTimeout)
	defer cancel()
	return a.git.RebaseAbort(abortCtx)
}

// rebaseAbortTimeout bounds the cleanup abort that runs after a rebase conflict.
// it is deliberately not the sync's context, which may already be canceled by a
// shutdown that arrived while the pull was conflicting.
const rebaseAbortTimeout = 30 * time.Second

const maxDiffBytes = 32 * 1024

func (a *Agent) generateCommitMessage(ctx context.Context, sha string, status *git.Status) (string, error) {
	fallback := git.GenerateCommitMessage(status)

	if a.llm == nil {
		dl.Warnf("no llm configured for '%s', using fallback commit message", a.cfg.Name)
		return fallback, nil
	}

	diff, err := a.git.Show(ctx, sha)
	if err != nil {
		if a.syncCanceled(ctx, err) {
			return "", err
		}
		dl.Warnf("failed to get commit diff for '%s': %v", a.cfg.Name, err)
		return fallback, nil
	}

	if len(diff) > maxDiffBytes {
		diff, err = a.git.ShowStat(ctx, sha)
		if err != nil {
			if a.syncCanceled(ctx, err) {
				return "", err
			}
			dl.Warnf("failed to get diff stat for '%s': %v", a.cfg.Name, err)
			return fallback, nil
		}
	}

	if a.cfg.CommitMessagePrompt == "" {
		a.cfg.CommitMessagePrompt = config.DefaultCommitMessagePrompt
	}

	result, err := a.llm.Complete(ctx, a.cfg.CommitMessagePrompt, diff, 0)
	if err != nil {
		if a.syncCanceled(ctx, err) {
			return "", err
		}
		dl.Warnf("llm commit message failed for '%s': %v", a.cfg.Name, err)
		return fallback, nil
	}

	if result == "" {
		dl.Warnf("llm returned empty commit message for '%s', using fallback", a.cfg.Name)
		return fallback, nil
	}

	dl.Infof("llm generated commit message for '%s'", a.cfg.Name)
	return result, nil
}

func (a *Agent) syncCanceled(ctx context.Context, err error) bool {
	return errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled)
}

func (a *Agent) clearCanceledSyncState(ctx context.Context) {
	if !a.syncCanceled(ctx, nil) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == Syncing {
		a.restingStateLocked()
	}
}

func (a *Agent) runHoldoutScheduler() {
	for {
		next := a.nextHoldoutTransitionAfter(a.now())
		if next.IsZero() {
			return
		}

		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}

		timer := time.NewTimer(wait)
		select {
		case <-a.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			a.handleHoldoutTransition()
		}
	}
}

// handleHoldoutTransition is invoked by the holdout scheduler at each window
// boundary to move the agent into or out of the Holdout state. it deliberately
// does NOT trigger a sync when a window ends. holdout exists to avoid touching a
// remote during a known-bad window (e.g. a nightly maintenance restart), so
// poking the remote the instant the window lifts defeats the purpose — and
// across a fleet of agents it stampedes every one of them against the still-
// recovering remote at the same second. instead, recovery is left to the next
// regular poll, which gives up to one poll interval of grace for the remote to
// come back and naturally staggers retries by each agent's ticker phase. the
// immediate-sync-on-exit behavior is intentionally kept for snooze and Resume,
// which are user-initiated and want responsiveness.
func (a *Agent) handleHoldoutTransition() {
	now := a.now()
	activeHoldout, holdoutUntil := a.holdoutStatusAt(now)

	a.mu.Lock()
	previousState := a.state
	if activeHoldout {
		a.holdoutUntil = holdoutUntil
		if a.state != Syncing {
			a.state = Holdout
			a.drainSyncRequestsLocked()
		}
	} else {
		if a.state == Holdout {
			a.restingStateLocked()
		}
	}
	a.mu.Unlock()

	switch {
	case activeHoldout && previousState != Holdout:
		dl.Infof("holdout started for '%s' until %s", a.cfg.Name, holdoutUntil.Format(time.RFC3339))
	case !activeHoldout && previousState == Holdout:
		dl.Infof("holdout ended for '%s'", a.cfg.Name)
	}
}

func (a *Agent) handleSnoozeExpiry() {
	a.mu.Lock()
	a.clearExpiredSnoozeLocked()
	queueSync := false
	if a.state != Syncing {
		state := a.restingStateLocked()
		queueSync = state == Watching || state == Attention
	}
	a.mu.Unlock()

	dl.Infof("snooze expired for '%s'", a.cfg.Name)
	if queueSync {
		select {
		case a.syncCh <- struct{}{}:
		default:
		}
	}
}

func (a *Agent) manualSnoozeActiveLocked(now time.Time) bool {
	return !a.snoozeUntil.IsZero() && now.Before(a.snoozeUntil)
}

func (a *Agent) holdoutStatusAt(now time.Time) (bool, time.Time) {
	if len(a.cfg.HoldoutWindows) == 0 {
		return false, time.Time{}
	}

	now = now.In(time.Local)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	for _, window := range a.cfg.HoldoutWindows {
		start := dayStart.Add(time.Duration(window.StartMinute) * time.Minute)
		end := dayStart.Add(time.Duration(window.EndMinute) * time.Minute)
		if !now.Before(start) && now.Before(end) {
			return true, end
		}
	}

	return false, time.Time{}
}

func (a *Agent) nextHoldoutTransitionAfter(now time.Time) time.Time {
	if len(a.cfg.HoldoutWindows) == 0 {
		return time.Time{}
	}

	if activeHoldout, holdoutUntil := a.holdoutStatusAt(now); activeHoldout {
		return holdoutUntil
	}

	now = now.In(time.Local)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	for _, window := range a.cfg.HoldoutWindows {
		start := dayStart.Add(time.Duration(window.StartMinute) * time.Minute)
		if start.After(now) {
			return start
		}
	}

	nextDay := dayStart.AddDate(0, 0, 1)
	return nextDay.Add(time.Duration(a.cfg.HoldoutWindows[0].StartMinute) * time.Minute)
}
