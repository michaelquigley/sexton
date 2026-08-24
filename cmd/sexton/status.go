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
	_, _ = fmt.Fprintln(w, "name\tstate\tbranch\tlast sync\tlast change\tlast commit\tdetail\tpause")
	for _, r := range resp.GetRepos() {
		now := time.Now()
		lastSync := formatRelativeTime(r.GetLastSync(), now)
		lastChange := formatRelativeTime(r.GetLastChange(), now)
		lastCommit := r.GetLastCommit()
		if lastCommit == "" {
			lastCommit = "-"
		}
		detail := r.GetError()
		if detail == "" {
			detail = r.GetAttentionDetail()
		}
		if detail == "" {
			detail = "-"
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
			lastSync, lastChange, lastCommit, detail, pause)
	}
	return w.Flush()
}

func formatRelativeTime(timestamp string, now time.Time) string {
	if timestamp == "" {
		return "-"
	}

	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}

	return format.DurationAgo(now.Sub(t))
}
