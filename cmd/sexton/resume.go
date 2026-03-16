package main

import (
	"context"
	"fmt"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "resume <repo>",
		Short: "resume a snoozed or halted repo",
		Args:  cobra.ExactArgs(1),
		RunE:  runResume,
	})
}

func runResume(_ *cobra.Command, args []string) error {
	client, conn, err := dialAgent()
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer conn.Close()

	resp, err := client.Resume(context.Background(), &sextonv1.ResumeRequest{Repo: args[0]})
	if err != nil {
		return fmt.Errorf("resume request failed: %w", err)
	}

	fmt.Println(resp.GetMessage())
	return nil
}
