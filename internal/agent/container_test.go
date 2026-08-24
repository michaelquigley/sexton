package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/michaelquigley/sexton/internal/config"
)

func TestNewContainerForcesNoneAfterMalformedRepoLocalConfig(t *testing.T) {
	repoRoot := t.TempDir()
	cmd := exec.Command("git", "init", "-q", repoRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init error = %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".sexton.yaml"), []byte("commit_policy: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	container, err := NewContainer(&config.GlobalConfig{
		Defaults: &config.RepoDefaults{CommitPolicy: config.PolicyAll},
		Repos:    []*config.RepoEntry{{Path: repoRoot}},
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	if len(container.Agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(container.Agents))
	}

	resolved := container.Agents[0].cfg
	if resolved.CommitPolicy != config.PolicyNone {
		t.Fatalf("commit policy = %q, want forced %q", resolved.CommitPolicy, config.PolicyNone)
	}
	if resolved.PolicyDefaulted {
		t.Fatal("PolicyDefaulted = true, want false for a forced policy")
	}
	if resolved.LocalConfigError == nil {
		t.Fatal("LocalConfigError = nil, want malformed repo-local config error")
	}
}
