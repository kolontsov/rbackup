package config

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

const validConfig = `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"

settings:
  namespace: host01
  cmd_timeout: 1h
  remote_timeout: 30m
  remote_retries: 4
  lock_wait: 0s
  lock_dir: /tmp/rbackup
  spool_dir: /var/tmp/rbackup
  max_backup_bytes: 53687091200

remotes:
  - name: s3-primary
    type: s3
    region: eu-central-1
    bucket: spnet-backup
    aws_profile: backup-prod

  - name: b2-secondary
    type: s3
    bucket: spnet-backup
    endpoint: "https://s3.us-west-004.backblazeb2.com"
    access_key_id: "KEYID"
    secret_access_key: "SECRET"

notifications:
  success_url: "https://example.com/ok"
  failure_url: "https://example.com/fail"
  webhook_timeout: 10s
  webhook_retries: 1

defaults:
  remotes: [s3-primary, b2-secondary]

backups:
  pocket-id:
    object_name: pocket-id.db
    cmd: "sqlite3 /opt/pocket-id/data/pocket-id.db '.backup /dev/stdout'"

  gitea:
    object_name: gitea.tar
    cmd: "docker exec gitea gitea dump --type tar --file -"

  onepassword:
    object_name: backup.1pux
    file: /srv/manual/onepassword/export.1pux
    seal: true
    manual_only: true
    remotes: [b2-secondary]
`

func TestParseValidConfig(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Encryption.AgeRecipient != "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh" {
		t.Errorf("age_recipient = %q", cfg.Encryption.AgeRecipient)
	}
	if cfg.Settings.Namespace != "host01" {
		t.Errorf("namespace = %q", cfg.Settings.Namespace)
	}
	if len(cfg.Remotes) != 2 {
		t.Errorf("remotes count = %d", len(cfg.Remotes))
	}
	if len(cfg.Backups) != 3 {
		t.Errorf("backups count = %d", len(cfg.Backups))
	}
	// Check duration parsing
	if cfg.Settings.CmdTimeoutD.String() != "1h0m0s" {
		t.Errorf("cmd_timeout duration = %v", cfg.Settings.CmdTimeoutD)
	}
	if cfg.Settings.RemoteTimeoutD.String() != "30m0s" {
		t.Errorf("remote_timeout duration = %v", cfg.Settings.RemoteTimeoutD)
	}
	if cfg.Settings.RemoteRetriesN != 4 {
		t.Errorf("remote_retries = %d", cfg.Settings.RemoteRetriesN)
	}
	if cfg.Notifications.WebhookRetriesN != 1 {
		t.Errorf("webhook_retries = %d", cfg.Notifications.WebhookRetriesN)
	}
	// Check derived keys
	entry := cfg.Backups["pocket-id"]
	if entry.DerivedKey("host01") != "host01/pocket-id.db.age" {
		t.Errorf("derived key = %q", entry.DerivedKey("host01"))
	}
	sealed := cfg.Backups["onepassword"]
	if sealed.DerivedKey("host01") != "host01/backup.1pux.age2" {
		t.Errorf("sealed derived key = %q", sealed.DerivedKey("host01"))
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  string
		wantErr string
	}{
		{
			name:    "missing_age_recipient",
			mutate:  `encryption: {}`,
			wantErr: "age_recipient is required",
		},
		{
			name: "bad_age_recipient_prefix",
			mutate: `encryption:
  age_recipient: "notage"`,
			wantErr: "age_recipient is invalid",
		},
		{
			name: "bad_age_recipient_format",
			mutate: `encryption:
  age_recipient: "age1notarealrecipient"
settings:
  namespace: host01`,
			wantErr: "age_recipient is invalid",
		},
		{
			name: "missing_namespace",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings: {}`,
			wantErr: "namespace is required",
		},
		{
			name: "negative_max_backup_bytes",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  max_backup_bytes: -1`,
			wantErr: "max_backup_bytes must be >= 0",
		},
		{
			name: "negative_min_disk_free_bytes",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  min_disk_free_bytes: -1`,
			wantErr: "min_disk_free_bytes must be >= 0",
		},
		{
			name: "bad_namespace_chars",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: "host 01"`,
			wantErr: "invalid characters",
		},
		{
			name: "remote_missing_name",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - type: s3
    bucket: b
    aws_profile: p`,
			wantErr: "name is required",
		},
		{
			name: "remote_duplicate_name",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
  - name: r1
    type: s3
    bucket: b2
    aws_profile: p2`,
			wantErr: "duplicate remote name",
		},
		{
			name: "remote_bad_type",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: gcs
    bucket: b
    aws_profile: p`,
			wantErr: "type must be 's3'",
		},
		{
			name: "remote_no_bucket",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    aws_profile: p`,
			wantErr: "bucket is required",
		},
		{
			name: "remote_no_creds",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b`,
			wantErr: "exactly one credential method",
		},
		{
			name: "remote_multiple_creds",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
    access_key_id: k
    secret_access_key: s`,
			wantErr: "exactly one credential method",
		},
		{
			name: "remote_profile_and_default_chain",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
    credential_source: default_chain`,
			wantErr: "exactly one credential method",
		},
		{
			name: "remote_unsupported_credential_source",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
    credential_source: custom_source`,
			wantErr: "unsupported credential_source",
		},
		{
			name: "remote_partial_explicit_keys",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    access_key_id: k`,
			wantErr: "access_key_id and secret_access_key must both be set",
		},
		{
			name: "backup_no_source",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db`,
			wantErr: "exactly one source",
		},
		{
			name: "backup_multiple_sources",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
    file: /tmp/test`,
			wantErr: "exactly one source",
		},
		{
			name: "backup_missing_object_name",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    cmd: "echo hello"`,
			wantErr: "object_name is required",
		},
		{
			name: "backup_object_name_ends_age",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.age
    cmd: "echo hello"`,
			wantErr: "must not end with .age",
		},
		{
			name: "backup_object_name_ends_age2",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.age2
    cmd: "echo hello"`,
			wantErr: "must not end with .age",
		},
		{
			name: "backup_no_remotes",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
backups:
  test:
    object_name: test.db
    cmd: "echo hello"`,
			wantErr: "no remotes configured",
		},
		{
			name: "backup_unknown_remote",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
    remotes: [nonexistent]`,
			wantErr: "unknown remote",
		},
		{
			name: "backup_duplicate_remote_ref",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
    remotes: [r1, r1]`,
			wantErr: "duplicate remote reference",
		},
		{
			name: "defaults_unknown_remote",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [nonexistent]`,
			wantErr: "unknown remote",
		},
		{
			name: "defaults_duplicate_remote_ref",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1, r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"`,
			wantErr: "defaults.remotes contains duplicate remote",
		},
		{
			name: "duplicate_derived_key",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test1:
    object_name: same.db
    cmd: "echo a"
  test2:
    object_name: same.db
    cmd: "echo b"`,
			wantErr: "duplicate derived key",
		},
		{
			name: "bad_duration",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  cmd_timeout: invalid`,
			wantErr: "invalid cmd_timeout",
		},
		{
			name: "bad_remote_timeout",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  remote_timeout: invalid`,
			wantErr: "invalid remote_timeout",
		},
		{
			name: "negative_remote_retries",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  remote_retries: -1`,
			wantErr: "invalid remote_retries",
		},
		{
			name: "negative_lock_wait",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  lock_wait: -1s`,
			wantErr: "invalid lock_wait",
		},
		{
			name: "negative_webhook_retries",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
notifications:
  webhook_retries: -1
backups:
  test:
    object_name: test.db
    cmd: "echo hello"`,
			wantErr: "invalid webhook_retries",
		},
		{
			name: "both_remote_and_io_timeout",
			mutate: `encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  remote_timeout: 30m
  io_timeout: 30m`,
			wantErr: "cannot both be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.mutate))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolvedRemotes(t *testing.T) {
	entry := BackupEntry{Remotes: []string{"custom"}}
	if r := entry.ResolvedRemotes([]string{"default"}); len(r) != 1 || r[0] != "custom" {
		t.Errorf("expected [custom], got %v", r)
	}

	entry2 := BackupEntry{}
	if r := entry2.ResolvedRemotes([]string{"default"}); len(r) != 1 || r[0] != "default" {
		t.Errorf("expected [default], got %v", r)
	}
}

func TestRemoteCredentialSourceDefaultChain(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    credential_source: default_chain
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`
	_, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTimeoutAndRetryDefaults(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`
	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Settings.CmdTimeoutD != DefaultCmdTimeout {
		t.Errorf("cmd timeout default = %v, want %v", parsed.Settings.CmdTimeoutD, DefaultCmdTimeout)
	}
	if parsed.Settings.RemoteTimeoutD != DefaultRemoteTimeout {
		t.Errorf("remote timeout default = %v, want %v", parsed.Settings.RemoteTimeoutD, DefaultRemoteTimeout)
	}
	if parsed.Settings.RemoteRetriesN != DefaultRemoteRetries {
		t.Errorf("remote retries default = %d, want %d", parsed.Settings.RemoteRetriesN, DefaultRemoteRetries)
	}
	if parsed.Notifications.WebhookTimeoutD != DefaultWebhookTimeout {
		t.Errorf("webhook timeout default = %v, want %v", parsed.Notifications.WebhookTimeoutD, DefaultWebhookTimeout)
	}
	if parsed.Notifications.WebhookRetriesN != DefaultWebhookRetries {
		t.Errorf("webhook retries default = %d, want %d", parsed.Notifications.WebhookRetriesN, DefaultWebhookRetries)
	}
}

func TestIOTimeoutBackwardCompatibility(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
  io_timeout: 11m
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`
	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Settings.RemoteTimeoutD != 11*time.Minute {
		t.Errorf("remote timeout = %v, want %v", parsed.Settings.RemoteTimeoutD, 11*time.Minute)
	}
}

func TestParseRejectsUnknownTopLevelField(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
unexpected_top_level: true
`

	_, err := Parse([]byte(cfg))
	if err == nil {
		t.Fatal("expected parse error for unknown top-level field")
	}
	if !strings.Contains(err.Error(), "unexpected_top_level") {
		t.Errorf("error should mention unknown field, got: %v", err)
	}
}

func TestParseRejectsUnknownNestedField(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
    unknown_backup_field: true
`

	_, err := Parse([]byte(cfg))
	if err == nil {
		t.Fatal("expected parse error for unknown nested field")
	}
	if !strings.Contains(err.Error(), "unknown_backup_field") {
		t.Errorf("error should mention unknown field, got: %v", err)
	}
}

func TestBackupIDAndObjectNameNormalization(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  "  spaced-id  ":
    object_name: "  data.db  "
    cmd: "echo hello"
`
	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := parsed.Backups["spaced-id"]; !ok {
		t.Fatalf("expected normalized backup id to exist")
	}
	if _, ok := parsed.Backups["  spaced-id  "]; ok {
		t.Fatalf("unexpected non-normalized backup id key")
	}

	entry := parsed.Backups["spaced-id"]
	if entry.ObjectName != "data.db" {
		t.Fatalf("object_name = %q, want %q", entry.ObjectName, "data.db")
	}
	if got := entry.DerivedKey(parsed.Settings.Namespace); got != "host01/data.db.age" {
		t.Fatalf("derived key = %q, want %q", got, "host01/data.db.age")
	}
}

func TestDuplicateBackupIDAfterTrim(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  plain:
    object_name: one.db
    cmd: "echo one"
  " plain ":
    object_name: two.db
    cmd: "echo two"
`
	_, err := Parse([]byte(cfg))
	if err == nil {
		t.Fatal("expected duplicate backup id error")
	}
	if !strings.Contains(err.Error(), "duplicate backup_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseMultiRecipientList(t *testing.T) {
	id1, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	r1 := id1.Recipient().String()
	r2 := id2.Recipient().String()

	cfg := fmt.Sprintf(`
encryption:
  age_recipient:
    - %q
    - %q
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`, r1, r2)

	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := r1 + "\n" + r2
	if parsed.Encryption.AgeRecipient != want {
		t.Fatalf("age_recipient = %q, want %q", parsed.Encryption.AgeRecipient, want)
	}
}

func TestParseDuplicateRecipientRejected(t *testing.T) {
	id1, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	r := id1.Recipient().String()

	cfg := fmt.Sprintf(`
encryption:
  age_recipient:
    - %q
    - %q
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`, r, r)

	_, err = Parse([]byte(cfg))
	if err == nil {
		t.Fatal("expected duplicate recipient error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSigningConfigParsesValid(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
signing:
  host_key: "/etc/ssh/ssh_host_ed25519_key"
  allowed_signers_file: "/path/to/known_hosts"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`
	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Signing.HostKey != "/etc/ssh/ssh_host_ed25519_key" {
		t.Errorf("host_key = %q", parsed.Signing.HostKey)
	}
	if parsed.Signing.AllowedSignersFile != "/path/to/known_hosts" {
		t.Errorf("allowed_signers_file = %q", parsed.Signing.AllowedSignersFile)
	}
}

func TestSigningConfigNoSection(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`
	_, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("config without signing section should parse: %v", err)
	}
}

func TestSigningAllowedSignersNonEd25519Rejected(t *testing.T) {
	cfg := `
encryption:
  age_recipient: "age15h2c52h35t8le3akxa6stl7f9a86sa4n3exgx8u5y7hhwp539axq6v2pwh"
signing:
  allowed_signers:
    - "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC7..."
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`
	_, err := Parse([]byte(cfg))
	if err == nil {
		t.Fatal("expected validation error for non-Ed25519 allowed signer")
	}
	if !strings.Contains(err.Error(), "signing.allowed_signers") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHybridAgeRecipient(t *testing.T) {
	hybridID, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatalf("generating hybrid identity: %v", err)
	}
	hybridRecipient := hybridID.Recipient().String()

	cfg := fmt.Sprintf(`
encryption:
  age_recipient: %q
settings:
  namespace: host01
remotes:
  - name: r1
    type: s3
    bucket: b
    aws_profile: p
defaults:
  remotes: [r1]
backups:
  test:
    object_name: test.db
    cmd: "echo hello"
`, hybridRecipient)

	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error parsing hybrid recipient: %v", err)
	}
	if parsed.Encryption.AgeRecipient != hybridRecipient {
		t.Fatalf("age_recipient = %q, want %q", parsed.Encryption.AgeRecipient, hybridRecipient)
	}
}
