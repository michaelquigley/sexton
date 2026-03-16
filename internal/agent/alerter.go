package agent

import (
	"context"
	"time"

	"github.com/michaelquigley/df/dl"
)

type AlertEvent struct {
	Severity  string
	RepoPath  string
	Message   string
	Error     error
	Timestamp time.Time
}

type Alerter interface {
	Alert(ctx context.Context, event AlertEvent) error
}

type LogAlerter struct{}

func (a *LogAlerter) Alert(_ context.Context, event AlertEvent) error {
	switch event.Severity {
	case "error":
		if event.Error != nil {
			dl.Errorf("[%s] %s: %v", event.RepoPath, event.Message, event.Error)
		} else {
			dl.Errorf("[%s] %s", event.RepoPath, event.Message)
		}
	case "warning":
		dl.Warnf("[%s] %s", event.RepoPath, event.Message)
	default:
		dl.Infof("[%s] %s", event.RepoPath, event.Message)
	}
	return nil
}
