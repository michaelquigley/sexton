package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/config"
)

type failingAlerter struct {
	calls int
}

func (a *failingAlerter) Alert(context.Context, AlertEvent) error {
	a.calls++
	return errors.New("mattermost unavailable")
}

func TestAlertDeliveryFailureIsLogged(t *testing.T) {
	var output bytes.Buffer
	dl.Init(dl.DefaultOptions().JSON().SetOutput(&output))
	defer dl.Init(dl.DefaultOptions())

	sink := &failingAlerter{}
	a := newAgentForTest(&stubGit{}, sink)
	a.alert("attention", "local changes need attention", nil)
	a.alertWithFiles("info", "sync complete", modifiedStatus("notes.md"), "sexton: update 1 file")

	if sink.calls != 2 {
		t.Fatalf("sink calls = %d, want 2", sink.calls)
	}
	logged := output.String()
	for _, want := range []string{"test-repo", "local changes need attention", "sync complete", "mattermost unavailable"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("delivery failure log does not contain %q: %s", want, logged)
		}
	}
}

func TestLogAlerterRendersAttentionAsWarning(t *testing.T) {
	var output bytes.Buffer
	dl.Init(dl.DefaultOptions().JSON().SetOutput(&output))
	defer dl.Init(dl.DefaultOptions())

	if err := (&LogAlerter{}).Alert(context.Background(), AlertEvent{
		Severity: "attention",
		RepoName: "notes",
		Message:  "local changes need attention",
	}); err != nil {
		t.Fatalf("Alert() error = %v", err)
	}
	logged := output.String()
	if !strings.Contains(logged, `"level":"WARN"`) || !strings.Contains(logged, "local changes need attention") {
		t.Fatalf("attention log = %s", logged)
	}
}

func TestStartWarnsOnceForDefaultedCommitPolicy(t *testing.T) {
	alerts := &recordingAlerter{}
	a := newAgentForTest(&stubGit{}, alerts)
	a.cfg.CommitPolicy = config.PolicyNone
	a.cfg.PolicyDefaulted = true

	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	stopAgentAndWait(t, a)

	warnings := messagesWithSeverity(alerts.events, "warning")
	if len(warnings) != 1 || warnings[0] != "no commit_policy configured; defaulting to 'none'" {
		t.Fatalf("startup warnings = %#v", warnings)
	}
}

func TestStartMalformedRepoLocalWarningOutranksDefaultedWarning(t *testing.T) {
	alerts := &recordingAlerter{}
	a := newAgentForTest(&stubGit{}, alerts)
	a.cfg.CommitPolicy = config.PolicyNone
	a.cfg.PolicyDefaulted = true
	a.cfg.LocalConfigError = errors.New("line 2: malformed yaml")

	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	stopAgentAndWait(t, a)

	warnings := messagesWithSeverity(alerts.events, "warning")
	if len(warnings) != 1 {
		t.Fatalf("startup warnings = %#v", warnings)
	}
	if want := "repo-local config malformed (line 2: malformed yaml); commit policy forced to 'none'"; warnings[0] != want {
		t.Fatalf("startup warning = %q, want %q", warnings[0], want)
	}
}
