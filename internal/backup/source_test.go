package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kolontsov/rbackup/internal/config"
)

func TestRunSourceCmd(t *testing.T) {
	var buf bytes.Buffer
	entry := config.BackupEntry{Cmd: "echo hello"}
	settings := config.Settings{CmdTimeoutD: 10 * time.Second}

	_, err := RunSource(context.Background(), entry, settings, &buf)
	if err != nil {
		t.Fatalf("RunSource cmd: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "hello" {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunSourceCmdNonZeroExit(t *testing.T) {
	var buf bytes.Buffer
	entry := config.BackupEntry{Cmd: "exit 1"}
	settings := config.Settings{CmdTimeoutD: 10 * time.Second}

	_, err := RunSource(context.Background(), entry, settings, &buf)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}

func TestRunSourceCmdTimeout(t *testing.T) {
	var buf bytes.Buffer
	entry := config.BackupEntry{Cmd: "sleep 10"}
	settings := config.Settings{CmdTimeoutD: 100 * time.Millisecond}

	_, err := RunSource(context.Background(), entry, settings, &buf)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRunSourceFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(f, []byte("file contents"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	entry := config.BackupEntry{File: f}
	n, err := RunSource(context.Background(), entry, config.Settings{}, &buf)
	if err != nil {
		t.Fatalf("RunSource file: %v", err)
	}
	if n != 13 {
		t.Errorf("bytes = %d", n)
	}
	if buf.String() != "file contents" {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunSourceDir(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "data")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("aaa"), 0644)
	os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("bbb"), 0644)

	var buf bytes.Buffer
	entry := config.BackupEntry{Dir: subDir}
	n, err := RunSource(context.Background(), entry, config.Settings{}, &buf)
	if err != nil {
		t.Fatalf("RunSource dir: %v", err)
	}
	if n == 0 {
		t.Error("expected non-zero bytes for tar.gz")
	}
	// Should produce a valid tar.gz
	if buf.Len() == 0 {
		t.Error("empty tar.gz output")
	}
}

func TestRunSourceDirPreservesSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "data")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "target.txt"), []byte("target"), 0644)
	linkPath := filepath.Join(subDir, "link.txt")
	if err := os.Symlink("target.txt", linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	var buf bytes.Buffer
	entry := config.BackupEntry{Dir: subDir}
	if _, err := RunSource(context.Background(), entry, config.Settings{}, &buf); err != nil {
		t.Fatalf("RunSource dir with symlink: %v", err)
	}

	gzr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("creating gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	wantName := filepath.Join(filepath.Base(subDir), "link.txt")
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar: %v", err)
		}
		if hdr.Name != wantName {
			continue
		}
		found = true
		if hdr.Typeflag != tar.TypeSymlink {
			t.Fatalf("header type = %v, want symlink", hdr.Typeflag)
		}
		if hdr.Linkname != "target.txt" {
			t.Fatalf("symlink target = %q, want %q", hdr.Linkname, "target.txt")
		}
		break
	}
	if !found {
		t.Fatalf("symlink entry %q not found in tar", wantName)
	}
}

func TestRunSourceZeroByteFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "empty.txt")
	os.WriteFile(f, []byte{}, 0644)

	var buf bytes.Buffer
	entry := config.BackupEntry{File: f}
	n, err := RunSource(context.Background(), entry, config.Settings{}, &buf)
	if err != nil {
		t.Fatalf("RunSource empty file: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes, got %d", n)
	}
}

func TestLockAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	lock, err := AcquireLock(dir, "test", 0)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer lock.Release()

	// Second lock should fail immediately
	_, err = AcquireLock(dir, "test", 0)
	if err == nil {
		t.Fatal("expected lock contention error")
	}
}

func TestLockDifferentNamespaces(t *testing.T) {
	dir := t.TempDir()
	lock1, err := AcquireLock(dir, "ns1", 0)
	if err != nil {
		t.Fatalf("AcquireLock ns1: %v", err)
	}
	defer lock1.Release()

	lock2, err := AcquireLock(dir, "ns2", 0)
	if err != nil {
		t.Fatalf("AcquireLock ns2: %v", err)
	}
	defer lock2.Release()
}
