package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kolontsov/rbackup/internal/storage"
)

// ValidateKey checks the restore/verify --key value per spec rules.
func ValidateKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("--key is required and must not be empty")
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("--key must not start with '/'")
	}
	if strings.Contains(key, "//") {
		return "", fmt.Errorf("--key must not contain empty path components ('//')")
	}
	return key, nil
}

// IsSealed returns true if the key ends with .age2.
func IsSealed(key string) bool {
	return strings.HasSuffix(key, ".age2")
}

// RestoreOptions configures a restore operation.
type RestoreOptions struct {
	Client         *storage.Client
	Key            string
	VersionID      string
	IdentityPath   string
	SealPassphrase string
	AllowedSigners []string
	OutputPath     string
	Force          bool
}

// Restore downloads and decrypts a backup to a local file.
func Restore(ctx context.Context, opts RestoreOptions) error {
	if opts.IdentityPath == "" {
		return fmt.Errorf("identity file is required")
	}

	// Check output path
	if !opts.Force {
		if _, err := os.Stat(opts.OutputPath); err == nil {
			return fmt.Errorf("output file %q already exists (use --force to overwrite)", opts.OutputPath)
		}
	}

	// Create temp file in same directory for atomic rename
	dir := filepath.Dir(opts.OutputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".rbackup-restore-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpClosed := false
	defer func() {
		if !tmpClosed {
			_ = tmpFile.Close()
		}
		_ = os.Remove(tmpPath) // no-op if rename succeeded
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		return fmt.Errorf("setting temp file permissions: %w", err)
	}

	if err := streamDecrypt(ctx, opts.Client, opts.Key, opts.VersionID, opts.IdentityPath, opts.SealPassphrase, "decrypting", opts.AllowedSigners, tmpFile); err != nil {
		return err
	}

	if err := tmpFile.Close(); err != nil {
		tmpClosed = true
		return fmt.Errorf("closing temp file: %w", err)
	}
	tmpClosed = true

	// Atomic rename
	if err := os.Rename(tmpPath, opts.OutputPath); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}
