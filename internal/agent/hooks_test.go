package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/sexton/internal/config"
)

func newTestAgent(dir string) *Agent {
	return &Agent{
		cfg: &config.ResolvedRepo{
			Path:  dir,
			Name:  "test-repo",
			Hooks: &config.ResolvedHooks{},
		},
	}
}

func TestRunHooks_NilSlice(t *testing.T) {
	a := newTestAgent(t.TempDir())
	err := a.runHooks(context.Background(), "post_pull", nil)
	if err != nil {
		t.Fatalf("expected nil error for nil hooks, got: %v", err)
	}
}

func TestRunHooks_EmptySlice(t *testing.T) {
	a := newTestAgent(t.TempDir())
	err := a.runHooks(context.Background(), "post_pull", []*config.ResolvedHook{})
	if err != nil {
		t.Fatalf("expected nil error for empty hooks, got: %v", err)
	}
}

func TestRunHooks_Success(t *testing.T) {
	dir := t.TempDir()
	a := newTestAgent(dir)

	marker := filepath.Join(dir, "marker.txt")
	hooks := []*config.ResolvedHook{
		{Command: "echo hello > " + marker, Timeout: 10 * time.Second},
	}

	err := a.runHooks(context.Background(), "post_pull", hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("marker file was not created")
	}
}

func TestRunHooks_NonZeroExit(t *testing.T) {
	a := newTestAgent(t.TempDir())

	hooks := []*config.ResolvedHook{
		{Command: "exit 42", Timeout: 10 * time.Second},
	}

	err := a.runHooks(context.Background(), "pre_commit", hooks)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}

	if !strings.Contains(err.Error(), "pre_commit") {
		t.Errorf("error should contain phase name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "exit_code=42") {
		t.Errorf("error should contain exit code, got: %v", err)
	}
}

func TestRunHooks_Timeout(t *testing.T) {
	a := newTestAgent(t.TempDir())

	hooks := []*config.ResolvedHook{
		{Command: "sleep 60", Timeout: 100 * time.Millisecond},
	}

	start := time.Now()
	err := a.runHooks(context.Background(), "post_sync", hooks)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for timed-out hook")
	}

	if elapsed > 5*time.Second {
		t.Errorf("hook should have been killed quickly, took %v", elapsed)
	}
}

func TestRunHooks_FailFast(t *testing.T) {
	dir := t.TempDir()
	a := newTestAgent(dir)

	first := filepath.Join(dir, "first.txt")
	third := filepath.Join(dir, "third.txt")

	hooks := []*config.ResolvedHook{
		{Command: "touch " + first, Timeout: 10 * time.Second},
		{Command: "exit 1", Timeout: 10 * time.Second},
		{Command: "touch " + third, Timeout: 10 * time.Second},
	}

	err := a.runHooks(context.Background(), "post_pull", hooks)
	if err == nil {
		t.Fatal("expected error from second hook")
	}

	if _, err := os.Stat(first); os.IsNotExist(err) {
		t.Error("first hook should have run")
	}
	if _, err := os.Stat(third); !os.IsNotExist(err) {
		t.Error("third hook should NOT have run")
	}
}

func TestRunHooks_Environment(t *testing.T) {
	dir := t.TempDir()
	a := newTestAgent(dir)

	envFile := filepath.Join(dir, "env.txt")
	hooks := []*config.ResolvedHook{
		{Command: "env > " + envFile, Timeout: 10 * time.Second},
	}

	err := a.runHooks(context.Background(), "post_pull", hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	env := string(data)
	expectations := map[string]string{
		"SEXTON_REPO_PATH": dir,
		"SEXTON_REPO_NAME": "test-repo",
		"SEXTON_HOOK":      "post_pull",
	}

	for key, val := range expectations {
		expected := key + "=" + val
		if !strings.Contains(env, expected) {
			t.Errorf("expected %s in environment, got:\n%s", expected, env)
		}
	}
}

func TestRunHooks_CustomEnv(t *testing.T) {
	dir := t.TempDir()
	a := newTestAgent(dir)

	envFile := filepath.Join(dir, "env.txt")
	hooks := []*config.ResolvedHook{
		{
			Command: "env > " + envFile,
			Timeout: 10 * time.Second,
			Env: map[string]string{
				"MY_CUSTOM_VAR": "hello",
				"ANOTHER_VAR":   "world",
			},
		},
	}

	err := a.runHooks(context.Background(), "post_pull", hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	env := string(data)
	for _, expected := range []string{"MY_CUSTOM_VAR=hello", "ANOTHER_VAR=world"} {
		if !strings.Contains(env, expected) {
			t.Errorf("expected %s in environment, got:\n%s", expected, env)
		}
	}
}

func TestRunHooks_WorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	a := newTestAgent(dir)

	pwdFile := filepath.Join(dir, "pwd.txt")
	hooks := []*config.ResolvedHook{
		{Command: "pwd > " + pwdFile, Timeout: 10 * time.Second},
	}

	err := a.runHooks(context.Background(), "pre_push", hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("failed to read pwd file: %v", err)
	}

	got := strings.TrimSpace(string(data))
	if got != dir {
		t.Errorf("expected working directory %s, got %s", dir, got)
	}
}

func TestRunHooks_CustomDir(t *testing.T) {
	repoDir := t.TempDir()
	customDir := t.TempDir()
	a := newTestAgent(repoDir)

	pwdFile := filepath.Join(repoDir, "pwd.txt")
	hooks := []*config.ResolvedHook{
		{Command: "pwd > " + pwdFile, Timeout: 10 * time.Second, Dir: customDir},
	}

	err := a.runHooks(context.Background(), "post_pull", hooks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("failed to read pwd file: %v", err)
	}

	got := strings.TrimSpace(string(data))
	if got != customDir {
		t.Errorf("expected working directory %s, got %s", customDir, got)
	}
}

func TestRunHooks_RelativeCustomDirResolvesAgainstRepoRoot(t *testing.T) {
	repoDir := t.TempDir()
	customDir := filepath.Join(repoDir, "scripts")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("failed to create custom dir: %v", err)
	}

	pwdFile := filepath.Join(repoDir, "pwd.txt")
	resolved, err := config.Resolve(
		&config.RepoEntry{
			Path: repoDir,
			Hooks: &config.HooksConfig{
				PostPull: []*config.HookEntry{
					{Command: "pwd > " + pwdFile, Timeout: "10s", Dir: "scripts"},
				},
			},
		},
		&config.RepoDefaults{},
		&config.RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	a := &Agent{cfg: resolved}
	err = a.runHooks(context.Background(), "post_pull", resolved.Hooks.PostPull)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("failed to read pwd file: %v", err)
	}

	got := strings.TrimSpace(string(data))
	if got != customDir {
		t.Errorf("expected working directory %s, got %s", customDir, got)
	}
}
