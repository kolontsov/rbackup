package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var cfgFile string

const (
	canonicalRepoURL = "https://github.com/kolontsov/rbackup"
)

var rootCmd = &cobra.Command{
	Use:   "rbackup",
	Short: "Encrypted offsite backups to S3-compatible remotes",
	Long: "Encrypted offsite backups to S3-compatible remotes.\n\n" +
		"Docs & Updates: " + canonicalRepoURL,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: $RBACKUP_CONFIG or /etc/rbackup/config.yaml)")
}

func resolveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	if env := os.Getenv("RBACKUP_CONFIG"); env != "" {
		return env
	}
	return "/etc/rbackup/config.yaml"
}

func Execute() error {
	return rootCmd.Execute()
}

func SetVersionInfo(v, c, d string) {
	versionStr = v
	commitStr = c
	buildDateStr = d
}
