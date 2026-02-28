//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
)

// ---------------------------------------------------------------------------
// Backup: cmd source
// ---------------------------------------------------------------------------

func TestBackupCmd(t *testing.T) {
	cfg := testConfig(t)
	cfg.Backups["echo-test"] = config.BackupEntry{
		ObjectName: "echo-test.txt",
		Cmd:        "echo 'hello from cmd'",
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "echo-test", cfg.Backups["echo-test"], cfg, clients, backup.BackupOptions{})

	if receipt.Status != "success" {
		t.Fatalf("expected success, got %s: %+v", receipt.Status, receipt.Results)
	}
	if len(receipt.Results) != 2 {
		t.Fatalf("expected 2 remote results, got %d", len(receipt.Results))
	}
	for _, r := range receipt.Results {
		if r.Status != "success" {
			t.Errorf("remote %s: %s - %s", r.Remote, r.Status, r.Error)
		}
		if r.Key != "integ/echo-test.txt.age" {
			t.Errorf("key = %q", r.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// Backup: file source
// ---------------------------------------------------------------------------

func TestBackupFile(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "data.bin", "file source content here")

	cfg.Backups["file-test"] = config.BackupEntry{
		ObjectName: "data.bin",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "file-test", cfg.Backups["file-test"], cfg, clients, backup.BackupOptions{})

	if receipt.Status != "success" {
		t.Fatalf("expected success: %+v", receipt.Results)
	}
}

// ---------------------------------------------------------------------------
// Backup: dir source (tar.gz)
// ---------------------------------------------------------------------------

func TestBackupDir(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcDir := writeTestDir(t, tmpDir, "mydata", map[string]string{
		"a.txt":     "aaa",
		"b.txt":     "bbb",
		"sub/c.txt": "ccc",
	})

	cfg.Backups["dir-test"] = config.BackupEntry{
		ObjectName: "mydata.tar",
		Dir:        srcDir,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "dir-test", cfg.Backups["dir-test"], cfg, clients, backup.BackupOptions{})

	if receipt.Status != "success" {
		t.Fatalf("expected success: %+v", receipt.Results)
	}
}

// ---------------------------------------------------------------------------
// Upload metadata verification
// ---------------------------------------------------------------------------

func TestUploadMetadata(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "meta.txt", "metadata test content")

	cfg.Backups["meta-test"] = config.BackupEntry{
		ObjectName: "meta.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "meta-test", cfg.Backups["meta-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	// Verify metadata on minio1
	head := headObject(t, minio1Endpoint, minio1Bucket, "integ/meta.txt.age")
	sha256Val, ok := head.Metadata["x-rbackup-sha256"]
	if !ok || sha256Val == "" {
		t.Error("missing x-rbackup-sha256 metadata")
	}
	if len(sha256Val) != 64 {
		t.Errorf("x-rbackup-sha256 len = %d, want 64", len(sha256Val))
	}
	if _, err := hex.DecodeString(sha256Val); err != nil {
		t.Errorf("x-rbackup-sha256 is not valid hex: %v", err)
	}

	createdAt, ok := head.Metadata["x-rbackup-created-at"]
	if !ok || createdAt == "" {
		t.Error("missing x-rbackup-created-at metadata")
	}
	// Should be valid RFC3339
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		t.Errorf("x-rbackup-created-at %q is not valid RFC3339: %v", createdAt, err)
	}

	recipientFP, ok := head.Metadata[crypto.RecipientMetadataFingerprintKey]
	if !ok || recipientFP == "" {
		t.Errorf("missing %s metadata", crypto.RecipientMetadataFingerprintKey)
	}
	if len(recipientFP) != 64 {
		t.Errorf("%s len = %d, want 64", crypto.RecipientMetadataFingerprintKey, len(recipientFP))
	}
}

// ---------------------------------------------------------------------------
// Receipt correlation across remotes
// ---------------------------------------------------------------------------

func TestReceiptCorrelation(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "corr.txt", "correlation test")

	cfg.Backups["corr-test"] = config.BackupEntry{
		ObjectName: "corr.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "corr-test", cfg.Backups["corr-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	// All remote results should have the same key because rbackup encrypts once
	// and uploads the same ciphertext to each selected remote.
	if len(receipt.Results) < 2 {
		t.Fatalf("expected >=2 results, got %d", len(receipt.Results))
	}
	key := receipt.Results[0].Key
	for _, r := range receipt.Results[1:] {
		if r.Key != key {
			t.Errorf("key mismatch: %q vs %q", key, r.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// backup --all with manual_only skip
// ---------------------------------------------------------------------------

func TestBackupAllManualOnlySkip(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()

	cfg.Backups["auto-entry"] = config.BackupEntry{
		ObjectName: "auto.txt",
		Cmd:        "echo auto-data",
	}
	cfg.Backups["manual-entry"] = config.BackupEntry{
		ObjectName: "manual.txt",
		File:       writeTestFile(t, tmpDir, "manual.txt", "manual data"),
		ManualOnly: true,
	}
	clients := testClients(t, cfg)

	receipts := backup.RunAll(context.Background(), cfg, clients, backup.BackupOptions{})

	if len(receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(receipts))
	}

	var autoReceipt, manualReceipt *backup.Receipt
	for i := range receipts {
		switch receipts[i].BackupID {
		case "auto-entry":
			autoReceipt = &receipts[i]
		case "manual-entry":
			manualReceipt = &receipts[i]
		}
	}

	if autoReceipt == nil {
		t.Fatal("missing auto-entry receipt")
	}
	if autoReceipt.Status != "success" {
		t.Errorf("auto-entry status = %q: %+v", autoReceipt.Status, autoReceipt.Results)
	}

	if manualReceipt == nil {
		t.Fatal("missing manual-entry receipt")
	}
	if manualReceipt.Status != "skipped" {
		t.Errorf("manual-entry status = %q, want skipped", manualReceipt.Status)
	}
	if manualReceipt.Reason != "manual_only" {
		t.Errorf("manual-entry reason = %q", manualReceipt.Reason)
	}
}

// ---------------------------------------------------------------------------
// Partial remote failure
// ---------------------------------------------------------------------------

func TestPartialRemoteFailure(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "partial.txt", "partial failure test")

	cfg.Backups["partial-test"] = config.BackupEntry{
		ObjectName: "partial.txt",
		File:       srcFile,
	}

	// Build clients but give one a bad bucket
	clients := testClients(t, cfg)
	badRemote := cfg.Remotes[1]
	badRemote.Bucket = "nonexistent-bucket-xyz"
	badClient, _ := storage.NewClient(context.Background(), badRemote)
	clients["minio2"] = badClient

	receipt := backup.RunBackup(context.Background(), "partial-test", cfg.Backups["partial-test"], cfg, clients, backup.BackupOptions{})

	if receipt.Status != "failure" {
		t.Errorf("expected failure for partial remote failure, got %q", receipt.Status)
	}

	// At least one result should be success, at least one failure
	hasSuccess, hasFailure := false, false
	for _, r := range receipt.Results {
		if r.Status == "success" {
			hasSuccess = true
		}
		if r.Status == "failure" {
			hasFailure = true
		}
	}
	if !hasSuccess {
		t.Error("expected at least one successful upload")
	}
	if !hasFailure {
		t.Error("expected at least one failed upload")
	}
}

// ---------------------------------------------------------------------------
// Zero-byte source: cmd
// ---------------------------------------------------------------------------

func TestZeroByteCmdSource(t *testing.T) {
	cfg := testConfig(t)
	cfg.Backups["zero-cmd"] = config.BackupEntry{
		ObjectName: "zero-cmd.txt",
		Cmd:        "true", // produces 0 bytes
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "zero-cmd", cfg.Backups["zero-cmd"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "failure" {
		t.Errorf("expected failure for zero-byte cmd, got %q", receipt.Status)
	}
	found := false
	for _, r := range receipt.Results {
		if strings.Contains(r.Error, "0 bytes") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected '0 bytes' in error, got %+v", receipt.Results)
	}
}

// ---------------------------------------------------------------------------
// Zero-byte source: file
// ---------------------------------------------------------------------------

func TestZeroByteFileSource(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "empty.txt", "")

	cfg.Backups["zero-file"] = config.BackupEntry{
		ObjectName: "zero-file.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "zero-file", cfg.Backups["zero-file"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "failure" {
		t.Errorf("expected failure for zero-byte file, got %q", receipt.Status)
	}
}

// ---------------------------------------------------------------------------
// Zero-byte source: dir (empty directory)
// ---------------------------------------------------------------------------

func TestZeroByteDirSource(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	// Even an empty dir produces a non-zero tar.gz (tar headers),
	// so this test verifies the dir source works rather than expecting 0 bytes.
	emptyDir := filepath.Join(tmpDir, "emptydir")
	os.MkdirAll(emptyDir, 0755)

	cfg.Backups["empty-dir"] = config.BackupEntry{
		ObjectName: "empty-dir.tar",
		Dir:        emptyDir,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "empty-dir", cfg.Backups["empty-dir"], cfg, clients, backup.BackupOptions{})
	// An empty directory still produces tar headers, so this should succeed
	if receipt.Status != "success" {
		t.Fatalf("expected success for empty dir source, got %q: %+v", receipt.Status, receipt.Results)
	}
	if len(receipt.Results) == 0 {
		t.Fatal("expected per-remote results")
	}
	for _, r := range receipt.Results {
		if r.Status != "success" {
			t.Fatalf("remote %s status = %q, want success (err=%q)", r.Remote, r.Status, r.Error)
		}
	}
}

// ---------------------------------------------------------------------------
// Max backup bytes enforcement
// ---------------------------------------------------------------------------

func TestMaxBackupBytesExceeded(t *testing.T) {
	cfg := testConfig(t)
	cfg.Settings.MaxBackupBytes = 1 // 1 byte limit — any ciphertext will exceed this

	cfg.Backups["spool-limit"] = config.BackupEntry{
		ObjectName: "spool-limit.txt",
		Cmd:        "echo 'this will produce ciphertext larger than 1 byte'",
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "spool-limit", cfg.Backups["spool-limit"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "failure" {
		t.Errorf("expected failure for spool limit, got %q", receipt.Status)
	}
	found := false
	for _, r := range receipt.Results {
		if strings.Contains(r.Error, "max_backup_bytes") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected max_backup_bytes in error: %+v", receipt.Results)
	}
}

// ---------------------------------------------------------------------------
// Backup: cmd non-zero exit failure
// ---------------------------------------------------------------------------

func TestBackupCmdNonZeroExit(t *testing.T) {
	cfg := testConfig(t)
	cfg.Backups["bad-cmd"] = config.BackupEntry{
		ObjectName: "bad-cmd.txt",
		Cmd:        "echo some-data && exit 1",
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "bad-cmd", cfg.Backups["bad-cmd"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "failure" {
		t.Errorf("expected failure for non-zero exit, got %q", receipt.Status)
	}
}

// ---------------------------------------------------------------------------
// Backup: cmd timeout
// ---------------------------------------------------------------------------

func TestBackupCmdTimeout(t *testing.T) {
	cfg := testConfig(t)
	cfg.Settings.CmdTimeoutD = 500 * time.Millisecond

	cfg.Backups["slow-cmd"] = config.BackupEntry{
		ObjectName: "slow-cmd.txt",
		Cmd:        "sleep 30",
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "slow-cmd", cfg.Backups["slow-cmd"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "failure" {
		t.Errorf("expected failure for timeout, got %q", receipt.Status)
	}
}

// ---------------------------------------------------------------------------
// Multi-remote: backup uploads to both remotes
// ---------------------------------------------------------------------------

func TestMultiRemoteUpload(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "multi.txt", "multi-remote test")

	cfg.Backups["multi-test"] = config.BackupEntry{
		ObjectName: "multi.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "multi-test", cfg.Backups["multi-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("expected success: %+v", receipt.Results)
	}

	// Verify both remotes have the object
	head1 := headObject(t, minio1Endpoint, minio1Bucket, "integ/multi.txt.age")
	if head1.ContentLength == nil || *head1.ContentLength == 0 {
		t.Error("minio1 object has 0 length")
	}

	head2 := headObject(t, minio2Endpoint, minio2Bucket, "integ/multi.txt.age")
	if head2.ContentLength == nil || *head2.ContentLength == 0 {
		t.Error("minio2 object has 0 length")
	}

	// Same content length = same ciphertext uploaded to both
	if *head1.ContentLength != *head2.ContentLength {
		t.Errorf("content length mismatch: %d vs %d", *head1.ContentLength, *head2.ContentLength)
	}
}

// ---------------------------------------------------------------------------
// Key suffix derivation
// ---------------------------------------------------------------------------

func TestKeySuffixDerivation(t *testing.T) {
	entry := config.BackupEntry{ObjectName: "test.db", Seal: false}
	if entry.Suffix() != ".age" {
		t.Errorf("non-sealed suffix = %q", entry.Suffix())
	}
	if entry.DerivedKey("ns") != "ns/test.db.age" {
		t.Errorf("derived key = %q", entry.DerivedKey("ns"))
	}

	sealed := config.BackupEntry{ObjectName: "test.db", Seal: true}
	if sealed.Suffix() != ".age2" {
		t.Errorf("sealed suffix = %q", sealed.Suffix())
	}
	if sealed.DerivedKey("ns") != "ns/test.db.age2" {
		t.Errorf("sealed derived key = %q", sealed.DerivedKey("ns"))
	}
}

// ---------------------------------------------------------------------------
// Sealed backup without passphrase
// ---------------------------------------------------------------------------

func TestSealedBackupNoPassphrase(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "no-pass.txt", "data")

	cfg.Backups["no-pass"] = config.BackupEntry{
		ObjectName: "no-pass.txt",
		File:       srcFile,
		Seal:       true,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "no-pass", cfg.Backups["no-pass"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "failure" {
		t.Error("expected failure when seal passphrase is missing")
	}
	hasPassphraseErr := false
	for _, r := range receipt.Results {
		if strings.Contains(r.Error, "passphrase") {
			hasPassphraseErr = true
		}
	}
	if !hasPassphraseErr {
		t.Errorf("expected passphrase error: %+v", receipt.Results)
	}
}
