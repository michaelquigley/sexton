package mattermost

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/michaelquigley/push/build"
	"github.com/michaelquigley/sexton/internal/agent"
	"github.com/michaelquigley/sexton/internal/format"
)

// FormatAlert formats an alert event as mattermost markdown.
func FormatAlert(event agent.AlertEvent, mentionUsers []string) string {
	var b strings.Builder
	switch event.Severity {
	case "error":
		b.WriteString("**error**")
	case "warning":
		b.WriteString("**warning**")
	case "attention":
		formatConfiguredMentions(&b, mentionUsers)
		b.WriteString("**attention**")
	default:
		b.WriteString("**info**")
	}
	fmt.Fprintf(&b, " [%s] %s", neutralMattermostText(event.RepoName), neutralMattermostText(event.Message))
	if event.Error != nil {
		fmt.Fprintf(&b, ": %s", neutralMattermostText(event.Error.Error()))
	}
	if event.CommitMessage != "" {
		fmt.Fprintf(&b, "\n> %s", neutralMattermostText(event.CommitMessage))
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
		b.WriteString(mattermostCodeSpan(f))
	}
}

func formatConfiguredMentions(b *strings.Builder, mentionUsers []string) {
	for _, username := range mentionUsers {
		username = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(username), "@"))
		if username == "" {
			continue
		}
		fmt.Fprintf(b, "@%s ", username)
	}
}

func mattermostCodeSpan(value string) string {
	visible := strings.ReplaceAll(visibleMattermostText(value), "@", "@\u200b")
	longestRun := 0
	for _, part := range strings.FieldsFunc(visible, func(r rune) bool { return r != '`' }) {
		if len(part) > longestRun {
			longestRun = len(part)
		}
	}
	delimiter := strings.Repeat("`", longestRun+1)
	return delimiter + visible + delimiter
}

func neutralMattermostText(value string) string {
	visible := visibleMattermostText(value)
	var b strings.Builder
	for _, r := range visible {
		switch r {
		case '@':
			b.WriteString("@\u200b")
		case '\\', '`', '*', '_', '[', ']', '|', '~':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func visibleMattermostText(value string) string {
	var b strings.Builder
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, "\\x%02x", value[0])
			value = value[1:]
			continue
		}
		value = value[size:]
		switch r {
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&b, "\\u%04x", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// FormatStatus formats a list of repo statuses as a markdown table.
func FormatStatus(statuses []RepoStatus) string {
	if len(statuses) == 0 {
		return "no repos configured"
	}
	var b strings.Builder
	b.WriteString("| repo | state | branch | last sync | last change | detail |\n")
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
		switch {
		case s.HoldoutRemaining > 0:
			state = fmt.Sprintf("holdout (%s left)", s.HoldoutRemaining.Truncate(time.Second))
		case s.SnoozeRemaining > 0:
			state = fmt.Sprintf("snoozed (%s left)", s.SnoozeRemaining.Truncate(time.Second))
		}
		detail := ""
		if s.Error != "" {
			detail = s.Error
		} else if s.AttentionDetail != "" {
			detail = s.AttentionDetail
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			neutralMattermostText(s.Name), neutralMattermostText(state), neutralMattermostText(s.Branch),
			lastSync, lastChange, neutralMattermostText(detail))
	}
	fmt.Fprintf(&b, "\n_sexton %s_", build.String())
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
func FormatResumeResponse(message, repo string) string {
	if message == "" || message == "resumed" {
		return fmt.Sprintf("resumed '%s'", repo)
	}
	return message
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
