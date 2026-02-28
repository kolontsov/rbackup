package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestConfigValidateHappyPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: test
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`), 0644)

	cfgFile = cfgPath
	defer func() { cfgFile = "" }()

	rootCmd.SetArgs([]string{"config", "validate"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected success: %v", err)
	}
}

func TestConfigValidateInvalid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
encryption: {}
`), 0644)

	cfgFile = cfgPath
	defer func() { cfgFile = "" }()

	rootCmd.SetArgs([]string{"config", "validate"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestConfigValidateHybridRecipient(t *testing.T) {
	hybridID, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatalf("generate hybrid identity: %v", err)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`
encryption:
  age_recipient: %q
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: test
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`, hybridID.Recipient().String())
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfgFile = cfgPath
	defer func() { cfgFile = "" }()

	rootCmd.SetArgs([]string{"config", "validate"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected success for PQ recipient: %v", err)
	}
}

func TestConfigTestRequiresFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: test
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`), 0644)

	cfgFile = cfgPath
	defer func() { cfgFile = "" }()

	rootCmd.SetArgs([]string{"config", "test"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error without --read or --write")
	}
}

func TestRuntimeDirWarning(t *testing.T) {
	if msg := runtimeDirWarning("settings.lock_dir", ""); !strings.Contains(msg, "is empty") {
		t.Fatalf("expected empty warning, got %q", msg)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if msg := runtimeDirWarning("settings.spool_dir", missing); !strings.Contains(msg, "does not exist") {
		t.Fatalf("expected missing-dir warning, got %q", msg)
	}

	filePath := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if msg := runtimeDirWarning("settings.spool_dir", filePath); !strings.Contains(msg, "not a directory") {
		t.Fatalf("expected not-a-directory warning, got %q", msg)
	}

	dirPath := t.TempDir()
	if msg := runtimeDirWarning("settings.spool_dir", dirPath); msg != "" {
		t.Fatalf("expected no warning for existing dir, got %q", msg)
	}
}
