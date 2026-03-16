package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/michaelquigley/df/dl"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0])),
	Short: "sexton - versioned repository synchronization using git",
}

func init() {
	dl.Init(dl.DefaultOptions().SetTrimPrefix("github.com/michaelquigley/"))
	rootCmd.AddCommand(agentCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
