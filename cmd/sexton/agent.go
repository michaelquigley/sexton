package main

import (
	"github.com/michaelquigley/df/da"
	"github.com/michaelquigley/sexton/internal/agent"
	"github.com/michaelquigley/sexton/internal/config"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent <config>",
	Short: "run the sync agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgent,
}

func runAgent(_ *cobra.Command, args []string) error {
	cfg, err := config.Load(args[0])
	if err != nil {
		return err
	}

	c, err := agent.NewContainer(cfg)
	if err != nil {
		return err
	}

	return da.Run(c)
}
