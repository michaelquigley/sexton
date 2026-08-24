package mattermost

import (
	"context"

	"github.com/michaelquigley/sexton/internal/agent"
)

// Alerter posts alert events to a Mattermost channel. it implements
// agent.Alerter.
type Alerter struct {
	client       *Client
	channelID    string
	mentionUsers []string
}

// NewAlerter creates a new Alerter that posts to the given channel.
func NewAlerter(client *Client, channelID string, mentionUsers []string) *Alerter {
	return &Alerter{client: client, channelID: channelID, mentionUsers: append([]string(nil), mentionUsers...)}
}

func (a *Alerter) Alert(_ context.Context, event agent.AlertEvent) error {
	text := FormatAlert(event, a.mentionUsers)
	return a.client.PostMessage(a.channelID, text)
}
