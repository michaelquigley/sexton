package mattermost

import (
	"context"

	"github.com/michaelquigley/sexton/internal/agent"
)

// MattermostAlerter posts alert events to a Mattermost channel.
type MattermostAlerter struct {
	client    *Client
	channelID string
}

// NewAlerter creates a new MattermostAlerter that posts to the given channel.
func NewAlerter(client *Client, channelID string) *MattermostAlerter {
	return &MattermostAlerter{client: client, channelID: channelID}
}

func (a *MattermostAlerter) Alert(_ context.Context, event agent.AlertEvent) error {
	text := FormatAlert(event)
	return a.client.PostMessage(a.channelID, text)
}
