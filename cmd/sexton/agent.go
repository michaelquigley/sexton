package main

import (
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
		ag := a.c.FindAgent(repo)
		if ag == nil {
			return nil, fmt.Errorf("repo '%s' not found", repo)
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
	ag := a.c.FindAgent(repo)
	if ag == nil {
		return fmt.Errorf("repo '%s' not found", repo)
	}
	return ag.TriggerSync()
}

func (a *containerAdapter) SnoozeRepo(repo string, d time.Duration) (time.Time, error) {
	ag := a.c.FindAgent(repo)
	if ag == nil {
		return time.Time{}, fmt.Errorf("repo '%s' not found", repo)
	}
	return ag.Snooze(d)
}

func (a *containerAdapter) ResumeRepo(repo string) error {
	ag := a.c.FindAgent(repo)
	if ag == nil {
		return fmt.Errorf("repo '%s' not found", repo)
	}
	return ag.Resume()
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
	if err := ag.HaltError(); err != nil {
		info.Error = err.Error()
	}
	return info
}
