package rpc

import (
	"errors"
	"time"
)

var ErrRepoNotFound = errors.New("repo not found")

// RepoInfo holds status information for a single monitored repo.
type RepoInfo struct {
	Path            string
	Name            string
	State           string
	Branch          string
	LastSync        time.Time
	LastCommit      string
	Error           string
	SnoozeRemaining time.Duration
}

// AgentController is the interface that the gRPC handler uses to interact with
// the agent container. it is satisfied by the adapter in cmd/sexton, avoiding
// circular imports between agent and rpc.
type AgentController interface {
	RepoStatus(repo string) ([]RepoInfo, error)
	TriggerSync(repo string) error
	SnoozeRepo(repo string, d time.Duration) (time.Time, error)
	ResumeRepo(repo string) error
}
