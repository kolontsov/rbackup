//go:build integration

package integration

import (
	"context"
	stded25519 "crypto/ed25519"
	crand "crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/verify"
	"golang.org/x/crypto/ssh"
)

func writeSSHHostKey(t *testing.T) (privPath string, pubKey string) {
	t.Helper()

	pub, priv, err := stded25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	privPath = filepath.Join(dir, "ssh_host_ed25519_key")
	if err := os.WriteFile(privPath, pem.EncodeToMemory(pemBytes), 0600); err != nil {
		t.Fatal(err)
	}
	pubKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return privPath, pubKey
}

// ---------------------------------------------------------------------------
// Restore: latest version
// ---------------------------------------------------------------------------

func TestRestoreLatest(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	content := "restore-latest-test-data"
	srcFile := writeTestFile(t, tmpDir, "restore.txt", content)

	cfg.Backups["restore-test"] = config.BackupEntry{
		ObjectName: "restore.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "restore-test", cfg.Backups["restore-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	// Restore from minio1
	outputPath := filepath.Join(tmpDir, "restored.txt")
	client := testClient(t, cfg.Remotes[0])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/restore.txt.age",
		IdentityPath: testIdentityFile,
		OutputPath:   outputPath,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	restored, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != content {
		t.Errorf("restored content = %q, want %q", restored, content)
	}

	// Verify output file permissions
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("restored file permissions = %04o, want 0600", info.Mode().Perm())
	}
}

// ---------------------------------------------------------------------------
// Restore: explicit version ID
// ---------------------------------------------------------------------------

func TestRestoreVersionID(t *testing.T) {
	// Enable versioning on minio1 for this test
	if err := enableVersioning(minio1Endpoint, minio1Bucket); err != nil {
		t.Fatalf("enabling versioning: %v", err)
	}

	cfg := testConfig(t)
	// Only use minio1 for this test
	cfg.Defaults.Remotes = []string{"minio1"}
	tmpDir := t.TempDir()

	// Upload version 1
	srcFile1 := writeTestFile(t, tmpDir, "ver1.txt", "version-1-content")
	cfg.Backups["ver-test"] = config.BackupEntry{
		ObjectName: "ver.txt",
		File:       srcFile1,
	}
	clients := testClients(t, cfg)

	r1 := backup.RunBackup(context.Background(), "ver-test", cfg.Backups["ver-test"], cfg, clients, backup.BackupOptions{})
	if r1.Status != "success" {
		t.Fatalf("backup v1 failed: %+v", r1.Results)
	}
	v1ID := r1.Results[0].VersionID

	// Upload version 2 (overwrites same key)
	os.WriteFile(srcFile1, []byte("version-2-content"), 0644)
	r2 := backup.RunBackup(context.Background(), "ver-test", cfg.Backups["ver-test"], cfg, clients, backup.BackupOptions{})
	if r2.Status != "success" {
		t.Fatalf("backup v2 failed: %+v", r2.Results)
	}

	if v1ID == "" {
		t.Skip("MinIO did not return version IDs; skipping version-specific restore")
	}

	// Restore version 1 explicitly
	outPath := filepath.Join(tmpDir, "restored-v1.txt")
	client := testClient(t, cfg.Remotes[0])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/ver.txt.age",
		VersionID:    v1ID,
		IdentityPath: testIdentityFile,
		OutputPath:   outPath,
	})
	if err != nil {
		t.Fatalf("restore v1: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	if string(data) != "version-1-content" {
		t.Errorf("restored v1 = %q, want %q", data, "version-1-content")
	}
}

// ---------------------------------------------------------------------------
// Sealed (.age2) backup, restore, verify
// ---------------------------------------------------------------------------

func TestSealedRestoreVerify(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	content := "sealed-secret-data"
	srcFile := writeTestFile(t, tmpDir, "secret.bin", content)

	cfg.Backups["sealed-test"] = config.BackupEntry{
		ObjectName: "secret.bin",
		File:       srcFile,
		Seal:       true,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "sealed-test", cfg.Backups["sealed-test"], cfg, clients, backup.BackupOptions{
		SealPassphrase: testSealPass,
	})
	if receipt.Status != "success" {
		t.Fatalf("sealed backup failed: %+v", receipt.Results)
	}
	// Key should end with .age2
	for _, r := range receipt.Results {
		if !strings.HasSuffix(r.Key, ".age2") {
			t.Errorf("sealed key should end .age2, got %q", r.Key)
		}
	}

	// Restore sealed
	outPath := filepath.Join(tmpDir, "restored-sealed.bin")
	client := testClient(t, cfg.Remotes[0])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:         client,
		Key:            "integ/secret.bin.age2",
		IdentityPath:   testIdentityFile,
		SealPassphrase: testSealPass,
		OutputPath:     outPath,
	})
	if err != nil {
		t.Fatalf("sealed restore: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	if string(data) != content {
		t.Errorf("sealed restore = %q", data)
	}

	// Verify sealed
	err = verify.Verify(context.Background(), verify.VerifyOptions{
		Client:         client,
		Key:            "integ/secret.bin.age2",
		IdentityPath:   testIdentityFile,
		SealPassphrase: testSealPass,
	})
	if err != nil {
		t.Fatalf("sealed verify: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sealed restore: wrong passphrase
// ---------------------------------------------------------------------------

func TestSealedWrongPassphrase(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "sealed-wrong.bin", "some data")

	cfg.Backups["sealed-wrong"] = config.BackupEntry{
		ObjectName: "sealed-wrong.bin",
		File:       srcFile,
		Seal:       true,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "sealed-wrong", cfg.Backups["sealed-wrong"], cfg, clients, backup.BackupOptions{
		SealPassphrase: testSealPass,
	})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	// Attempt restore with wrong passphrase
	outPath := filepath.Join(tmpDir, "wrong-pass.bin")
	client := testClient(t, cfg.Remotes[0])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:         client,
		Key:            "integ/sealed-wrong.bin.age2",
		IdentityPath:   testIdentityFile,
		SealPassphrase: "wrong-passphrase",
		OutputPath:     outPath,
	})
	if err == nil {
		t.Fatal("expected error with wrong passphrase")
	}
}

// ---------------------------------------------------------------------------
// Sealed restore: missing passphrase
// ---------------------------------------------------------------------------

func TestSealedNoPassphrase(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "sealed-nop.bin", "data")

	cfg.Backups["sealed-nop"] = config.BackupEntry{
		ObjectName: "sealed-nop.bin",
		File:       srcFile,
		Seal:       true,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "sealed-nop", cfg.Backups["sealed-nop"], cfg, clients, backup.BackupOptions{
		SealPassphrase: testSealPass,
	})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	outPath := filepath.Join(tmpDir, "nop.bin")
	client := testClient(t, cfg.Remotes[0])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/sealed-nop.bin.age2",
		IdentityPath: testIdentityFile,
		// No passphrase
		OutputPath: outPath,
	})
	if err == nil {
		t.Fatal("expected error without passphrase")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("error should mention passphrase: %v", err)
	}
}

func TestSignedRestoreVerify(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	content := "signed-restore-verify-data"
	srcFile := writeTestFile(t, tmpDir, "signed.txt", content)
	hostKeyPath, pubKey := writeSSHHostKey(t)

	cfg.Signing.HostKey = hostKeyPath
	cfg.Backups["signed-test"] = config.BackupEntry{
		ObjectName: "signed.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "signed-test", cfg.Backups["signed-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("signed backup failed: %+v", receipt.Results)
	}

	head := headObject(t, minio1Endpoint, minio1Bucket, "integ/signed.txt.age")
	if strings.TrimSpace(head.Metadata[crypto.SignatureMetadataKey]) == "" {
		t.Fatalf("missing %s metadata on signed backup", crypto.SignatureMetadataKey)
	}

	client := testClient(t, cfg.Remotes[0])
	if err := verify.Verify(context.Background(), verify.VerifyOptions{
		Client:         client,
		Key:            "integ/signed.txt.age",
		IdentityPath:   testIdentityFile,
		AllowedSigners: []string{pubKey},
	}); err != nil {
		t.Fatalf("signed verify: %v", err)
	}

	outPath := filepath.Join(tmpDir, "signed-restored.txt")
	if err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:         client,
		Key:            "integ/signed.txt.age",
		IdentityPath:   testIdentityFile,
		AllowedSigners: []string{pubKey},
		OutputPath:     outPath,
	}); err != nil {
		t.Fatalf("signed restore: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("restored signed content = %q, want %q", data, content)
	}
}

func TestVerifyRejectsUnsignedObjectWhenSignersConfigured(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "unsigned.txt", "unsigned data")
	_, pubKey := writeSSHHostKey(t)

	cfg.Backups["unsigned-test"] = config.BackupEntry{
		ObjectName: "unsigned.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "unsigned-test", cfg.Backups["unsigned-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("unsigned backup failed: %+v", receipt.Results)
	}

	client := testClient(t, cfg.Remotes[0])
	err := verify.Verify(context.Background(), verify.VerifyOptions{
		Client:         client,
		Key:            "integ/unsigned.txt.age",
		IdentityPath:   testIdentityFile,
		AllowedSigners: []string{pubKey},
	})
	if err == nil {
		t.Fatal("expected unsigned backup to be rejected when signers are configured")
	}
	if !strings.Contains(err.Error(), "missing x-rbackup-signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Verify: streaming (no temp files)
// ---------------------------------------------------------------------------

func TestVerifyStreaming(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "verify.txt", "verify streaming test")

	cfg.Backups["verify-test"] = config.BackupEntry{
		ObjectName: "verify.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "verify-test", cfg.Backups["verify-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	client := testClient(t, cfg.Remotes[0])
	err := verify.Verify(context.Background(), verify.VerifyOptions{
		Client:       client,
		Key:          "integ/verify.txt.age",
		IdentityPath: testIdentityFile,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Verify: wrong identity
// ---------------------------------------------------------------------------

func TestVerifyWrongIdentity(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "verify-wrong.txt", "wrong id test")

	cfg.Backups["verify-wrong"] = config.BackupEntry{
		ObjectName: "verify-wrong.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "verify-wrong", cfg.Backups["verify-wrong"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	// Create a different identity
	wrongIdPath := filepath.Join(tmpDir, "wrong-id.txt")
	wrongIdentity, _, err := crypto.DeriveIdentity("completely-different-pass-16", "other-org")
	if err != nil {
		t.Fatalf("derive wrong identity: %v", err)
	}
	if err := crypto.WriteIdentityFile(wrongIdPath, wrongIdentity); err != nil {
		t.Fatalf("write wrong identity: %v", err)
	}

	client := testClient(t, cfg.Remotes[0])
	err = verify.Verify(context.Background(), verify.VerifyOptions{
		Client:       client,
		Key:          "integ/verify-wrong.txt.age",
		IdentityPath: wrongIdPath,
	})
	if err == nil {
		t.Fatal("expected error with wrong identity")
	}
}

// ---------------------------------------------------------------------------
// Restore: overwrite guard
// ---------------------------------------------------------------------------

func TestRestoreOverwriteGuard(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "overwrite.txt", "overwrite test")

	cfg.Backups["ow-test"] = config.BackupEntry{
		ObjectName: "overwrite.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "ow-test", cfg.Backups["ow-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	// Create existing output file
	outPath := filepath.Join(tmpDir, "existing.txt")
	os.WriteFile(outPath, []byte("existing"), 0644)

	client := testClient(t, cfg.Remotes[0])

	// Without --force: should fail
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/overwrite.txt.age",
		IdentityPath: testIdentityFile,
		OutputPath:   outPath,
	})
	if err == nil {
		t.Fatal("expected error for existing output file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}

	// With --force: should succeed
	err = verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/overwrite.txt.age",
		IdentityPath: testIdentityFile,
		OutputPath:   outPath,
		Force:        true,
	})
	if err != nil {
		t.Fatalf("restore with --force: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	if string(data) != "overwrite test" {
		t.Errorf("restored = %q", data)
	}
}

// ---------------------------------------------------------------------------
// Restore: temp file mode 0600
// ---------------------------------------------------------------------------

func TestRestoreTempFileMode(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "mode.txt", "mode test")

	cfg.Backups["mode-test"] = config.BackupEntry{
		ObjectName: "mode.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "mode-test", cfg.Backups["mode-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup: %+v", receipt.Results)
	}

	outPath := filepath.Join(tmpDir, "mode-out.txt")
	client := testClient(t, cfg.Remotes[0])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/mode.txt.age",
		IdentityPath: testIdentityFile,
		OutputPath:   outPath,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	info, _ := os.Stat(outPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("output file permissions = %04o, want 0600", info.Mode().Perm())
	}
}

// ---------------------------------------------------------------------------
// Restore from second remote
// ---------------------------------------------------------------------------

func TestRestoreFromSecondRemote(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	content := "restore-from-second-remote"
	srcFile := writeTestFile(t, tmpDir, "second.txt", content)

	cfg.Backups["second-test"] = config.BackupEntry{
		ObjectName: "second.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "second-test", cfg.Backups["second-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup: %+v", receipt.Results)
	}

	// Restore from minio2
	outPath := filepath.Join(tmpDir, "from-second.txt")
	client := testClient(t, cfg.Remotes[1])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/second.txt.age",
		IdentityPath: testIdentityFile,
		OutputPath:   outPath,
	})
	if err != nil {
		t.Fatalf("restore from minio2: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	if string(data) != content {
		t.Errorf("restored = %q", data)
	}
}
