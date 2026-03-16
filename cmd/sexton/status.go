package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "show agent status",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("not yet implemented")
			return nil
		},
	})
}
