package config

import (
	"os"
	"path/filepath"
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
