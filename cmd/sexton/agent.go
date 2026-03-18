package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/michaelquigley/df/da"
	"github.com/michaelquigley/sexton/internal/agent"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/rpc"
	"github.com/spf13/cobra"
)

var agentConfigPath string

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "run the sync agent",
	Args:  cobra.NoArgs,
	RunE:  runAgent,
}

func init() {
	agentCmd.Flags().StringVar(&agentConfigPath, "config", "", "path to config file (default: ~/.config/sexton/config.yaml)")
}

func runAgent(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(agentConfigPath)
	if err != nil {
		return err
	}

	c, err := agent.NewContainer(cfg)
	if err != nil {
		return err
	}

	srv := rpc.NewServer(config.SocketPath(), &containerAdapter{c: c})
	if err := srv.Start(); err != nil {
		return err
	}
	defer srv.Stop()

	return da.Run(c)
}

// containerAdapter bridges agent.Container to rpc.AgentController without
// requiring the agent package to import rpc.
type containerAdapter struct {
	c *agent.Container
}

func (a *containerAdapter) RepoStatus(repo string) ([]rpc.RepoInfo, error) {
	if repo != "" {
		ag, err := a.resolveAgent(repo)
		if err != nil {
			return nil, err
		}
		return []rpc.RepoInfo{agentToRepoInfo(ag)}, nil
	}

	var infos []rpc.RepoInfo
	for _, ag := range a.c.Agents {
		infos = append(infos, agentToRepoInfo(ag))
	}
	return infos, nil
}

func (a *containerAdapter) TriggerSync(repo string) error {
	ag, err := a.resolveAgent(repo)
	if err != nil {
		return err
	}
	return ag.TriggerSync()
}

func (a *containerAdapter) SnoozeRepo(repo string, d time.Duration) (time.Time, error) {
	ag, err := a.resolveAgent(repo)
	if err != nil {
		return time.Time{}, err
	}
	return ag.Snooze(d)
}

func (a *containerAdapter) ResumeRepo(repo string) error {
	ag, err := a.resolveAgent(repo)
	if err != nil {
		return err
	}
	return ag.Resume()
}

func (a *containerAdapter) resolveAgent(repo string) (*agent.Agent, error) {
	ag, err := a.c.ResolveAgent(repo)
	if err == nil {
		return ag, nil
	}

	var lookupErr *agent.LookupError
	if errors.As(err, &lookupErr) {
		switch {
		case errors.Is(err, agent.ErrRepoNotFound):
			return nil, fmt.Errorf("%w: %q", rpc.ErrRepoNotFound, lookupErr.Query)
		case errors.Is(err, agent.ErrAmbiguousRepo):
			return nil, rpc.NewAmbiguousRepoError(lookupErr.Query, lookupErr.Matches)
		}
	}

	return nil, err
}

func agentToRepoInfo(ag *agent.Agent) rpc.RepoInfo {
	info := rpc.RepoInfo{
		Path:            ag.Path(),
		Name:            ag.Name(),
		State:           ag.State().String(),
		Branch:          ag.Branch(),
		LastSync:        ag.LastSync(),
		LastCommit:      ag.LastCommit(),
		SnoozeRemaining: ag.SnoozeRemaining(),
	}
	if detail := ag.ErrorDetail(); detail != "" {
		info.Error = detail
	}
	return info
}
