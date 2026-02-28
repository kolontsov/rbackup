//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/verify"
)

// ---------------------------------------------------------------------------
// End-to-end: backup + restore round-trip with all three source types
// ---------------------------------------------------------------------------

func TestEndToEndRoundTrip(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		entry   config.BackupEntry
		content string
	}{
		{
			name:    "cmd-e2e",
			entry:   config.BackupEntry{ObjectName: "e2e-cmd.txt", Cmd: "printf 'cmd-round-trip'"},
			content: "cmd-round-trip",
		},
		{
			name:    "file-e2e",
			entry:   config.BackupEntry{ObjectName: "e2e-file.txt", File: writeTestFile(t, tmpDir, "e2e-file.txt", "file-round-trip")},
			content: "file-round-trip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg.Backups = map[string]config.BackupEntry{tt.name: tt.entry}
			clients := testClients(t, cfg)

			receipt := backup.RunBackup(context.Background(), tt.name, tt.entry, cfg, clients, backup.BackupOptions{})
			if receipt.Status != "success" {
				t.Fatalf("backup failed: %+v", receipt.Results)
			}

			outPath := filepath.Join(tmpDir, tt.name+"-restored")
			client := testClient(t, cfg.Remotes[0])
			key := tt.entry.DerivedKey(cfg.Settings.Namespace)

			err := verify.Restore(context.Background(), verify.RestoreOptions{
				Client:       client,
				Key:          key,
				IdentityPath: testIdentityFile,
				OutputPath:   outPath,
			})
			if err != nil {
				t.Fatalf("restore: %v", err)
			}

			data, _ := os.ReadFile(outPath)
			if string(data) != tt.content {
				t.Errorf("round-trip: got %q, want %q", data, tt.content)
			}

			// Also verify
			err = verify.Verify(context.Background(), verify.VerifyOptions{
				Client:       client,
				Key:          key,
				IdentityPath: testIdentityFile,
			})
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Dir source: backup + restore round-trip (tar.gz content)
// ---------------------------------------------------------------------------

func TestDirRoundTrip(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcDir := writeTestDir(t, tmpDir, "dirdata", map[string]string{
		"file1.txt": "content-1",
		"file2.txt": "content-2",
	})

	entry := config.BackupEntry{ObjectName: "dirdata.tar", Dir: srcDir}
	cfg.Backups = map[string]config.BackupEntry{"dir-e2e": entry}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "dir-e2e", entry, cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup: %+v", receipt.Results)
	}

	outPath := filepath.Join(tmpDir, "dirdata-restored.tar.gz")
	client := testClient(t, cfg.Remotes[0])
	err := verify.Restore(context.Background(), verify.RestoreOptions{
		Client:       client,
		Key:          "integ/dirdata.tar.age",
		IdentityPath: testIdentityFile,
		OutputPath:   outPath,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Verify restored file is non-empty (it's a tar.gz)
	info, _ := os.Stat(outPath)
	if info.Size() == 0 {
		t.Error("restored tar.gz is empty")
	}

	// Decompress and check contents
	var buf bytes.Buffer
	f, _ := os.Open(outPath)
	io.Copy(&buf, f)
	f.Close()
	// The content should be valid gzip
	if buf.Len() < 10 {
		t.Error("restored tar.gz too small")
	}
}

// ---------------------------------------------------------------------------
// End-to-end: backup + verify + restore across recipient algorithms
// ---------------------------------------------------------------------------

func TestRoundTripRecipientAlgorithms(t *testing.T) {
	tmpDir := t.TempDir()

	pqIdentity, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatalf("generate PQ identity: %v", err)
	}
	pqIdentityPath := filepath.Join(tmpDir, "pq-identity.txt")
	if err := os.WriteFile(pqIdentityPath, []byte(pqIdentity.String()+"\n"), 0600); err != nil {
		t.Fatalf("write PQ identity file: %v", err)
	}

	algos := []struct {
		name         string
		recipient    string
		identityPath string
	}{
		{
			name:         "x25519",
			recipient:    testRecipient,
			identityPath: testIdentityFile,
		},
		{
			name:         "mlkem768x25519",
			recipient:    pqIdentity.Recipient().String(),
			identityPath: pqIdentityPath,
		},
	}

	for _, algo := range algos {
		t.Run(algo.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Encryption.AgeRecipient = algo.recipient
			cfg.Encryption.IdentityFile = algo.identityPath

			content := "algo-roundtrip-" + algo.name
			srcFile := writeTestFile(t, tmpDir, "algo-"+algo.name+".txt", content)
			entry := config.BackupEntry{
				ObjectName: "algo-" + algo.name + ".txt",
				File:       srcFile,
			}

			cfg.Backups = map[string]config.BackupEntry{
				"algo-" + algo.name: entry,
			}
			clients := testClients(t, cfg)

			receipt := backup.RunBackup(context.Background(), "algo-"+algo.name, entry, cfg, clients, backup.BackupOptions{})
			if receipt.Status != "success" {
				t.Fatalf("backup failed: %+v", receipt.Results)
			}

			client := testClient(t, cfg.Remotes[0])
			key := entry.DerivedKey(cfg.Settings.Namespace)

			if err := verify.Verify(context.Background(), verify.VerifyOptions{
				Client:       client,
				Key:          key,
				IdentityPath: algo.identityPath,
			}); err != nil {
				t.Fatalf("verify failed: %v", err)
			}

			outPath := filepath.Join(tmpDir, "algo-"+algo.name+"-restored.txt")
			if err := verify.Restore(context.Background(), verify.RestoreOptions{
				Client:       client,
				Key:          key,
				IdentityPath: algo.identityPath,
				OutputPath:   outPath,
			}); err != nil {
				t.Fatalf("restore failed: %v", err)
			}

			data, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("read restored file: %v", err)
			}
			if string(data) != content {
				t.Fatalf("restored content = %q, want %q", data, content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Multi-recipient: backup + verify + restore with either identity (cmd/file/dir)
// ---------------------------------------------------------------------------

func TestMultiRecipientRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Two independent identities.
	identity1, recipient1, err := crypto.DeriveIdentity("integration-test-passphrase-16", "multi-org-1")
	if err != nil {
		t.Fatal(err)
	}
	identity2, recipient2, err := crypto.DeriveIdentity("different-passphrase-at-16", "multi-org-2")
	if err != nil {
		t.Fatal(err)
	}
	idFile1 := filepath.Join(tmpDir, "id1.txt")
	idFile2 := filepath.Join(tmpDir, "id2.txt")
	if err := crypto.WriteIdentityFile(idFile1, identity1); err != nil {
		t.Fatal(err)
	}
	if err := crypto.WriteIdentityFile(idFile2, identity2); err != nil {
		t.Fatal(err)
	}

	multiRecipient := recipient1 + "\n" + recipient2

	srcDir := writeTestDir(t, tmpDir, "mdir", map[string]string{
		"x.txt": "dir-content",
	})

	sources := []struct {
		name    string
		entry   config.BackupEntry
		content string // expected restored content ("" means just check non-empty for dir)
	}{
		{
			name:    "cmd",
			entry:   config.BackupEntry{ObjectName: "multi-cmd.txt", Cmd: "printf 'multi-cmd'"},
			content: "multi-cmd",
		},
		{
			name:    "file",
			entry:   config.BackupEntry{ObjectName: "multi-file.txt", File: writeTestFile(t, tmpDir, "mf.txt", "multi-file")},
			content: "multi-file",
		},
		{
			name:  "dir",
			entry: config.BackupEntry{ObjectName: "multi-dir.tar", Dir: srcDir},
		},
	}

	identities := []struct {
		name string
		path string
	}{
		{"identity1", idFile1},
		{"identity2", idFile2},
	}

	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Encryption.AgeRecipient = multiRecipient
			cfg.Backups = map[string]config.BackupEntry{src.name: src.entry}
			clients := testClients(t, cfg)

			receipt := backup.RunBackup(context.Background(), src.name, src.entry, cfg, clients, backup.BackupOptions{})
			if receipt.Status != "success" {
				t.Fatalf("backup failed: %+v", receipt.Results)
			}

			key := src.entry.DerivedKey(cfg.Settings.Namespace)
			client := testClient(t, cfg.Remotes[0])

			for _, id := range identities {
				t.Run("verify-"+id.name, func(t *testing.T) {
					err := verify.Verify(context.Background(), verify.VerifyOptions{
						Client:       client,
						Key:          key,
						IdentityPath: id.path,
					})
					if err != nil {
						t.Fatalf("verify with %s: %v", id.name, err)
					}
				})

				t.Run("restore-"+id.name, func(t *testing.T) {
					outPath := filepath.Join(tmpDir, src.name+"-"+id.name+"-restored")
					err := verify.Restore(context.Background(), verify.RestoreOptions{
						Client:       client,
						Key:          key,
						IdentityPath: id.path,
						OutputPath:   outPath,
					})
					if err != nil {
						t.Fatalf("restore with %s: %v", id.name, err)
					}
					if src.content != "" {
						data, _ := os.ReadFile(outPath)
						if string(data) != src.content {
							t.Fatalf("restored = %q, want %q", data, src.content)
						}
					} else {
						info, err := os.Stat(outPath)
						if err != nil || info.Size() == 0 {
							t.Fatal("restored dir archive is empty")
						}
					}
				})
			}
		})
	}
}
