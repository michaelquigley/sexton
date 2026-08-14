package mattermost

import (
	"context"

	"github.com/michaelquigley/sexton/internal/agent"
)

// Alerter posts alert events to a Mattermost channel. it implements
// agent.Alerter.
type Alerter struct {
	client    *Client
	channelID string
}

// NewAlerter creates a new Alerter that posts to the given channel.
func NewAlerter(client *Client, channelID string) *Alerter {
	return &Alerter{client: client, channelID: channelID}
}

func (a *Alerter) Alert(_ context.Context, event agent.AlertEvent) error {
	text := FormatAlert(event)
	return a.client.PostMessage(a.channelID, text)
}
