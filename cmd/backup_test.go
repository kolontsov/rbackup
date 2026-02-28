package cmd

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kolontsov/rbackup/internal/config"
)

func TestAllModeRemoteRefsSkipsManualOnly(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Remotes: []string{"default-a", "default-b"},
		},
		Backups: map[string]config.BackupEntry{
			"auto-defaults": {
				ObjectName: "auto-defaults.db",
				Cmd:        "echo auto-defaults",
			},
			"auto-custom": {
				ObjectName: "auto-custom.db",
				Cmd:        "echo auto-custom",
				Remotes:    []string{"custom-x", "default-a"},
			},
			"manual-only": {
				ObjectName: "manual.db",
				File:       "/tmp/manual.db",
				ManualOnly: true,
				Remotes:    []string{"manual-remote"},
			},
		},
	}

	got := allModeRemoteRefs(cfg)
	want := []string{"custom-x", "default-a", "default-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allModeRemoteRefs() = %v, want %v", got, want)
	}
}

func TestAllModeRemoteRefsOnlyManualEntries(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Remotes: []string{"default-a"},
		},
		Backups: map[string]config.BackupEntry{
			"manual-only": {
				ObjectName: "manual.db",
				File:       "/tmp/manual.db",
				ManualOnly: true,
				Remotes:    []string{"manual-remote"},
			},
		},
	}

	got := allModeRemoteRefs(cfg)
	if len(got) != 0 {
		t.Fatalf("allModeRemoteRefs() = %v, want empty", got)
	}
}

func TestBackupAllNoTargetsNotice(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  lock_dir: `+filepath.Join(dir, "locks")+`
  spool_dir: `+filepath.Join(dir, "spool")+`
backups: {}
`), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfgFile = cfgPath
	defer func() { cfgFile = "" }()

	oldStderr := os.Stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stderr pipe: %v", err)
	}
	os.Stderr = stderrW
	defer func() {
		_ = stderrW.Close()
		os.Stderr = oldStderr
	}()

	rootCmd.SetArgs([]string{"backup", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup --all should succeed for empty targets: %v", err)
	}

	_ = stderrW.Close()
	stderrBytes, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatalf("reading stderr: %v", err)
	}
	if !strings.Contains(string(stderrBytes), "No work done: no backup targets configured") {
		t.Fatalf("expected no-target notice, got stderr: %q", string(stderrBytes))
	}
}

func TestBackupAllReceiptWriteFailure(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  lock_dir: `+filepath.Join(dir, "locks")+`
  spool_dir: `+filepath.Join(dir, "spool")+`
backups: {}
`), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfgFile = cfgPath
	defer func() { cfgFile = "" }()

	oldStdout := os.Stdout
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	_ = stdoutR.Close()
	_ = stdoutW.Close()
	os.Stdout = stdoutW
	defer func() {
		os.Stdout = oldStdout
	}()

	rootCmd.SetArgs([]string{"backup", "--all"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when writing receipts fails")
	}
	if !strings.Contains(err.Error(), "writing receipts") {
		t.Fatalf("expected writing receipts error, got: %v", err)
	}
}
