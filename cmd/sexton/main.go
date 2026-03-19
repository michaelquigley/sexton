package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/michaelquigley/df/dl"
	"github.com/spf13/cobra"
)

var verbose bool
var rootCmd = &cobra.Command{
	Use:   strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0])),
	Short: "sexton - versioned repository synchronization using git",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if verbose {
			dl.Init(dl.DefaultOptions().SetTrimPrefix("github.com/michaelquigley/").SetLevel(slog.LevelDebug))
		}
	},
}

func init() {
	dl.Init(dl.DefaultOptions().SetTrimPrefix("github.com/michaelquigley/").SetLevel(slog.LevelInfo))
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
	rootCmd.AddCommand(agentCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
