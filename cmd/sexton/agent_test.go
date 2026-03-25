package main

import (
	"strings"
	"testing"

	"github.com/michaelquigley/sexton/internal/agent"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/mattermost"
)

func TestBuildAlerterReusesMattermostClientForSharedIngress(t *testing.T) {
	restore := stubMattermostLifecycle(t)
	defer restore()

	var startCount int
	var stopCount int
	startMattermostClient = func(c *mattermost.Client, handler mattermost.CommandHandler) error {
		startCount++
		return nil
	}
	stopMattermostClient = func(c *mattermost.Client) {
		stopCount++
	}

	alerts := []*config.AlertConfig{
		{
			Type: "mattermost",
			Mattermost: &config.MattermostConfig{
				URL:          "https://mm.local",
				TokenEnv:     "MM_TOKEN",
				ChannelID:    "chan-1",
				TriggerWords: []string{"sexton", "bot"},
				AllowedUsers: []string{"Michael", "alice"},
			},
		},
		{
			Type: "mattermost",
			Mattermost: &config.MattermostConfig{
				URL:          "https://mm.local",
				TokenEnv:     "MM_TOKEN",
				ChannelID:    "chan-2",
				TriggerWords: []string{"BOT", "sexton"},
				AllowedUsers: []string{"alice", "michael"},
			},
		},
	}

	alerter, cleanup, err := buildAlerter(alerts, &containerAdapter{c: &agent.Container{}})
	if err != nil {
		t.Fatalf("buildAlerter() error = %v", err)
	}
	if cleanup == nil {
		t.Fatal("buildAlerter() cleanup = nil, want non-nil")
	}
	if startCount != 1 {
		t.Fatalf("mattermost client start count = %d, want 1", startCount)
	}
	if _, ok := alerter.(*agent.MultiAlerter); !ok {
		t.Fatalf("buildAlerter() = %T, want *agent.MultiAlerter", alerter)
	}

	cleanup()
	if stopCount != 1 {
		t.Fatalf("mattermost client stop count = %d, want 1", stopCount)
	}
}

func TestBuildAlerterRejectsConflictingMattermostIngress(t *testing.T) {
	restore := stubMattermostLifecycle(t)
	defer restore()

	var startCount int
	var stopCount int
	startMattermostClient = func(c *mattermost.Client, handler mattermost.CommandHandler) error {
		startCount++
		return nil
	}
	stopMattermostClient = func(c *mattermost.Client) {
		stopCount++
	}

	alerts := []*config.AlertConfig{
		{
			Type: "mattermost",
			Mattermost: &config.MattermostConfig{
				URL:          "https://mm.local",
				Token:        "secret",
				ChannelID:    "chan-1",
				TriggerWords: []string{"sexton"},
				AllowedUsers: []string{"michael"},
			},
		},
		{
			Type: "mattermost",
			Mattermost: &config.MattermostConfig{
				URL:          "https://mm.local",
				Token:        "secret",
				ChannelID:    "chan-2",
				TriggerWords: []string{"other"},
				AllowedUsers: []string{"michael"},
			},
		},
	}

	_, cleanup, err := buildAlerter(alerts, &containerAdapter{c: &agent.Container{}})
	if err == nil {
		t.Fatal("buildAlerter() error = nil, want non-nil")
	}
	if cleanup != nil {
		t.Fatal("buildAlerter() cleanup != nil on error")
	}
	if !strings.Contains(err.Error(), "identical allowed_users and trigger_words") {
		t.Fatalf("buildAlerter() error = %q, want conflict message", err)
	}
	if startCount != 1 {
		t.Fatalf("mattermost client start count = %d, want 1", startCount)
	}
	if stopCount != 1 {
		t.Fatalf("mattermost client stop count = %d, want 1", stopCount)
	}
}

func TestBuildAlerterRejectsMattermostWithoutChannelID(t *testing.T) {
	restore := stubMattermostLifecycle(t)
	defer restore()

	var startCount int
	startMattermostClient = func(c *mattermost.Client, handler mattermost.CommandHandler) error {
		startCount++
		return nil
	}

	alerts := []*config.AlertConfig{
		{
			Type: "mattermost",
			Mattermost: &config.MattermostConfig{
				URL:       "https://mm.local",
				TokenEnv:  "MM_TOKEN",
				ChannelID: "   ",
			},
		},
	}

	_, cleanup, err := buildAlerter(alerts, &containerAdapter{c: &agent.Container{}})
	if err == nil {
		t.Fatal("buildAlerter() error = nil, want non-nil")
	}
	if cleanup != nil {
		t.Fatal("buildAlerter() cleanup != nil on error")
	}
	if !strings.Contains(err.Error(), "requires a non-empty channel_id") {
		t.Fatalf("buildAlerter() error = %q, want channel_id validation", err)
	}
	if startCount != 0 {
		t.Fatalf("mattermost client start count = %d, want 0", startCount)
	}
}

func TestBuildAlerterStartsDistinctMattermostClientsForDistinctIdentity(t *testing.T) {
	restore := stubMattermostLifecycle(t)
	defer restore()

	var startCount int
	var stopCount int
	startMattermostClient = func(c *mattermost.Client, handler mattermost.CommandHandler) error {
		startCount++
		return nil
	}
	stopMattermostClient = func(c *mattermost.Client) {
		stopCount++
	}

	alerts := []*config.AlertConfig{
		{
			Type: "mattermost",
			Mattermost: &config.MattermostConfig{
				URL:          "https://mm-one.local",
				TokenEnv:     "MM_ONE_TOKEN",
				ChannelID:    "chan-1",
				TriggerWords: []string{"sexton"},
			},
		},
		{
			Type: "mattermost",
			Mattermost: &config.MattermostConfig{
				URL:          "https://mm-two.local",
				TokenEnv:     "MM_TWO_TOKEN",
				ChannelID:    "chan-2",
				TriggerWords: []string{"sexton"},
			},
		},
	}

	_, cleanup, err := buildAlerter(alerts, &containerAdapter{c: &agent.Container{}})
	if err != nil {
		t.Fatalf("buildAlerter() error = %v", err)
	}
	if startCount != 2 {
		t.Fatalf("mattermost client start count = %d, want 2", startCount)
	}

	cleanup()
	if stopCount != 2 {
		t.Fatalf("mattermost client stop count = %d, want 2", stopCount)
	}
}

func stubMattermostLifecycle(t *testing.T) func() {
	t.Helper()

	prevStart := startMattermostClient
	prevStop := stopMattermostClient

	return func() {
		startMattermostClient = prevStart
		stopMattermostClient = prevStop
	}
}
