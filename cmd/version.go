package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	versionStr   = "dev"
	commitStr    = "unknown"
	buildDateStr = "unknown"
)

func formatVersion(version, commit, date string) string {
	return fmt.Sprintf("rbackup %s (%s, %s)\n", version, commit, date)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print(formatVersion(versionStr, commitStr, buildDateStr))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
