package main

import (
	"context"
	"fmt"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "snooze <repo> <duration>",
		Short: "pause sync for a duration (e.g. 30m, 1h)",
		Args:  cobra.ExactArgs(2),
		RunE:  runSnooze,
	})
}

func runSnooze(_ *cobra.Command, args []string) error {
	client, conn, err := dialAgentFn()
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := client.Snooze(context.Background(), &sextonv1.SnoozeRequest{
		Repo:     args[0],
		Duration: args[1],
	})
	if err != nil {
		return fmt.Errorf("snooze request failed: %w", err)
	}

	fmt.Printf("snoozed until %s\n", resp.GetExpires())
	return nil
}
