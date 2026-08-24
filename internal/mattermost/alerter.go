package mattermost

import (
	"context"
	"errors"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/agent"
)

// Alerter posts alert events to a Mattermost channel, delivering
// attention-severity events as direct messages when dm_users is configured.
// it implements agent.Alerter.
type Alerter struct {
	client       *Client
	channelID    string
	mentionUsers []string
	dmUsers      []string
}

// NewAlerter creates a new Alerter that posts to the given channel and
// direct-messages the given users on attention alerts.
func NewAlerter(client *Client, channelID string, mentionUsers, dmUsers []string) *Alerter {
	return &Alerter{
		client:       client,
		channelID:    channelID,
		mentionUsers: append([]string(nil), mentionUsers...),
		dmUsers:      append([]string(nil), dmUsers...),
	}
}

func (a *Alerter) Alert(_ context.Context, event agent.AlertEvent) error {
	if event.Severity == "attention" && len(a.dmUsers) > 0 {
		dmErr := a.alertDirect(event)
		if dmErr == nil {
			return nil
		}
		// the alert must not be lost: fall back to the channel post with
		// mentions, and keep the direct-message failure visible in the log.
		dl.Warnf("failed to direct-message attention alert for '%s'; falling back to channel: %v", event.RepoName, dmErr)
		if postErr := a.client.PostMessage(a.channelID, FormatAlert(event, a.mentionUsers)); postErr != nil {
			return errors.Join(dmErr, postErr)
		}
		return nil
	}

	return a.client.PostMessage(a.channelID, FormatAlert(event, a.mentionUsers))
}

// alertDirect posts the event to every configured dm user, without mention
// prefixes: a direct message notifies on its own.
func (a *Alerter) alertDirect(event agent.AlertEvent) error {
	text := FormatAlert(event, nil)
	var errs []error
	for _, username := range a.dmUsers {
		channelID, err := a.client.DirectChannelWith(username)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := a.client.PostMessage(channelID, text); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
