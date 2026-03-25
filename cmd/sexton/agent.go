package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/michaelquigley/df/da"
	"github.com/michaelquigley/sexton/internal/agent"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/mattermost"
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

	adapter := &containerAdapter{c: c}

	srv := rpc.NewServer(config.SocketPath(), adapter)
	if err := srv.Start(); err != nil {
		return err
	}
	defer srv.Stop()

	alerter, mmCleanup, err := buildAlerter(cfg.Alerts, adapter)
	if err != nil {
		return err
	}
	if mmCleanup != nil {
		defer mmCleanup()
	}
	c.Alerter = alerter

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
		LastChange:      ag.LastChange(),
		SnoozeRemaining: ag.SnoozeRemaining(),
	}
	if detail := ag.ErrorDetail(); detail != "" {
		info.Error = detail
	}
	return info
}

// buildAlerter constructs an alerter from the config. Returns the alerter and
// an optional cleanup function for mattermost clients.
func buildAlerter(alerts []*config.AlertConfig, adapter *containerAdapter) (agent.Alerter, func(), error) {
	if len(alerts) == 0 {
		return &agent.LogAlerter{}, nil, nil
	}

	var alerters []agent.Alerter
	var mmClients []*mattermost.Client

	for _, ac := range alerts {
		switch ac.Type {
		case "log", "":
			alerters = append(alerters, &agent.LogAlerter{})
		case "mattermost":
			if ac.Mattermost == nil {
				return nil, nil, fmt.Errorf("alert type 'mattermost' requires a mattermost config block")
			}
			mc := mattermost.NewClient(ac.Mattermost)
			ma := &mattermostAdapter{ca: adapter}
			if err := mc.Start(ma); err != nil {
				// stop any already-started clients
				for _, c := range mmClients {
					c.Stop()
				}
				return nil, nil, fmt.Errorf("mattermost client start failed: %w", err)
			}
			mmClients = append(mmClients, mc)
			alerters = append(alerters, mattermost.NewAlerter(mc, ac.Mattermost.ChannelID))
		default:
			return nil, nil, fmt.Errorf("unknown alert type '%s'", ac.Type)
		}
	}

	var cleanup func()
	if len(mmClients) > 0 {
		cleanup = func() {
			for _, c := range mmClients {
				c.Stop()
			}
		}
	}

	if len(alerters) == 1 {
		return alerters[0], cleanup, nil
	}
	return &agent.MultiAlerter{Alerters: alerters}, cleanup, nil
}

// mattermostAdapter bridges containerAdapter to mattermost.CommandHandler.
type mattermostAdapter struct {
	ca *containerAdapter
}

func (a *mattermostAdapter) Status(repo string) ([]mattermost.RepoStatus, error) {
	infos, err := a.ca.RepoStatus(repo)
	if err != nil {
		return nil, err
	}
	var out []mattermost.RepoStatus
	for _, info := range infos {
		out = append(out, mattermost.RepoStatus{
			Name:            info.Name,
			Path:            info.Path,
			State:           info.State,
			Branch:          info.Branch,
			LastSync:        info.LastSync,
			LastCommit:      info.LastCommit,
			LastChange:      info.LastChange,
			Error:           info.Error,
			SnoozeRemaining: info.SnoozeRemaining,
		})
	}
	return out, nil
}

func (a *mattermostAdapter) Sync(repo string) error {
	return a.ca.TriggerSync(repo)
}

func (a *mattermostAdapter) Snooze(repo string, d time.Duration) (time.Time, error) {
	return a.ca.SnoozeRepo(repo, d)
}

func (a *mattermostAdapter) Resume(repo string) error {
	return a.ca.ResumeRepo(repo)
}
