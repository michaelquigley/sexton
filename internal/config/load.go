package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michaelquigley/df/dd"
)

func Load(configPath string) (*GlobalConfig, error) {
	cfg := defaultGlobalConfig()

	path := configPath
	if path == "" {
		path = GlobalConfigPath()
	}

	err := dd.MergeYAMLFile(cfg, path)
	if err != nil {
		var fileErr *dd.FileError
		if errors.As(err, &fileErr) && fileErr.IsNotFound() {
			return cfg, nil
		}
		return nil, err
	}

	// expand ~ in repo paths
	for _, r := range cfg.Repos {
		r.Path = ExpandPath(r.Path)
	}

	return cfg, nil
}

func LoadRepoLocal(repoRoot string) (*RepoLocalConfig, error) {
	path := filepath.Join(repoRoot, ".sexton.yaml")

	cfg := &RepoLocalConfig{}

	err := dd.MergeYAMLFile(cfg, path)
	if err != nil {
		var fileErr *dd.FileError
		if errors.As(err, &fileErr) && fileErr.IsNotFound() {
			return cfg, nil
		}
		return nil, err
	}

	return cfg, nil
}

func Resolve(entry *RepoEntry, defaults *RepoDefaults, local *RepoLocalConfig) (*ResolvedRepo, error) {
	pollStr := coalesce(local.PollInterval, entry.PollInterval, defaults.PollInterval, "30s")
	poll, err := time.ParseDuration(pollStr)
	if err != nil {
		return nil, err
	}

	path := ExpandPath(entry.Path)
	explicitName := local.Name != "" || entry.Name != ""
	name := coalesce(local.Name, entry.Name, filepath.Base(path))

	hooks, err := resolveHooks(path, defaults.Hooks, entry.Hooks, local.Hooks)
	if err != nil {
		return nil, err
	}

	return &ResolvedRepo{
		Path:                path,
		Name:                name,
		ExplicitName:        explicitName,
		PollInterval:        poll,
		Branch:              coalesce(local.Branch, entry.Branch, defaults.Branch, "main"),
		Remote:              coalesce(local.Remote, entry.Remote, defaults.Remote, "origin"),
		CommitMessagePrompt: coalesce(local.CommitMessagePrompt, entry.CommitMessagePrompt, defaults.CommitMessagePrompt, DefaultCommitMessagePrompt),
		Hooks:               hooks,
	}, nil
}

const defaultHookTimeout = 30 * time.Second

func resolveHooks(repoRoot string, defaults, entry, local *HooksConfig) (*ResolvedHooks, error) {
	resolved := &ResolvedHooks{}

	resolvePhase := func(localPhase, entryPhase, defaultsPhase []*HookEntry) ([]*ResolvedHook, error) {
		var entries []*HookEntry
		switch {
		case len(localPhase) > 0:
			entries = localPhase
		case len(entryPhase) > 0:
			entries = entryPhase
		case len(defaultsPhase) > 0:
			entries = defaultsPhase
		default:
			return nil, nil
		}

		hooks := make([]*ResolvedHook, len(entries))
		for i, e := range entries {
			timeout := defaultHookTimeout
			if e.Timeout != "" {
				var err error
				timeout, err = time.ParseDuration(e.Timeout)
				if err != nil {
					return nil, fmt.Errorf("invalid hook timeout %q: %w", e.Timeout, err)
				}
			}
			hooks[i] = &ResolvedHook{
				Command: e.Command,
				Timeout: timeout,
				Dir:     resolveHookDir(repoRoot, e.Dir),
				Env:     e.Env,
			}
		}
		return hooks, nil
	}

	var localHooks, entryHooks, defaultHooks HooksConfig
	if local != nil {
		localHooks = *local
	}
	if entry != nil {
		entryHooks = *entry
	}
	if defaults != nil {
		defaultHooks = *defaults
	}

	var err error
	if resolved.PreCommit, err = resolvePhase(localHooks.PreCommit, entryHooks.PreCommit, defaultHooks.PreCommit); err != nil {
		return nil, err
	}
	if resolved.PostCommit, err = resolvePhase(localHooks.PostCommit, entryHooks.PostCommit, defaultHooks.PostCommit); err != nil {
		return nil, err
	}
	if resolved.PostPull, err = resolvePhase(localHooks.PostPull, entryHooks.PostPull, defaultHooks.PostPull); err != nil {
		return nil, err
	}
	if resolved.PrePush, err = resolvePhase(localHooks.PrePush, entryHooks.PrePush, defaultHooks.PrePush); err != nil {
		return nil, err
	}
	if resolved.PostSync, err = resolvePhase(localHooks.PostSync, entryHooks.PostSync, defaultHooks.PostSync); err != nil {
		return nil, err
	}

	return resolved, nil
}

func resolveHookDir(repoRoot, dir string) string {
	if dir == "" {
		return ""
	}

	expanded := ExpandPath(dir)
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded)
	}
	return filepath.Clean(filepath.Join(repoRoot, expanded))
}

func GlobalConfigPath() string {
	return filepath.Join(GlobalConfigDir(), "config.yaml")
}

func SocketPath() string {
	return filepath.Join(GlobalConfigDir(), "sexton.sock")
}

func GlobalConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sexton")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "sexton")
	}
	return filepath.Join(home, ".config", "sexton")
}

func ExpandPath(path string) string {
	if path == "" {
		return path
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	} else if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = home
		}
	}

	path = os.ExpandEnv(path)

	return path
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
