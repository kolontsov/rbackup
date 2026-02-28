package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const matrixAgeRecipient = "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"

func runRootCommand(t *testing.T, args ...string) error {
	t.Helper()
	resetRootFlags(t)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	resetRootFlags(t)
	return err
}

func resetRootFlags(t *testing.T) {
	t.Helper()
	seen := map[*pflag.FlagSet]struct{}{}
	resetSet := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		if _, ok := seen[fs]; ok {
			return
		}
		seen[fs] = struct{}{}
		fs.VisitAll(func(f *pflag.Flag) {
			_ = fs.Set(f.Name, f.DefValue)
			f.Changed = false
		})
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		resetSet(c.Flags())
		resetSet(c.PersistentFlags())
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
	cfgFile = ""
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writeCommandConfig(t *testing.T, identityPath, backupsYAML string) string {
	t.Helper()
	workDir := t.TempDir()
	identityLine := ""
	if identityPath != "" {
		identityLine = fmt.Sprintf("  identity_file: %q\n", identityPath)
	}
	if backupsYAML == "" {
		backupsYAML = "backups: {}"
	}
	return writeTestConfig(t, fmt.Sprintf(`
encryption:
  age_recipient: %q
%ssettings:
  namespace: host01
  lock_dir: %q
  spool_dir: %q
remotes:
  - name: r1
    type: s3
    region: us-east-1
    bucket: test-bucket
    access_key_id: test-access-key
    secret_access_key: test-secret-key
defaults:
  remotes: [r1]
%s
`, matrixAgeRecipient, identityLine, filepath.Join(workDir, "locks"), filepath.Join(workDir, "spool"), backupsYAML))
}

func writeNoRemoteConfig(t *testing.T) string {
	t.Helper()
	workDir := t.TempDir()
	return writeTestConfig(t, fmt.Sprintf(`
encryption:
  age_recipient: %q
settings:
  namespace: host01
  lock_dir: %q
  spool_dir: %q
remotes: []
defaults:
  remotes: []
backups: {}
`, matrixAgeRecipient, filepath.Join(workDir, "locks"), filepath.Join(workDir, "spool")))
}

func writeModeFile(t *testing.T, mode os.FileMode, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}

func requireErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err.Error(), want)
	}
}

func withOutput(args []string, outputPath string) []string {
	cloned := make([]string, len(args))
	copy(cloned, args)
	for i := range cloned {
		if cloned[i] == "$OUT" {
			cloned[i] = outputPath
		}
	}
	return cloned
}

func TestBackupCLICommandMatrix(t *testing.T) {
	cfgMissing := writeCommandConfig(t, "", `
backups:
  known:
    object_name: known.db
    cmd: "echo known"
`)
	cfgSealed := writeCommandConfig(t, "", `
backups:
  sealed:
    object_name: sealed.bin
    cmd: "echo sealed"
    seal: true
`)
	cfgEmpty := writeCommandConfig(t, "", "backups: {}")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no_target",
			args:    []string{"backup"},
			wantErr: "exactly one target required",
		},
		{
			name:    "target_must_be_prefixed",
			args:    []string{"backup", "known"},
			wantErr: "exactly one target required",
		},
		{
			name:    "cannot_combine_all_and_entry",
			args:    []string{"backup", "--all", "@known"},
			wantErr: "cannot combine @<name> with --all",
		},
		{
			name:    "missing_entry",
			args:    []string{"backup", "@missing", "--config", cfgMissing},
			wantErr: "backup entry \"missing\" not found",
		},
		{
			name:    "sealed_requires_passphrase",
			args:    []string{"backup", "@sealed", "--config", cfgSealed},
			wantErr: "sealed object requires passphrase",
		},
		{
			name:    "all_mode_empty_config_succeeds",
			args:    []string{"backup", "--all", "--config", cfgEmpty},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runRootCommand(t, tt.args...)
			requireErrContains(t, err, tt.wantErr)
		})
	}
}

func TestConfigCLICommandMatrix(t *testing.T) {
	cfg := writeNoRemoteConfig(t)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "test_requires_mode_flag",
			args:    []string{"config", "test", "--config", cfg},
			wantErr: "at least one mode flag required",
		},
		{
			name:    "test_read_no_remotes",
			args:    []string{"config", "test", "--config", cfg, "--read"},
			wantErr: "",
		},
		{
			name:    "test_write_no_remotes",
			args:    []string{"config", "test", "--config", cfg, "--write", "--write-key", ".rbackup/test/matrix"},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runRootCommand(t, tt.args...)
			requireErrContains(t, err, tt.wantErr)
		})
	}
}

func TestDeriveKeyCLICommandMatrix(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing_org_flag",
			args:    []string{"derive-key"},
			wantErr: "required flag(s) \"org\" not set",
		},
		{
			name:    "write_mode_requires_output",
			args:    []string{"derive-key", "--org", "acme"},
			wantErr: "write mode requires -o",
		},
		{
			name:    "write_mode_requires_output_pq",
			args:    []string{"derive-key", "--org", "acme", "--pq"},
			wantErr: "write mode requires -o",
		},
		{
			name:    "validate_mode_requires_recipient",
			args:    []string{"derive-key", "--org", "acme", "--validate"},
			wantErr: "--validate requires --recipient",
		},
		{
			name:    "validate_mode_disallows_output",
			args:    []string{"derive-key", "--org", "acme", "--validate", "--recipient", "age1example", "-o", "/tmp/identity"},
			wantErr: "--validate does not allow -o",
		},
		{
			name:    "validate_mode_rejects_non_x25519_recipient",
			args:    []string{"derive-key", "--org", "acme", "--validate", "--recipient", "age1pq1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"},
			wantErr: "invalid --recipient",
		},
		{
			name:    "validate_mode_rejects_non_pq_recipient",
			args:    []string{"derive-key", "--org", "acme", "--pq", "--validate", "--recipient", "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"},
			wantErr: "invalid --recipient for --pq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runRootCommand(t, tt.args...)
			requireErrContains(t, err, tt.wantErr)
		})
	}
}

func TestRestoreCLICommandMatrix(t *testing.T) {
	identityPath := writeModeFile(t, 0600, "AGE-SECRET-KEY-1EXAMPLE")
	insecurePassFile := writeModeFile(t, 0644, "seal-secret\n")

	cfgNoIdentity := writeCommandConfig(t, "", "backups: {}")
	cfgWithIdentity := writeCommandConfig(t, identityPath, "backups: {}")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "invalid_key_format",
			args:    []string{"restore", "--remote", "r1", "--key", "/bad", "-o", "$OUT"},
			wantErr: "must not start with '/'",
		},
		{
			name:    "identity_is_required",
			args:    []string{"restore", "--config", cfgNoIdentity, "--remote", "r1", "--key", "host01/test.age", "-o", "$OUT"},
			wantErr: "identity file required",
		},
		{
			name:    "unknown_remote",
			args:    []string{"restore", "--config", cfgWithIdentity, "--remote", "missing", "--key", "host01/test.age", "-o", "$OUT"},
			wantErr: "remote \"missing\" not found",
		},
		{
			name:    "sealed_passphrase_file_must_be_secure",
			args:    []string{"restore", "--config", cfgWithIdentity, "--remote", "r1", "--key", "host01/test.age2", "-o", "$OUT", "--seal-passphrase-file", insecurePassFile},
			wantErr: "insecure permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outPath := filepath.Join(t.TempDir(), "restored.bin")
			err := runRootCommand(t, withOutput(tt.args, outPath)...)
			requireErrContains(t, err, tt.wantErr)
		})
	}
}

func TestVerifyCLICommandMatrix(t *testing.T) {
	identityPath := writeModeFile(t, 0600, "AGE-SECRET-KEY-1EXAMPLE")
	insecurePassFile := writeModeFile(t, 0644, "seal-secret\n")

	cfgNoIdentity := writeCommandConfig(t, "", "backups: {}")
	cfgWithIdentity := writeCommandConfig(t, identityPath, "backups: {}")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "invalid_key_format",
			args:    []string{"verify", "--remote", "r1", "--key", "/bad"},
			wantErr: "must not start with '/'",
		},
		{
			name:    "identity_is_required",
			args:    []string{"verify", "--config", cfgNoIdentity, "--remote", "r1", "--key", "host01/test.age"},
			wantErr: "identity file required",
		},
		{
			name:    "unknown_remote",
			args:    []string{"verify", "--config", cfgWithIdentity, "--remote", "missing", "--key", "host01/test.age"},
			wantErr: "remote \"missing\" not found",
		},
		{
			name:    "sealed_passphrase_file_must_be_secure",
			args:    []string{"verify", "--config", cfgWithIdentity, "--remote", "r1", "--key", "host01/test.age2", "--seal-passphrase-file", insecurePassFile},
			wantErr: "insecure permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runRootCommand(t, tt.args...)
			requireErrContains(t, err, tt.wantErr)
		})
	}
}

func TestRecoveryTxtCLICommandMatrix(t *testing.T) {
	t.Run("template_without_org", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "RECOVERY.txt")
		err := runRootCommand(t, "recovery-txt", "-o", outputPath)
		requireErrContains(t, err, "")

		data, readErr := os.ReadFile(outputPath)
		if readErr != nil {
			t.Fatalf("read recovery file: %v", readErr)
		}
		if !strings.Contains(string(data), "<your-org>") {
			t.Fatalf("template should contain <your-org> placeholder: %q", string(data))
		}
	})

	t.Run("writes_file", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "RECOVERY.txt")
		err := runRootCommand(t, "recovery-txt", "--org", "  Acme-Prod  ", "-o", outputPath)
		requireErrContains(t, err, "")

		data, readErr := os.ReadFile(outputPath)
		if readErr != nil {
			t.Fatalf("read recovery file: %v", readErr)
		}
		if !strings.Contains(string(data), "acme-prod") {
			t.Fatalf("recovery file missing normalized org: %q", string(data))
		}
	})

	t.Run("upload_best_effort_with_no_remotes", func(t *testing.T) {
		cfg := writeNoRemoteConfig(t)
		err := runRootCommand(t, "recovery-txt", "--config", cfg, "--org", "acme-prod", "--upload")
		requireErrContains(t, err, "")
	})

	t.Run("upload_best_effort_with_missing_config", func(t *testing.T) {
		err := runRootCommand(t, "recovery-txt", "--config", "/path/that/does/not/exist.yaml", "--org", "acme-prod", "--upload")
		requireErrContains(t, err, "")
	})
}

func TestRecoveryTextContent(t *testing.T) {
	text := recoveryText("Acme-Prod")
	checks := []struct {
		substr, desc string
	}{
		{"acme-prod", "normalized org"},
		{"If the Identity Was Derived", "derive-key conditional section"},
		{"Argon2id", "Argon2id mention"},
		{"rbackup-key-v1-acme-prod", "salt with org"},
		{"Passphrase: <REQUIRED>", "passphrase input"},
		{"262144 KB", "memory in KB"},
		{"AGE-SECRET-KEY-PQ-1", "pq identity format"},
		{"Backup Signature Verification", "signature section"},
		{"ed25519.Verify", "manual verification recipe"},
	}
	for _, c := range checks {
		if !strings.Contains(text, c.substr) {
			t.Errorf("recovery text should contain %s (%q)", c.desc, c.substr)
		}
	}
}

func TestResolveSealPassphraseInteractiveFromFile(t *testing.T) {
	passFile := writeModeFile(t, 0600, "test-passphrase\n")
	got, err := resolveSealPassphraseInteractive(passFile)
	if err != nil {
		t.Fatalf("resolve passphrase: %v", err)
	}
	if got != "test-passphrase" {
		t.Fatalf("passphrase = %q, want %q", got, "test-passphrase")
	}
}
