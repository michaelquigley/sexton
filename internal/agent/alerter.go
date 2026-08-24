package agent

import (
	"context"
	"errors"
	"time"

	"github.com/michaelquigley/df/dl"
)

type AlertFiles struct {
	Modified []string
	Added    []string
	Deleted  []string
}

type AlertEvent struct {
	Severity      string
	RepoName      string
	Message       string
	Error         error
	Timestamp     time.Time
	Files         *AlertFiles
	CommitMessage string
}

type Alerter interface {
	Alert(ctx context.Context, event AlertEvent) error
}

type LogAlerter struct{}

func (a *LogAlerter) Alert(_ context.Context, event AlertEvent) error {
	switch event.Severity {
	case "error":
		if event.Error != nil {
			dl.Errorf("[%s] %s: %v", event.RepoName, event.Message, event.Error)
		} else {
			dl.Errorf("[%s] %s", event.RepoName, event.Message)
		}
	case "warning", "attention":
		dl.Warnf("[%s] %s", event.RepoName, event.Message)
	default:
		if event.Files != nil {
			dl.Infof("[%s] %s (%d modified, %d added, %d deleted)",
				event.RepoName, event.Message,
				len(event.Files.Modified), len(event.Files.Added), len(event.Files.Deleted))
		} else {
			dl.Infof("[%s] %s", event.RepoName, event.Message)
		}
		if event.CommitMessage != "" {
			dl.Infof("[%s] commit message: '%s'", event.RepoName, event.CommitMessage)
		}
	}
	return nil
}

// MultiAlerter composes multiple alerters, calling all of them for each event.
type MultiAlerter struct {
	Alerters []Alerter
}

func (m *MultiAlerter) Alert(ctx context.Context, event AlertEvent) error {
	var errs []error
	for _, a := range m.Alerters {
		if err := a.Alert(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
