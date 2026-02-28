package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSecureFile_OK(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "secure.txt")
	if err := os.WriteFile(f, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ValidateSecureFile(f); err != nil {
		t.Errorf("expected ok, got: %v", err)
	}
}

func TestValidateSecureFile_Insecure(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "insecure.txt")
	if err := os.WriteFile(f, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ValidateSecureFile(f)
	if err == nil {
		t.Fatal("expected error for 0644 permissions")
	}
	if !strings.Contains(err.Error(), "insecure permissions") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSecureFile_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	err := ValidateSecureFile(link)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSecureFile_Directory(t *testing.T) {
	dir := t.TempDir()
	err := ValidateSecureFile(dir)
	if err == nil {
		t.Fatal("expected error for directory")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSecureFile_Missing(t *testing.T) {
	err := ValidateSecureFile("/nonexistent/path/to/file")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadSecureFile_OK(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "secure.txt")
	if err := os.WriteFile(f, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	data, err := ReadSecureFile(f)
	if err != nil {
		t.Fatalf("expected ok, got: %v", err)
	}
	if string(data) != "secret" {
		t.Errorf("data = %q, want %q", data, "secret")
	}
}

func TestReadSecureFile_Insecure(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "insecure.txt")
	if err := os.WriteFile(f, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSecureFile(f)
	if err == nil {
		t.Fatal("expected error for 0644 permissions")
	}
}
