package agent

import (
	"fmt"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
	"github.com/michaelquigley/sexton/internal/llm"
)

type Container struct {
	LLM     *llm.Client
	Alerter Alerter
	Agents  []*Agent
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

	c := &Container{
		LLM:     llm.NewClient(cfg.LLM),
		Alerter: &LogAlerter{},
	}

	for _, entry := range cfg.Repos {
		local, err := config.LoadRepoLocal(config.ExpandPath(entry.Path))
		if err != nil {
			dl.Warnf("failed to load repo-local config for '%s': %v", entry.Path, err)
			local = &config.RepoLocalConfig{}
		}

		resolved, err := config.Resolve(entry, defaults, local)
		if err != nil {
			dl.Warnf("failed to resolve config for '%s': %v", entry.Path, err)
			continue
		}

		g := git.New(resolved.Path)
		if g == nil {
			dl.Warnf("'%s' is not a git repository, skipping", resolved.Name)
			continue
		}

		c.Agents = append(c.Agents, New(resolved, g))
	}

	if len(c.Agents) == 0 {
		return nil, fmt.Errorf("no valid repos to watch")
	}
	if err := validateRepoIdentifiers(c.Agents); err != nil {
		return nil, err
	}

	dl.Infof("starting sexton with %d repo(s)", len(c.Agents))

	return c, nil
}

func validateRepoIdentifiers(agents []*Agent) error {
	paths := make(map[string]string, len(agents))
	names := make(map[string]string, len(agents))

	for _, ag := range agents {
		if prev, ok := paths[ag.cfg.Path]; ok {
			return fmt.Errorf("duplicate repo path %q for %q and %q", ag.cfg.Path, prev, ag.cfg.Name)
		}
		paths[ag.cfg.Path] = ag.cfg.Name

		if !ag.cfg.ExplicitName {
			continue
		}
		if prev, ok := names[ag.cfg.Name]; ok {
			return fmt.Errorf("duplicate configured repo name %q for %q and %q", ag.cfg.Name, prev, ag.cfg.Path)
		}
		names[ag.cfg.Name] = ag.cfg.Path
	}

	return nil
}
