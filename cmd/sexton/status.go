package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"github.com/michaelquigley/sexton/internal/format"
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
	client, conn, err := dialAgentFn()
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer func() { _ = conn.Close() }()

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
	_, _ = fmt.Fprintln(w, "NAME\tSTATE\tBRANCH\tLAST SYNC\tLAST CHANGE\tLAST COMMIT\tERROR\tPAUSE")
	for _, r := range resp.GetRepos() {
		now := time.Now()
		lastSync := formatLastSync(r.GetLastSync(), now)
		lastChange := formatLastSync(r.GetLastChange(), now)
		lastCommit := r.GetLastCommit()
		if lastCommit == "" {
			lastCommit = "-"
		}
		errStr := r.GetError()
		if errStr == "" {
			errStr = "-"
		}
		pause := r.GetHoldoutRemaining()
		if pause == "" {
			pause = r.GetSnoozeRemaining()
		}
		if pause == "" {
			pause = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.GetName(), r.GetState(), r.GetBranch(),
			lastSync, lastChange, lastCommit, errStr, pause)
	}
	return w.Flush()
}

func formatLastSync(lastSync string, now time.Time) string {
	if lastSync == "" {
		return "-"
	}

	t, err := time.Parse(time.RFC3339, lastSync)
	if err != nil {
		return lastSync
	}

	return format.DurationAgo(now.Sub(t))
}
