package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show Clampany version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Clampany v0.2")
	},
} 