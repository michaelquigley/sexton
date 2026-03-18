package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
	"github.com/michaelquigley/sexton/internal/llm"
)

type Agent struct {
	cfg     *config.ResolvedRepo
	git     *git.Git
	llm     *llm.Client
	alerter Alerter

	mu    sync.Mutex
	state State

	stopCh chan struct{}
	doneCh chan struct{}
	syncCh chan struct{}

	snoozeTimer *time.Timer
	snoozeUntil time.Time
	haltErr     error

	lastSync   time.Time
	lastCommit string
}

func New(cfg *config.ResolvedRepo, g *git.Git) *Agent {
	return &Agent{
		cfg:    cfg,
		git:    g,
		state:  Watching,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		syncCh: make(chan struct{}, 1),
	}
}

func (a *Agent) Wire(c *Container) error {
	a.llm = c.LLM
	a.alerter = c.Alerter
	return nil
}

func (a *Agent) Start() error {
	go a.run()
	return nil
}

func (a *Agent) Stop() error {
	close(a.stopCh)
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
	return a.cfg.Branch
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

func (a *Agent) HaltError() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.haltErr
}

func (a *Agent) SnoozeRemaining() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != Snoozed {
		return 0
	}
	remaining := time.Until(a.snoozeUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// TriggerSync requests an immediate sync cycle. errors if the agent is halted or snoozed.
func (a *Agent) TriggerSync() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == Halted {
		return fmt.Errorf("agent is halted")
	}
	if a.state == Snoozed {
		return fmt.Errorf("agent is snoozed")
	}
	select {
	case a.syncCh <- struct{}{}:
	default:
		// sync already pending
	}
	return nil
}

// Snooze pauses the agent for the given duration. errors if the agent is halted.
func (a *Agent) Snooze(d time.Duration) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == Halted {
		return time.Time{}, fmt.Errorf("cannot snooze a halted agent")
	}
	a.state = Snoozed
	a.snoozeUntil = time.Now().Add(d)
	if a.snoozeTimer != nil {
		a.snoozeTimer.Stop()
	}
	a.snoozeTimer = time.NewTimer(d)
	dl.Infof("snoozed '%s' until %s", a.cfg.Name, a.snoozeUntil.Format(time.RFC3339))
	return a.snoozeUntil, nil
}

// Resume transitions a halted or snoozed agent back to watching and triggers an immediate sync.
func (a *Agent) Resume() error {
	a.mu.Lock()
	if a.state != Halted && a.state != Snoozed {
		a.mu.Unlock()
		return fmt.Errorf("agent is not halted or snoozed (state: '%s')", a.state)
	}
	if a.snoozeTimer != nil {
		a.snoozeTimer.Stop()
		a.snoozeTimer = nil
	}
	a.snoozeUntil = time.Time{}
	a.haltErr = nil
	a.state = Watching
	a.mu.Unlock()
	dl.Infof("resumed '%s'", a.cfg.Name)
	select {
	case a.syncCh <- struct{}{}:
	default:
	}
	return nil
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
			a.mu.Lock()
			if a.state == Snoozed {
				a.state = Watching
				a.snoozeTimer = nil
				a.snoozeUntil = time.Time{}
			}
			a.mu.Unlock()
			dl.Infof("snooze expired for '%s'", a.cfg.Name)
			a.sync()
		}
	}
}

func (a *Agent) sync() {
	a.mu.Lock()
	if a.state == Halted || a.state == Snoozed {
		a.mu.Unlock()
		return
	}
	a.state = Syncing
	a.mu.Unlock()

	dl.Debugf("sync started for '%s'", a.cfg.Name)

	ctx := context.Background()

	dirty, err := a.git.IsDirty()
	if err != nil {
		a.halt("failed to check status", err)
		return
	}

	if dirty {
		status, _ := a.git.Status()

		if err := a.git.StageAll(ctx); err != nil {
			a.halt("staging failed", err)
			return
		}

		dl.Infof("generating commit message for '%s'", a.cfg.Name)
		msg := a.generateCommitMessage(ctx, status)
		dl.Infof("generated commit message '%v'", msg)

		if err := a.git.Commit(ctx, msg); err != nil {
			if !errors.Is(err, git.ErrNothingToCommit) {
				a.halt("commit failed", err)
				return
			}
		}
	}

	_, err = a.git.Pull(ctx)
	if err != nil {
		if errors.Is(err, git.ErrConflict) {
			_ = a.git.RebaseAbort(ctx)
			a.halt("rebase conflict", err)
			return
		}
		if errors.Is(err, git.ErrNoRemote) {
			// no remote configured — commit-only mode
			a.mu.Lock()
			a.state = Watching
			a.lastSync = time.Now()
			a.mu.Unlock()
			return
		}
		if errors.Is(err, git.ErrDirtyWorkingTree) {
			// shouldn't happen since we committed above, but handle gracefully
			a.mu.Lock()
			a.state = Watching
			a.mu.Unlock()
			return
		}
		a.halt("pull failed", err)
		return
	}

	if err := a.git.Push(ctx); err != nil {
		if errors.Is(err, git.ErrNoRemote) {
			a.mu.Lock()
			a.state = Watching
			a.lastSync = time.Now()
			a.mu.Unlock()
			return
		}
		a.halt("push failed", err)
		return
	}

	sha, _ := a.git.ShortHEAD()

	a.mu.Lock()
	a.state = Watching
	a.lastSync = time.Now()
	a.lastCommit = sha
	a.mu.Unlock()

	dl.Debugf("sync complete for '%s'", a.cfg.Name)

	if dirty {
		a.alert("info", "sync complete ("+sha+")", nil)
	}
}

func (a *Agent) halt(message string, err error) {
	a.mu.Lock()
	a.state = Halted
	a.haltErr = err
	a.mu.Unlock()
	a.alert("error", message, err)
}

func (a *Agent) alert(severity, message string, err error) {
	_ = a.alerter.Alert(context.Background(), AlertEvent{
		Severity:  severity,
		RepoPath:  a.cfg.Name,
		Message:   message,
		Error:     err,
		Timestamp: time.Now(),
	})
}

const maxDiffBytes = 32 * 1024

func (a *Agent) generateCommitMessage(ctx context.Context, status *git.Status) string {
	fallback := git.GenerateCommitMessage(status)

	if a.llm == nil {
		dl.Warnf("no llm configured for '%s', using fallback commit message", a.cfg.Name)
		return fallback
	}

	diff, err := a.git.DiffStaged()
	if err != nil {
		dl.Warnf("failed to get staged diff for '%s': %v", a.cfg.Name, err)
		return fallback
	}

	if len(diff) > maxDiffBytes {
		diff, err = a.git.DiffStat()
		if err != nil {
			dl.Warnf("failed to get diff stat for '%s': %v", a.cfg.Name, err)
			return fallback
		}
	}

	if a.cfg.CommitMessagePrompt == "" {
		a.cfg.CommitMessagePrompt = config.DefaultCommitMessagePrompt
	}

	result, err := a.llm.Complete(ctx, a.cfg.CommitMessagePrompt, diff, 0)
	if err != nil {
		dl.Warnf("llm commit message failed for '%s': %v", a.cfg.Name, err)
		return fallback
	}

	if result == "" {
		dl.Warnf("llm returned empty commit message for '%s', using fallback", a.cfg.Name)
		return fallback
	}

	dl.Infof("llm generated commit message for '%s'", a.cfg.Name)
	return result
}
