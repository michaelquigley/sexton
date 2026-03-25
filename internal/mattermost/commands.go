package mattermost

import (
	"fmt"
	"strings"
	"time"
)

// CommandHandler defines the operations available via mattermost commands.
type CommandHandler interface {
	Status(repo string) ([]RepoStatus, error)
	Sync(repo string) error
	Snooze(repo string, duration time.Duration) (time.Time, error)
	Resume(repo string) error
}

// RepoStatus holds status information for a single repo.
type RepoStatus struct {
	Name            string
	Path            string
	State           string
	Branch          string
	LastSync        time.Time
	LastCommit      string
	LastChange      time.Time
	Error           string
	SnoozeRemaining time.Duration
}

// Dispatch parses pre-stripped command text (trigger word or @mentions already
// removed) and dispatches to the appropriate CommandHandler method. Returns the
// formatted response and whether the text was recognized as a command.
func Dispatch(commandText string, handler CommandHandler) (string, bool) {
	tokens := strings.Fields(strings.TrimSpace(commandText))
	if len(tokens) == 0 {
		return FormatHelp(), true
	}

	cmd := strings.ToLower(tokens[0])
	args := tokens[1:]

	switch cmd {
	case "status":
		return dispatchStatus(args, handler), true
	case "sync":
		return dispatchSync(args, handler), true
	case "snooze":
		return dispatchSnooze(args, handler), true
	case "resume":
		return dispatchResume(args, handler), true
	case "help":
		return FormatHelp(), true
	default:
		return fmt.Sprintf("unknown command '%s'\n\n%s", cmd, FormatHelp()), true
	}
}

// StripTriggerWord checks if text starts with a trigger word (case-insensitive,
// word boundary) and returns the remainder. The bool indicates whether a trigger
// word was found.
func StripTriggerWord(text string, triggerWords []string) (string, bool) {
	lower := strings.ToLower(text)
	for _, tw := range triggerWords {
		twLower := strings.ToLower(tw)
		if !strings.HasPrefix(lower, twLower) {
			continue
		}
		rest := text[len(tw):]
		if rest == "" {
			return "", true
		}
		// must be followed by whitespace (word boundary)
		if rest[0] == ' ' || rest[0] == '\t' {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

func dispatchStatus(args []string, handler CommandHandler) string {
	repo := ""
	if len(args) > 0 {
		repo = args[0]
	}
	statuses, err := handler.Status(repo)
	if err != nil {
		return FormatError(err)
	}
	return FormatStatus(statuses)
}

func dispatchSync(args []string, handler CommandHandler) string {
	if len(args) == 0 {
		return "sync requires a repo argument"
	}
	if err := handler.Sync(args[0]); err != nil {
		return FormatError(err)
	}
	return FormatSyncResponse(args[0])
}

func dispatchSnooze(args []string, handler CommandHandler) string {
	if len(args) < 2 {
		return "snooze requires a repo and duration argument"
	}
	d, err := time.ParseDuration(args[1])
	if err != nil {
		return fmt.Sprintf("invalid duration '%s'", args[1])
	}
	until, err := handler.Snooze(args[0], d)
	if err != nil {
		return FormatError(err)
	}
	return FormatSnoozeResponse(args[0], until)
}

func dispatchResume(args []string, handler CommandHandler) string {
	if len(args) == 0 {
		return "resume requires a repo argument"
	}
	if err := handler.Resume(args[0]); err != nil {
		return FormatError(err)
	}
	return FormatResumeResponse(args[0])
}
