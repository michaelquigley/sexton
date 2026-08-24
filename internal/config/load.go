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

	if err := validateLLM(cfg.LLM); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateLLM rejects the one invalid llm combination. omitting the block
// entirely is supported and disables summarization, but a 'model' without an
// 'endpoint' builds no client at all, so the setting silently does nothing.
func validateLLM(cfg *LLMConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.Model != "" && cfg.Endpoint == "" {
		return fmt.Errorf("llm.model is set to %q but llm.endpoint is empty; set an endpoint or remove the llm block to use mechanical commit messages", cfg.Model)
	}
	return nil
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

	holdoutWindows, err := resolveHoldoutWindows(defaults.HoldoutWindows, entry.HoldoutWindows, local.HoldoutWindows)
	if err != nil {
		return nil, err
	}

	sshKey := coalesce(local.SSHKey, entry.SSHKey, defaults.SSHKey)
	if sshKey != "" {
		sshKey = ExpandPath(sshKey)
	}

	commitPolicy := coalesce(local.CommitPolicy, entry.CommitPolicy, defaults.CommitPolicy)
	policyDefaulted := commitPolicy == ""
	if policyDefaulted {
		commitPolicy = PolicyNone
	}
	if err := validateCommitPolicy(commitPolicy); err != nil {
		return nil, err
	}

	commitRegions, err := resolveCommitRegions(local.CommitRegions, entry.CommitRegions, defaults.CommitRegions)
	if err != nil {
		return nil, err
	}
	if commitPolicy == PolicyRegions && len(commitRegions) == 0 {
		return nil, fmt.Errorf("commit_policy is %q but commit_regions is empty; configure at least one region or use commit_policy: %s", PolicyRegions, PolicyAll)
	}

	return &ResolvedRepo{
		Path:                path,
		Name:                name,
		ExplicitName:        explicitName,
		PollInterval:        poll,
		Branch:              coalesce(local.Branch, entry.Branch, defaults.Branch, "main"),
		Remote:              coalesce(local.Remote, entry.Remote, defaults.Remote, "origin"),
		SSHKey:              sshKey,
		CommitMessagePrompt: coalesce(local.CommitMessagePrompt, entry.CommitMessagePrompt, defaults.CommitMessagePrompt, DefaultCommitMessagePrompt),
		CommitPolicy:        commitPolicy,
		CommitRegions:       commitRegions,
		PolicyDefaulted:     policyDefaulted,
		HoldoutWindows:      holdoutWindows,
		Hooks:               hooks,
	}, nil
}

func validateCommitPolicy(policy string) error {
	switch policy {
	case PolicyAll, PolicyRegions, PolicyNone:
		return nil
	default:
		return fmt.Errorf("invalid commit_policy %q; must be one of %q, %q, or %q", policy, PolicyAll, PolicyRegions, PolicyNone)
	}
}

func resolveCommitRegions(local, entry, defaults []string) ([]string, error) {
	var regions []string
	switch {
	case len(local) > 0:
		regions = local
	case len(entry) > 0:
		regions = entry
	case len(defaults) > 0:
		regions = defaults
	default:
		return nil, nil
	}

	resolved := make([]string, len(regions))
	for i, region := range regions {
		normalized, err := normalizeCommitRegion(region)
		if err != nil {
			return nil, fmt.Errorf("invalid commit_regions entry %q: %w", region, err)
		}
		resolved[i] = normalized
	}
	return resolved, nil
}

func normalizeCommitRegion(region string) (string, error) {
	if region == "" {
		return "", fmt.Errorf("region must not be empty")
	}
	if filepath.IsAbs(region) {
		return "", fmt.Errorf("region must be relative to the repository root")
	}

	cleaned := filepath.Clean(region)
	if cleaned == "." {
		return "", fmt.Errorf("region cannot select the whole repository; use commit_policy: %s", PolicyAll)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("region must not escape the repository root")
	}

	return filepath.ToSlash(cleaned) + "/", nil
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
