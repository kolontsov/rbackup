package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/storage"
)

func TestMaxBytesWriterWithinLimit(t *testing.T) {
	var buf bytes.Buffer
	w := &maxBytesWriter{
		w:         &buf,
		remaining: 5,
		limit:     5,
	}

	n, err := w.Write([]byte("abc"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 3 {
		t.Fatalf("written bytes = %d, want 3", n)
	}
	if got := buf.String(); got != "abc" {
		t.Fatalf("buffer = %q, want %q", got, "abc")
	}
}

func TestMaxBytesWriterOverflow(t *testing.T) {
	var buf bytes.Buffer
	w := &maxBytesWriter{
		w:         &buf,
		remaining: 5,
		limit:     5,
	}

	n, err := w.Write([]byte("abcdef"))
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !strings.Contains(err.Error(), "max_backup_bytes") {
		t.Fatalf("expected max_backup_bytes error, got: %v", err)
	}
	if n != 5 {
		t.Fatalf("written bytes = %d, want 5", n)
	}
	if got := buf.String(); got != "abcde" {
		t.Fatalf("buffer = %q, want %q", got, "abcde")
	}

	n, err = w.Write([]byte("z"))
	if err == nil {
		t.Fatal("expected overflow error on subsequent write")
	}
	if n != 0 {
		t.Fatalf("written bytes = %d, want 0", n)
	}
}

func TestRunBackupMaxBackupBytesHardLimit(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generating identity: %v", err)
	}

	spoolDir := t.TempDir()
	cfg := &config.Config{
		Encryption: config.Encryption{
			AgeRecipient: identity.Recipient().String(),
		},
		Settings: config.Settings{
			Namespace:     "testns",
			SpoolDir:      spoolDir,
			MaxBackupBytes: 32,
		},
	}

	entry := config.BackupEntry{
		ObjectName: "limited.txt",
		Cmd:        "printf 'this plaintext is intentionally long enough to exceed the ciphertext limit'",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	receipt := RunBackup(ctx, "limited", entry, cfg, nil, BackupOptions{})
	if receipt.Status != StatusFailure {
		t.Fatalf("status = %q, want %q", receipt.Status, StatusFailure)
	}
	if len(receipt.Results) == 0 {
		t.Fatal("expected failure result")
	}
	if !strings.Contains(receipt.Results[0].Error, "max_backup_bytes") {
		t.Fatalf("expected max_backup_bytes failure, got: %+v", receipt.Results)
	}

	files, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("reading spool dir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no leftover spool files, found %d", len(files))
	}
}

func TestPrintReceiptsNilIsJSONArray(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	if err := PrintReceipts(nil); err != nil {
		t.Fatalf("PrintReceipts: %v", err)
	}
	_ = w.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	var decoded []Receipt
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v (out=%q)", err, string(out))
	}
	if decoded == nil {
		t.Fatalf("expected JSON array, got nil slice (out=%q)", string(out))
	}
	if len(decoded) != 0 {
		t.Fatalf("expected empty array, got len=%d", len(decoded))
	}
}

func TestRunBackupPreflightFailureIncludesRemoteAndKey(t *testing.T) {
	cfg := &config.Config{
		Settings: config.Settings{
			Namespace: "testns",
			SpoolDir:  t.TempDir(),
		},
		Defaults: config.Defaults{
			Remotes: []string{"r1", "r2"},
		},
	}
	entry := config.BackupEntry{
		ObjectName: "secret.bin",
		File:       "/tmp/ignored",
		Seal:       true,
	}

	receipt := RunBackup(context.Background(), "sealed-preflight", entry, cfg, nil, BackupOptions{})
	if receipt.Status != StatusFailure {
		t.Fatalf("status = %q, want %q", receipt.Status, StatusFailure)
	}
	if receipt.CreatedAt == "" {
		t.Fatal("expected created_at for failure receipt")
	}
	if len(receipt.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(receipt.Results))
	}

	wantKey := "testns/secret.bin.age2"
	for i, rr := range receipt.Results {
		if rr.Remote == "" {
			t.Fatalf("result[%d] remote should not be empty", i)
		}
		if rr.Key != wantKey {
			t.Fatalf("result[%d] key = %q, want %q", i, rr.Key, wantKey)
		}
		if rr.Status != StatusFailure {
			t.Fatalf("result[%d] status = %q, want %q", i, rr.Status, StatusFailure)
		}
		if !strings.Contains(rr.Error, "passphrase") {
			t.Fatalf("result[%d] error = %q, want passphrase message", i, rr.Error)
		}
	}
}

func TestRunAllManualOnlyReceiptIncludesSkippedResults(t *testing.T) {
	cfg := &config.Config{
		Settings: config.Settings{
			Namespace: "testns",
		},
		Defaults: config.Defaults{
			Remotes: []string{"r1", "r2"},
		},
		Backups: map[string]config.BackupEntry{
			"manual": {
				ObjectName: "manual.db",
				File:       "/tmp/manual.db",
				ManualOnly: true,
			},
		},
	}

	receipts := RunAll(context.Background(), cfg, nil, BackupOptions{})
	if len(receipts) != 1 {
		t.Fatalf("receipts len = %d, want 1", len(receipts))
	}
	receipt := receipts[0]
	if receipt.Status != StatusSkipped {
		t.Fatalf("status = %q, want %q", receipt.Status, StatusSkipped)
	}
	if receipt.Reason != "manual_only" {
		t.Fatalf("reason = %q, want %q", receipt.Reason, "manual_only")
	}
	if receipt.CreatedAt == "" {
		t.Fatal("expected created_at for skipped receipt")
	}
	if len(receipt.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(receipt.Results))
	}
	for i, rr := range receipt.Results {
		if rr.Status != StatusSkipped {
			t.Fatalf("result[%d] status = %q, want %q", i, rr.Status, StatusSkipped)
		}
		if rr.Key != "testns/manual.db.age" {
			t.Fatalf("result[%d] key = %q", i, rr.Key)
		}
		if rr.Error != "manual_only" {
			t.Fatalf("result[%d] error = %q, want manual_only", i, rr.Error)
		}
	}
}

func TestRunBackupKeepsSpoolOnTotalRemoteFailure(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generating identity: %v", err)
	}

	spoolDir := t.TempDir()
	cfg := &config.Config{
		Encryption: config.Encryption{
			AgeRecipient: identity.Recipient().String(),
		},
		Settings: config.Settings{
			Namespace: "testns",
			SpoolDir:  spoolDir,
		},
		Defaults: config.Defaults{
			Remotes: []string{"missing-remote"},
		},
	}

	entry := config.BackupEntry{
		ObjectName: "test.db",
		Cmd:        "echo hello",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	receipt := RunBackup(ctx, "test", entry, cfg, map[string]*storage.Client{}, BackupOptions{})
	if receipt.Status != StatusFailure {
		t.Fatalf("status = %q, want %q", receipt.Status, StatusFailure)
	}

	files, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("reading spool dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 retained spool file, found %d", len(files))
	}
}

func TestRunBackupMinDiskFreeBytesFails(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generating identity: %v", err)
	}

	spoolDir := t.TempDir()
	cfg := &config.Config{
		Encryption: config.Encryption{
			AgeRecipient: identity.Recipient().String(),
		},
		Settings: config.Settings{
			Namespace:        "testns",
			SpoolDir:         spoolDir,
			MinDiskFreeBytes: 1 << 62,
		},
		Defaults: config.Defaults{
			Remotes: []string{"r1"},
		},
	}

	entry := config.BackupEntry{
		ObjectName: "test.db",
		Cmd:        "echo hello",
	}

	receipt := RunBackup(context.Background(), "test", entry, cfg, nil, BackupOptions{})
	if receipt.Status != StatusFailure {
		t.Fatalf("status = %q, want %q", receipt.Status, StatusFailure)
	}
	if !strings.Contains(receipt.Results[0].Error, "insufficient disk space") {
		t.Fatalf("expected disk space error, got: %s", receipt.Results[0].Error)
	}
}
