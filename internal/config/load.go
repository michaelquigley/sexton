package config

import (
	"errors"
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

	return &ResolvedRepo{
		Path:                ExpandPath(entry.Path),
		PollInterval:        poll,
		Branch:              coalesce(local.Branch, entry.Branch, defaults.Branch, "main"),
		Remote:              coalesce(local.Remote, entry.Remote, defaults.Remote, "origin"),
		CommitMessagePrompt: coalesce(local.CommitMessagePrompt, entry.CommitMessagePrompt, defaults.CommitMessagePrompt, DefaultCommitMessagePrompt),
	}, nil
}

func GlobalConfigPath() string {
	return filepath.Join(globalConfigDir(), "config.yaml")
}

func globalConfigDir() string {
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
