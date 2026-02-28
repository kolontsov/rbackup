package verify

import (
	"context"
	"github.com/kolontsov/rbackup/internal/storage"
	"io"
)

// VerifyOptions configures a verify operation.
type VerifyOptions struct {
	Client         *storage.Client
	Key            string
	VersionID      string
	IdentityPath   string
	SealPassphrase string
	AllowedSigners []string
}

// Verify downloads and decrypts a backup in a streaming pipeline to /dev/null.
// No temporary files are created.
func Verify(ctx context.Context, opts VerifyOptions) error {
	return streamDecrypt(ctx, opts.Client, opts.Key, opts.VersionID, opts.IdentityPath, opts.SealPassphrase, "verifying", opts.AllowedSigners, io.Discard)
}
