package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveHookDirLeavesEmptyDirUnset(t *testing.T) {
	repoRoot := t.TempDir()
	resolved, err := Resolve(
		&RepoEntry{
			Path: repoRoot,
			Hooks: &HooksConfig{
				PostPull: []*HookEntry{{Command: "true"}},
			},
		},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got := resolved.Hooks.PostPull[0].Dir; got != "" {
		t.Fatalf("resolved hook dir = %q, want empty", got)
	}
}

func TestResolveHookDirLeavesAbsolutePathUnchanged(t *testing.T) {
	repoRoot := t.TempDir()
	absoluteDir := filepath.Join(t.TempDir(), "hooks")
	resolved, err := Resolve(
		&RepoEntry{
			Path: repoRoot,
			Hooks: &HooksConfig{
				PostPull: []*HookEntry{{Command: "true", Dir: absoluteDir}},
			},
		},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got := resolved.Hooks.PostPull[0].Dir; got != absoluteDir {
		t.Fatalf("resolved hook dir = %q, want %q", got, absoluteDir)
	}
}

func TestResolveHookDirMakesRelativePathRepoRelative(t *testing.T) {
	repoRoot := t.TempDir()
	resolved, err := Resolve(
		&RepoEntry{
			Path: repoRoot,
			Hooks: &HooksConfig{
				PostPull: []*HookEntry{{Command: "true", Dir: "scripts"}},
			},
		},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Join(repoRoot, "scripts")
	if got := resolved.Hooks.PostPull[0].Dir; got != want {
		t.Fatalf("resolved hook dir = %q, want %q", got, want)
	}
}

func TestResolveHookDirMakesEnvExpandedRelativePathRepoRelative(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEXTON_HOOK_DIR", "scripts")

	resolved, err := Resolve(
		&RepoEntry{
			Path: repoRoot,
			Hooks: &HooksConfig{
				PostPull: []*HookEntry{{Command: "true", Dir: "$SEXTON_HOOK_DIR"}},
			},
		},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Join(repoRoot, "scripts")
	if got := resolved.Hooks.PostPull[0].Dir; got != want {
		t.Fatalf("resolved hook dir = %q, want %q", got, want)
	}
}

func TestResolveHookDirExpandsHomePath(t *testing.T) {
	repoRoot := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}

	resolved, err := Resolve(
		&RepoEntry{
			Path: repoRoot,
			Hooks: &HooksConfig{
				PostPull: []*HookEntry{{Command: "true", Dir: "~/hooks"}},
			},
		},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Join(home, "hooks")
	if got := resolved.Hooks.PostPull[0].Dir; got != want {
		t.Fatalf("resolved hook dir = %q, want %q", got, want)
	}
}

func TestLoadRejectsModelWithoutEndpoint(t *testing.T) {
	content := `
llm:
  model: gpt-5.6
repos:
  - path: /tmp/repo
`
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := Load(configPath); err == nil {
		t.Fatal("Load() error = nil, want an error naming the empty endpoint")
	} else if !strings.Contains(err.Error(), "llm.endpoint") {
		t.Fatalf("Load() error = %v, want it to name llm.endpoint", err)
	}
}

func TestLoadAcceptsNoLLMBlock(t *testing.T) {
	content := `
repos:
  - path: /tmp/repo
`
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil — omitting the llm block is supported", err)
	}
	if cfg.LLM != nil {
		t.Fatalf("cfg.LLM = %+v, want nil", cfg.LLM)
	}
}

func TestLoadBindsLLMAPIKey(t *testing.T) {
	content := `
llm:
  endpoint: http://localhost:8080/v1/chat/completions
  model: gpt-5.6
  api_key: test-key
repos:
  - path: /tmp/repo
`
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.LLM == nil {
		t.Fatal("cfg.LLM = nil, want llm config")
	}
	if cfg.LLM.APIKey != "test-key" {
		t.Fatalf("cfg.LLM.APIKey = %q, want %q", cfg.LLM.APIKey, "test-key")
	}
}

func TestLoadRejectsMattermostConfigMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "missing url",
			content: `
alerts:
  - type: mattermost
    mattermost:
      channel_id: chan-1
      token_env: MM_TOKEN
`,
			want: "required field missing",
		},
		{
			name: "missing channel_id",
			content: `
alerts:
  - type: mattermost
    mattermost:
      url: https://mm.local
      token_env: MM_TOKEN
`,
			want: "required field missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(configPath, []byte(strings.TrimSpace(tt.content)), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			_, err := Load(configPath)
			if err == nil {
				t.Fatal("Load() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestResolveSSHKeyCascadeAndExpansion(t *testing.T) {
	repoRoot := t.TempDir()

	// repo-local wins over global entry and defaults
	resolved, err := Resolve(
		&RepoEntry{Path: repoRoot, SSHKey: "/entry/key"},
		&RepoDefaults{SSHKey: "/defaults/key"},
		&RepoLocalConfig{SSHKey: "/local/key"},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.SSHKey != "/local/key" {
		t.Fatalf("resolved ssh key = %q, want /local/key", resolved.SSHKey)
	}

	// falls back to defaults when entry and local are unset
	resolved, err = Resolve(
		&RepoEntry{Path: repoRoot},
		&RepoDefaults{SSHKey: "/defaults/key"},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.SSHKey != "/defaults/key" {
		t.Fatalf("resolved ssh key = %q, want /defaults/key", resolved.SSHKey)
	}

	// ~ expands to the home directory
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	resolved, err = Resolve(
		&RepoEntry{Path: repoRoot, SSHKey: "~/.ssh/deploy"},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := filepath.Join(home, ".ssh/deploy"); resolved.SSHKey != want {
		t.Fatalf("resolved ssh key = %q, want %q", resolved.SSHKey, want)
	}

	// unset stays empty
	resolved, err = Resolve(
		&RepoEntry{Path: repoRoot},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.SSHKey != "" {
		t.Fatalf("resolved ssh key = %q, want empty", resolved.SSHKey)
	}
}

func TestLoadRepoLocalBindsCommitPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	content := `
commit_policy: regions
commit_regions:
  - journal/
`
	if err := os.WriteFile(filepath.Join(repoRoot, ".sexton.yaml"), []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadRepoLocal(repoRoot)
	if err != nil {
		t.Fatalf("LoadRepoLocal() error = %v", err)
	}
	if cfg.CommitPolicy != PolicyRegions {
		t.Fatalf("commit policy = %q, want %q", cfg.CommitPolicy, PolicyRegions)
	}
	if len(cfg.CommitRegions) != 1 || cfg.CommitRegions[0] != "journal/" {
		t.Fatalf("commit regions = %#v, want []string{%q}", cfg.CommitRegions, "journal/")
	}
}

func TestLoadBindsCommitPolicyAtGlobalLayers(t *testing.T) {
	content := `
defaults:
  commit_policy: all
  commit_regions:
    - defaults/
repos:
  - path: /tmp/repo
    commit_policy: regions
    commit_regions:
      - journal/
`
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Defaults.CommitPolicy != PolicyAll || len(cfg.Defaults.CommitRegions) != 1 || cfg.Defaults.CommitRegions[0] != "defaults/" {
		t.Fatalf("defaults commit policy = %q, regions = %#v", cfg.Defaults.CommitPolicy, cfg.Defaults.CommitRegions)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].CommitPolicy != PolicyRegions || len(cfg.Repos[0].CommitRegions) != 1 || cfg.Repos[0].CommitRegions[0] != "journal/" {
		t.Fatalf("repo commit config = %#v, want regions policy for journal/", cfg.Repos)
	}
}

func TestLoadBindsMattermostMentionUsers(t *testing.T) {
	content := `
alerts:
  - type: mattermost
    mattermost:
      url: https://mattermost.example.com
      token: secret
      channel_id: alerts
      mention_users:
        - michael
        - alice
repos:
  - path: /tmp/repo
`
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Alerts) != 1 || cfg.Alerts[0].Mattermost == nil {
		t.Fatalf("alerts = %#v", cfg.Alerts)
	}
	if got := cfg.Alerts[0].Mattermost.MentionUsers; !reflect.DeepEqual(got, []string{"michael", "alice"}) {
		t.Fatalf("mention users = %#v", got)
	}
}

func TestResolveCommitPolicyDefaultsToNone(t *testing.T) {
	resolved, err := Resolve(
		&RepoEntry{Path: t.TempDir()},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.CommitPolicy != PolicyNone {
		t.Fatalf("commit policy = %q, want %q", resolved.CommitPolicy, PolicyNone)
	}
	if !resolved.PolicyDefaulted {
		t.Fatal("PolicyDefaulted = false, want true")
	}
}

func TestResolveExplicitCommitPolicyAtEachLayerClearsDefaultFlag(t *testing.T) {
	tests := []struct {
		name     string
		local    *RepoLocalConfig
		entry    *RepoEntry
		defaults *RepoDefaults
		want     string
	}{
		{
			name:     "repo local",
			local:    &RepoLocalConfig{CommitPolicy: PolicyNone},
			entry:    &RepoEntry{CommitPolicy: PolicyAll},
			defaults: &RepoDefaults{CommitPolicy: PolicyAll},
			want:     PolicyNone,
		},
		{
			name:     "global repo entry",
			local:    &RepoLocalConfig{},
			entry:    &RepoEntry{CommitPolicy: PolicyAll},
			defaults: &RepoDefaults{CommitPolicy: PolicyNone},
			want:     PolicyAll,
		},
		{
			name:     "global defaults",
			local:    &RepoLocalConfig{},
			entry:    &RepoEntry{},
			defaults: &RepoDefaults{CommitPolicy: PolicyAll},
			want:     PolicyAll,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.entry.Path = t.TempDir()
			resolved, err := Resolve(tt.entry, tt.defaults, tt.local)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if resolved.CommitPolicy != tt.want {
				t.Fatalf("commit policy = %q, want %q", resolved.CommitPolicy, tt.want)
			}
			if resolved.PolicyDefaulted {
				t.Fatal("PolicyDefaulted = true, want false")
			}
		})
	}
}

func TestResolveCommitRegionsReplaceByLayer(t *testing.T) {
	tests := []struct {
		name     string
		local    []string
		entry    []string
		defaults []string
		want     string
	}{
		{
			name:     "repo local replaces lower layers",
			local:    []string{"local"},
			entry:    []string{"entry"},
			defaults: []string{"defaults"},
			want:     "local/",
		},
		{
			name:     "global repo entry replaces defaults",
			entry:    []string{"entry"},
			defaults: []string{"defaults"},
			want:     "entry/",
		},
		{
			name:     "global defaults are fallback",
			defaults: []string{"defaults"},
			want:     "defaults/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := Resolve(
				&RepoEntry{Path: t.TempDir(), CommitRegions: tt.entry},
				&RepoDefaults{CommitPolicy: PolicyRegions, CommitRegions: tt.defaults},
				&RepoLocalConfig{CommitRegions: tt.local},
			)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if len(resolved.CommitRegions) != 1 || resolved.CommitRegions[0] != tt.want {
				t.Fatalf("commit regions = %#v, want []string{%q}", resolved.CommitRegions, tt.want)
			}
		})
	}
}

func TestResolveNormalizesCommitRegions(t *testing.T) {
	resolved, err := Resolve(
		&RepoEntry{Path: t.TempDir()},
		&RepoDefaults{},
		&RepoLocalConfig{CommitPolicy: PolicyRegions, CommitRegions: []string{"./journal"}},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved.CommitRegions) != 1 || resolved.CommitRegions[0] != "journal/" {
		t.Fatalf("commit regions = %#v, want []string{%q}", resolved.CommitRegions, "journal/")
	}
	if strings.HasPrefix("journal-drafts/entry.md", resolved.CommitRegions[0]) {
		t.Fatal("normalized journal region matched the journal-drafts near miss")
	}
}

func TestResolveRejectsInvalidCommitPolicyConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		regions []string
		want    string
	}{
		{name: "unknown policy", policy: "sometimes", want: `must be one of "all", "regions", or "none"`},
		{name: "regions without paths", policy: PolicyRegions, want: "commit_regions is empty"},
		{name: "empty region", policy: PolicyRegions, regions: []string{""}, want: "must not be empty"},
		{name: "absolute region", policy: PolicyRegions, regions: []string{"/journal"}, want: "relative"},
		{name: "whole repo", policy: PolicyRegions, regions: []string{"."}, want: "commit_policy: all"},
		{name: "parent region", policy: PolicyRegions, regions: []string{"../journal"}, want: "must not escape"},
		{name: "cleaned escape", policy: PolicyRegions, regions: []string{"journal/../../outside"}, want: "must not escape"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(
				&RepoEntry{Path: t.TempDir()},
				&RepoDefaults{},
				&RepoLocalConfig{CommitPolicy: tt.policy, CommitRegions: tt.regions},
			)
			if err == nil {
				t.Fatal("Resolve() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Resolve() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestResolveAllowsInertCommitRegions(t *testing.T) {
	for _, policy := range []string{PolicyAll, PolicyNone} {
		t.Run(policy, func(t *testing.T) {
			resolved, err := Resolve(
				&RepoEntry{Path: t.TempDir()},
				&RepoDefaults{},
				&RepoLocalConfig{CommitPolicy: policy, CommitRegions: []string{"journal"}},
			)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if len(resolved.CommitRegions) != 1 || resolved.CommitRegions[0] != "journal/" {
				t.Fatalf("commit regions = %#v, want inert normalized region", resolved.CommitRegions)
			}
		})
	}
}
