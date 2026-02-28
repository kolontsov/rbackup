//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/list"
)

// ---------------------------------------------------------------------------
// List: basic — backup a file, then list it
// ---------------------------------------------------------------------------

func TestListBasic(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "list-basic.txt", "list basic content")

	cfg.Backups["list-basic"] = config.BackupEntry{
		ObjectName: "list-basic.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "list-basic", cfg.Backups["list-basic"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Fatalf("backup failed: %+v", receipt.Results)
	}

	entries, err := list.List(context.Background(), cfg, clients, "", "", list.Options{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) < 1 {
		t.Fatal("expected at least 1 entry")
	}

	found := false
	for _, e := range entries {
		if e.Backup == "list-basic" {
			found = true
			if e.Key != "integ/list-basic.txt.age" {
				t.Errorf("key = %q", e.Key)
			}
			if e.SizeBytes == 0 {
				t.Error("expected non-zero size")
			}
			if e.CreatedAt.IsZero() {
				t.Error("expected non-zero created_at")
			}
		}
	}
	if !found {
		t.Error("list-basic entry not found in results")
	}
}

// ---------------------------------------------------------------------------
// List: filter by entry name
// ---------------------------------------------------------------------------

func TestListFilterByEntry(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()

	cfg.Backups["list-a"] = config.BackupEntry{
		ObjectName: "list-a.txt",
		File:       writeTestFile(t, tmpDir, "a.txt", "aaa"),
	}
	cfg.Backups["list-b"] = config.BackupEntry{
		ObjectName: "list-b.txt",
		File:       writeTestFile(t, tmpDir, "b.txt", "bbb"),
	}
	clients := testClients(t, cfg)

	backup.RunBackup(context.Background(), "list-a", cfg.Backups["list-a"], cfg, clients, backup.BackupOptions{})
	backup.RunBackup(context.Background(), "list-b", cfg.Backups["list-b"], cfg, clients, backup.BackupOptions{})

	entries, err := list.List(context.Background(), cfg, clients, "list-a", "", list.Options{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}

	for _, e := range entries {
		if e.Backup != "list-a" {
			t.Errorf("unexpected backup %q in filtered results", e.Backup)
		}
	}
	if len(entries) == 0 {
		t.Error("expected entries for list-a")
	}
}

// ---------------------------------------------------------------------------
// List: filter by remote
// ---------------------------------------------------------------------------

func TestListFilterByRemote(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()

	cfg.Backups["list-remote"] = config.BackupEntry{
		ObjectName: "list-remote.txt",
		File:       writeTestFile(t, tmpDir, "remote.txt", "remote content"),
	}
	clients := testClients(t, cfg)

	backup.RunBackup(context.Background(), "list-remote", cfg.Backups["list-remote"], cfg, clients, backup.BackupOptions{})

	entries, err := list.List(context.Background(), cfg, clients, "", "minio1", list.Options{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}

	for _, e := range entries {
		if e.Remote != "minio1" {
			t.Errorf("unexpected remote %q in filtered results", e.Remote)
		}
	}
	if len(entries) == 0 {
		t.Error("expected entries for minio1")
	}
}

// ---------------------------------------------------------------------------
// List: max-age filter
// ---------------------------------------------------------------------------

func TestListMaxAge(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()

	cfg.Backups["list-age"] = config.BackupEntry{
		ObjectName: "list-age.txt",
		File:       writeTestFile(t, tmpDir, "age.txt", "age content"),
	}
	clients := testClients(t, cfg)

	backup.RunBackup(context.Background(), "list-age", cfg.Backups["list-age"], cfg, clients, backup.BackupOptions{})

	// Should appear with a generous max-age
	entries, err := list.List(context.Background(), cfg, clients, "list-age", "", list.Options{MaxAge: time.Hour})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected entries with 1h max-age")
	}

	// Should not appear with a tiny max-age
	entries, err = list.List(context.Background(), cfg, clients, "list-age", "", list.Options{MaxAge: time.Millisecond})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries with 1ms max-age, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// List: all versions
// ---------------------------------------------------------------------------

func TestListAllVersions(t *testing.T) {
	if err := enableVersioning(minio1Endpoint, minio1Bucket); err != nil {
		t.Fatalf("enabling versioning: %v", err)
	}

	cfg := testConfig(t)
	cfg.Defaults.Remotes = []string{"minio1"}
	tmpDir := t.TempDir()

	cfg.Backups["list-ver"] = config.BackupEntry{
		ObjectName: "list-ver.txt",
		File:       writeTestFile(t, tmpDir, "ver.txt", "version 1"),
	}
	clients := testClients(t, cfg)

	// Upload twice to create two versions
	r1 := backup.RunBackup(context.Background(), "list-ver", cfg.Backups["list-ver"], cfg, clients, backup.BackupOptions{})
	if r1.Status != "success" {
		t.Fatalf("first backup failed: %+v", r1.Results)
	}

	writeTestFile(t, tmpDir, "ver.txt", "version 2")
	r2 := backup.RunBackup(context.Background(), "list-ver", cfg.Backups["list-ver"], cfg, clients, backup.BackupOptions{})
	if r2.Status != "success" {
		t.Fatalf("second backup failed: %+v", r2.Results)
	}

	entries, err := list.List(context.Background(), cfg, clients, "list-ver", "minio1", list.Options{AllVersions: true})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("expected at least 2 versions, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// List: no backups → empty result
// ---------------------------------------------------------------------------

func TestListNoBackups(t *testing.T) {
	cfg := testConfig(t)
	cfg.Backups["nonexistent"] = config.BackupEntry{
		ObjectName: "does-not-exist.txt",
		Cmd:        "echo nope",
	}
	clients := testClients(t, cfg)

	// Don't actually run backup — just list
	entries, err := list.List(context.Background(), cfg, clients, "nonexistent", "", list.Options{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
