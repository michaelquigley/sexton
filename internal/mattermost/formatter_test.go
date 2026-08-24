package mattermost

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/michaelquigley/push/build"
	"github.com/michaelquigley/sexton/internal/agent"
)

func TestFormatAlertInfo(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "info",
		RepoName: "my-notes",
		Message:  "sync complete (abc123)",
	}
	got := FormatAlert(event, nil)
	if !strings.Contains(got, "**info**") {
		t.Errorf("expected info severity, got %q", got)
	}
	if !strings.Contains(got, "[my-notes]") {
		t.Errorf("expected repo path, got %q", got)
	}
	if !strings.Contains(got, "sync complete (abc123)") {
		t.Errorf("expected message, got %q", got)
	}
}

func TestFormatAlertWithFiles(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "info",
		RepoName: "my-notes",
		Message:  "sync complete (abc123)",
		Files: &agent.AlertFiles{
			Modified: []string{"notes/todo.md", "notes/ideas.md"},
			Added:    []string{"notes/new.md"},
			Deleted:  []string{"notes/old.md"},
		},
	}
	got := FormatAlert(event, nil)
	if !strings.Contains(got, "`notes/todo.md`") {
		t.Errorf("expected modified file, got %q", got)
	}
	if !strings.Contains(got, "- modified:") {
		t.Errorf("expected modified label, got %q", got)
	}
	if !strings.Contains(got, "- added: `notes/new.md`") {
		t.Errorf("expected added file, got %q", got)
	}
	if !strings.Contains(got, "- deleted: `notes/old.md`") {
		t.Errorf("expected deleted file, got %q", got)
	}
}

func TestFormatAlertWithFilesPartial(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "info",
		RepoName: "my-notes",
		Message:  "sync complete (abc123)",
		Files: &agent.AlertFiles{
			Modified: []string{"notes/todo.md"},
		},
	}
	got := FormatAlert(event, nil)
	if !strings.Contains(got, "- modified:") {
		t.Errorf("expected modified label, got %q", got)
	}
	if strings.Contains(got, "- added:") {
		t.Errorf("should not contain added section, got %q", got)
	}
	if strings.Contains(got, "- deleted:") {
		t.Errorf("should not contain deleted section, got %q", got)
	}
}

func TestFormatAlertError(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "error",
		RepoName: "my-notes",
		Message:  "pull failed",
		Error:    errors.New("conflict detected"),
	}
	got := FormatAlert(event, nil)
	if !strings.Contains(got, "**error**") {
		t.Errorf("expected error severity, got %q", got)
	}
	if !strings.Contains(got, "conflict detected") {
		t.Errorf("expected error detail, got %q", got)
	}
}

func TestFormatAlertAttentionMentionsOnlyConfiguredUsers(t *testing.T) {
	event := agent.AlertEvent{
		Severity: "attention",
		RepoName: "repo] **loud** @channel",
		Message:  `"drafts/@all|` + "`note`" + `.md" (pulls paused)`,
	}
	got := FormatAlert(event, []string{"michael", "@alice"})

	if !strings.HasPrefix(got, "@michael @alice **attention**") {
		t.Fatalf("attention alert = %q", got)
	}
	for _, accidental := range []string{"@channel", "@all", "**loud**"} {
		if strings.Contains(got, accidental) {
			t.Fatalf("attention alert contains active untrusted markdown or mention %q: %q", accidental, got)
		}
	}
	if strings.Count(got, "@michael") != 1 || strings.Count(got, "@alice") != 1 {
		t.Fatalf("configured mentions were not the only live mentions: %q", got)
	}
}

func TestFormatAlertNonAttentionIgnoresMentionUsers(t *testing.T) {
	got := FormatAlert(agent.AlertEvent{
		Severity: "error",
		RepoName: "notes",
		Message:  "pull failed",
	}, []string{"michael"})
	if strings.Contains(got, "@michael") {
		t.Fatalf("error alert included attention mention: %q", got)
	}
}

func TestNewAlerterCopiesMentionUsers(t *testing.T) {
	mentions := []string{"michael"}
	a := NewAlerter(nil, "alerts", mentions)
	mentions[0] = "changed"
	if len(a.mentionUsers) != 1 || a.mentionUsers[0] != "michael" {
		t.Fatalf("mention users = %#v", a.mentionUsers)
	}
}

func TestFormatAlertNeutralizesHostileFileNames(t *testing.T) {
	invalid := string([]byte{'b', 'a', 'd', 0xff, '.', 'm', 'd'})
	got := FormatAlert(agent.AlertEvent{
		Severity: "info",
		RepoName: "notes",
		Message:  "sync complete",
		Files: &agent.AlertFiles{Modified: []string{
			"notes/`tick`.md",
			"notes/@channel.md",
			"notes/line\nbreak.md",
			invalid,
		}}}, nil)

	if !utf8.ValidString(got) {
		t.Fatalf("alert is not valid UTF-8: %q", got)
	}
	if strings.Contains(got, "@channel") || strings.Contains(got, "line\nbreak") {
		t.Fatalf("alert retained an active mention or raw newline: %q", got)
	}
	if !strings.Contains(got, `\xff`) {
		t.Fatalf("alert did not preserve invalid byte visibly: %q", got)
	}
}

func TestMattermostCodeSpanContainsBackticksSafely(t *testing.T) {
	if got := mattermostCodeSpan("notes/`tick`.md"); got != "``notes/`tick`.md``" {
		t.Fatalf("code span = %q", got)
	}
}

func TestFormatStatusEmpty(t *testing.T) {
	got := FormatStatus(nil)
	if got != "no repos configured" {
		t.Errorf("expected empty message, got %q", got)
	}
}

func TestFormatStatusTable(t *testing.T) {
	statuses := []RepoStatus{
		{
			Name:     "notes",
			State:    "watching",
			Branch:   "main",
			LastSync: time.Now().Add(-5 * time.Minute),
		},
		{
			Name:            "config",
			State:           "snoozed",
			Branch:          "main",
			SnoozeRemaining: 30 * time.Minute,
		},
		{
			Name:             "backup",
			State:            "holdout",
			Branch:           "main",
			HoldoutRemaining: 45 * time.Minute,
		},
	}
	got := FormatStatus(statuses)
	if !strings.Contains(got, "| notes |") {
		t.Errorf("expected notes row, got %q", got)
	}
	if !strings.Contains(got, "snoozed (30m0s left)") {
		t.Errorf("expected snooze remaining, got %q", got)
	}
	if !strings.Contains(got, "holdout (45m0s left)") {
		t.Errorf("expected holdout remaining, got %q", got)
	}
	if !strings.Contains(got, "5m ago") {
		t.Errorf("expected human-friendly duration, got %q", got)
	}
	if !strings.Contains(got, build.String()) {
		t.Errorf("expected version footer %q, got %q", build.String(), got)
	}
}

func TestFormatStatusUsesDetailPrecedenceAndNeutralizesMarkdown(t *testing.T) {
	hostileDetail := `"drafts/line\nbreak|` + "`note`" + `@all\xff.md"`
	statuses := []RepoStatus{
		{
			Name:            "attention|repo @channel",
			State:           "attention",
			Branch:          "main|next",
			AttentionDetail: hostileDetail,
		},
		{
			Name:            "paused",
			State:           "snoozed",
			Branch:          "main",
			Error:           "push failed",
			AttentionDetail: "standing attention",
			SnoozeRemaining: time.Minute,
		},
	}

	got := FormatStatus(statuses)
	if !strings.Contains(got, "| detail |") || strings.Contains(got, "| error |") {
		t.Fatalf("status header = %q", got)
	}
	for _, unsafe := range []string{"@channel", "@all", "main|next"} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("status retained unsafe value %q: %q", unsafe, got)
		}
	}
	if !strings.Contains(got, "push failed") || strings.Contains(got, "standing attention") {
		t.Fatalf("error did not outrank retained attention: %q", got)
	}
	if strings.Contains(got, "line\nbreak") || !strings.Contains(got, `\xff`) {
		t.Fatalf("hostile detail was not rendered visibly: %q", got)
	}
	if !strings.Contains(got, "snoozed (1m0s left)") {
		t.Fatalf("paused state missing: %q", got)
	}
}

func TestFormatAlertWithCommitMessage(t *testing.T) {
	event := agent.AlertEvent{
		Severity:      "info",
		RepoName:      "my-notes",
		Message:       "sync complete (abc123)",
		CommitMessage: "add pane design spec and update project index",
	}
	got := FormatAlert(event, nil)
	if !strings.Contains(got, "> add pane design spec and update project index") {
		t.Errorf("expected commit message in blockquote, got %q", got)
	}
}

func TestFormatSyncResponse(t *testing.T) {
	got := FormatSyncResponse("my-notes")
	if got != "sync triggered for 'my-notes'" {
		t.Errorf("got %q", got)
	}
}

func TestFormatSnoozeResponse(t *testing.T) {
	until := time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)
	got := FormatSnoozeResponse("my-notes", until)
	if !strings.Contains(got, "snoozed 'my-notes'") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "2026-03-25T14:00:00Z") {
		t.Errorf("expected RFC3339 time, got %q", got)
	}
}

func TestFormatResumeResponse(t *testing.T) {
	got := FormatResumeResponse("resumed", "my-notes")
	if got != "resumed 'my-notes'" {
		t.Errorf("got %q", got)
	}
}

func TestFormatResumeResponsePassesThroughCustomMessage(t *testing.T) {
	got := FormatResumeResponse("holdout remains active until 2026-04-03T11:00:00-04:00", "my-notes")
	if !strings.Contains(got, "holdout remains active until") {
		t.Errorf("got %q", got)
	}
}

func TestFormatError(t *testing.T) {
	got := FormatError(errors.New("something broke"))
	if got != "error: something broke" {
		t.Errorf("got %q", got)
	}
}

func TestFormatHelp(t *testing.T) {
	got := FormatHelp()
	if !strings.Contains(got, "status") {
		t.Errorf("expected status command in help, got %q", got)
	}
	if !strings.Contains(got, "sync") {
		t.Errorf("expected sync command in help, got %q", got)
	}
}
