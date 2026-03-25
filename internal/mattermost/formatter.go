package mattermost

import (
	"fmt"
	"strings"
	"time"

	"github.com/michaelquigley/sexton/internal/agent"
	"github.com/michaelquigley/sexton/internal/format"
)

// FormatAlert formats an alert event as mattermost markdown.
func FormatAlert(event agent.AlertEvent) string {
	var b strings.Builder
	switch event.Severity {
	case "error":
		b.WriteString("**error**")
	case "warning":
		b.WriteString("**warning**")
	default:
		b.WriteString("**info**")
	}
	fmt.Fprintf(&b, " [%s] %s", event.RepoPath, event.Message)
	if event.Error != nil {
		fmt.Fprintf(&b, ": %v", event.Error)
	}
	if event.CommitMessage != "" {
		fmt.Fprintf(&b, "\n> %s", event.CommitMessage)
	}
	if event.Files != nil {
		formatFileList(&b, "modified", event.Files.Modified)
		formatFileList(&b, "added", event.Files.Added)
		formatFileList(&b, "deleted", event.Files.Deleted)
	}
	return b.String()
}

func formatFileList(b *strings.Builder, label string, files []string) {
	if len(files) == 0 {
		return
	}
	b.WriteString("\n- ")
	b.WriteString(label)
	b.WriteString(": ")
	for i, f := range files {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "`%s`", f)
	}
}

// FormatStatus formats a list of repo statuses as a markdown table.
func FormatStatus(statuses []RepoStatus) string {
	if len(statuses) == 0 {
		return "no repos configured"
	}
	var b strings.Builder
	b.WriteString("| repo | state | branch | last sync | last change | error |\n")
	b.WriteString("|------|-------|--------|-----------|-------------|-------|\n")
	for _, s := range statuses {
		lastSync := ""
		if !s.LastSync.IsZero() {
			lastSync = format.TimeAgo(s.LastSync)
		}
		lastChange := ""
		if !s.LastChange.IsZero() {
			lastChange = format.TimeAgo(s.LastChange)
		}
		state := s.State
		if s.SnoozeRemaining > 0 {
			state = fmt.Sprintf("snoozed (%s left)", s.SnoozeRemaining.Truncate(time.Second))
		}
		errStr := ""
		if s.Error != "" {
			errStr = s.Error
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			s.Name, state, s.Branch, lastSync, lastChange, errStr)
	}
	return b.String()
}

// FormatSyncResponse formats a sync trigger confirmation.
func FormatSyncResponse(repo string) string {
	return fmt.Sprintf("sync triggered for '%s'", repo)
}

// FormatSnoozeResponse formats a snooze confirmation.
func FormatSnoozeResponse(repo string, until time.Time) string {
	return fmt.Sprintf("snoozed '%s' until %s", repo, until.Format(time.RFC3339))
}

// FormatResumeResponse formats a resume confirmation.
func FormatResumeResponse(repo string) string {
	return fmt.Sprintf("resumed '%s'", repo)
}

// FormatError formats an error response.
func FormatError(err error) string {
	return fmt.Sprintf("error: %v", err)
}

// FormatHelp returns the list of available commands.
func FormatHelp() string {
	return `available commands:
- **status** [repo] -- show repo status (all repos if omitted)
- **sync** <repo> -- trigger an immediate sync
- **snooze** <repo> <duration> -- pause sync (e.g. 30m, 2h, 1h30m)
- **resume** <repo> -- resume a snoozed or errored repo
- **help** -- show this message`
}
