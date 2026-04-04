package rpc

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrRepoNotFound = errors.New("repo not found")
var ErrAmbiguousRepo = errors.New("ambiguous repo")

type AmbiguousRepoError struct {
	Query   string
	Matches []string
}

func NewAmbiguousRepoError(query string, matches []string) error {
	copied := append([]string(nil), matches...)
	return &AmbiguousRepoError{Query: query, Matches: copied}
}

func (e *AmbiguousRepoError) Error() string {
	return fmt.Sprintf("ambiguous repo %q; matches: %s; use configured name or full path", e.Query, strings.Join(e.Matches, ", "))
}

func (e *AmbiguousRepoError) Unwrap() error {
	return ErrAmbiguousRepo
}

// RepoInfo holds status information for a single monitored repo.
type RepoInfo struct {
	Path             string
	Name             string
	State            string
	Branch           string
	LastSync         time.Time
	LastCommit       string
	LastChange       time.Time
	Error            string
	SnoozeRemaining  time.Duration
	HoldoutRemaining time.Duration
}

// AgentController is the interface that the gRPC handler uses to interact with
// the agent container. it is satisfied by the adapter in cmd/sexton, avoiding
// circular imports between agent and rpc.
type AgentController interface {
	RepoStatus(repo string) ([]RepoInfo, error)
	TriggerSync(repo string) error
	SnoozeRepo(repo string, d time.Duration) (time.Time, error)
	ResumeRepo(repo string) (string, error)
}
