package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRootHelpIncludesDocsAndUpdates(t *testing.T) {
	resetRootFlags(t)
	defer resetRootFlags(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	defer func() {
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
	}()

	rootCmd.SetArgs([]string{"--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	got := out.String()
	want := "Docs & Updates: " + canonicalRepoURL
	if !strings.Contains(got, want) {
		t.Fatalf("help output missing %q\noutput:\n%s", want, got)
	}
}
