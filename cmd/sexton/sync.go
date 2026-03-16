package main

import (
	"context"
	"fmt"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "sync <repo>",
		Short: "trigger an immediate sync cycle",
		Args:  cobra.ExactArgs(1),
		RunE:  runSync,
	})
}

func runSync(_ *cobra.Command, args []string) error {
	client, conn, err := dialAgent()
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer conn.Close()

	resp, err := client.Sync(context.Background(), &sextonv1.SyncRequest{Repo: args[0]})
	if err != nil {
		return fmt.Errorf("sync request failed: %w", err)
	}

	fmt.Println(resp.GetMessage())
	return nil
}
