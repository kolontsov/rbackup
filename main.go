package main

import (
	"os"

	"github.com/kolontsov/rbackup/cmd"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, buildDate)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
