package agent

import (
	"fmt"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
)

type Container struct {
	Agents []*Agent
}

func NewContainer(cfg *config.GlobalConfig) (*Container, error) {
	if len(cfg.Repos) == 0 {
		return nil, fmt.Errorf("no repos configured")
	}

	defaults := cfg.Defaults
	if defaults == nil {
		defaults = &config.RepoDefaults{
			PollInterval: "30s",
			Branch:       "main",
			Remote:       "origin",
		}
	}

	alerter := &LogAlerter{}

	var agents []*Agent
	for _, entry := range cfg.Repos {
		local, err := config.LoadRepoLocal(config.ExpandPath(entry.Path))
		if err != nil {
			dl.Warnf("failed to load repo-local config for %s: %v", entry.Path, err)
			local = &config.RepoLocalConfig{}
		}

		resolved, err := config.Resolve(entry, defaults, local)
		if err != nil {
			dl.Warnf("failed to resolve config for %s: %v", entry.Path, err)
			continue
		}

		g := git.New(resolved.Path)
		if g == nil {
			dl.Warnf("%s is not a git repository, skipping", resolved.Path)
			continue
		}

		agents = append(agents, New(resolved, g, alerter))
	}

	if len(agents) == 0 {
		return nil, fmt.Errorf("no valid repos to watch")
	}

	dl.Infof("starting sexton with %d repo(s)", len(agents))

	return &Container{Agents: agents}, nil
}
