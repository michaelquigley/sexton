package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status [repo]",
		Short: "show agent status",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runStatus,
	})
}

func runStatus(_ *cobra.Command, args []string) error {
	client, conn, err := dialAgent()
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer conn.Close()

	req := &sextonv1.StatusRequest{}
	if len(args) > 0 {
		req.Repo = args[0]
	}

	resp, err := client.Status(context.Background(), req)
	if err != nil {
		return fmt.Errorf("status request failed: %w", err)
	}

	if len(resp.GetRepos()) == 0 {
		fmt.Println("no repos")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tBRANCH\tLAST SYNC\tLAST COMMIT\tERROR\tSNOOZE")
	for _, r := range resp.GetRepos() {
		lastSync := r.GetLastSync()
		if lastSync == "" {
			lastSync = "-"
		}
		lastCommit := r.GetLastCommit()
		if lastCommit == "" {
			lastCommit = "-"
		}
		errStr := r.GetError()
		if errStr == "" {
			errStr = "-"
		}
		snooze := r.GetSnoozeRemaining()
		if snooze == "" {
			snooze = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.GetName(), r.GetState(), r.GetBranch(),
			lastSync, lastCommit, errStr, snooze)
	}
	return w.Flush()
}
