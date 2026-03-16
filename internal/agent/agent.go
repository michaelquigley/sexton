package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
)

type Agent struct {
	cfg     *config.ResolvedRepo
	git     *git.Git
	alerter Alerter

	mu    sync.Mutex
	state State

	stopCh chan struct{}
	doneCh chan struct{}

	lastSync   time.Time
	lastCommit string
}

func New(cfg *config.ResolvedRepo, g *git.Git, alerter Alerter) *Agent {
	return &Agent{
		cfg:     cfg,
		git:     g,
		alerter: alerter,
		state:   Watching,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
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

func (a *Agent) run() {
	defer close(a.doneCh)

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	// run one sync immediately on start
	a.sync()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.sync()
		}
	}
}

func (a *Agent) sync() {
	a.mu.Lock()
	if a.state == Halted {
		a.mu.Unlock()
		return
	}
	a.state = Syncing
	a.mu.Unlock()

	ctx := context.Background()

	dirty, err := a.git.IsDirty()
	if err != nil {
		a.halt("failed to check status", err)
		return
	}

	if dirty {
		status, _ := a.git.Status()
		msg := git.GenerateCommitMessage(status)

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

	if dirty {
		a.alert("info", "sync complete ("+sha+")", nil)
	}
}

func (a *Agent) halt(message string, err error) {
	a.mu.Lock()
	a.state = Halted
	a.mu.Unlock()
	a.alert("error", message, err)
}

func (a *Agent) alert(severity, message string, err error) {
	_ = a.alerter.Alert(context.Background(), AlertEvent{
		Severity:  severity,
		RepoPath:  a.cfg.Path,
		Message:   message,
		Error:     err,
		Timestamp: time.Now(),
	})
}
