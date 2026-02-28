package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
	"github.com/kolontsov/rbackup/internal/verify"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify backup integrity without restoring",
	Long: `Download and decrypt a backup to /dev/null (nothing written to disk).

Checks performed:
  - age decryption succeeds with the given identity
  - ciphertext SHA-256 matches stored hash (if present)
  - signature is valid against allowed signers (if signing configured)`,
	Example: `  rbackup verify --remote minio --key ns/db/2025-01-15.sql.age
  rbackup verify --remote s3 --key ns/db/2025-01-15.sql.age --version-id abc123`,
	RunE: runVerify,
}

func init() {
	verifyCmd.Flags().String("remote", "", "remote name (required)")
	verifyCmd.Flags().String("key", "", "S3 object key (required)")
	verifyCmd.Flags().String("version-id", "", "S3 version ID (optional)")
	verifyCmd.Flags().String("identity", "", "identity file path (overrides config)")
	verifyCmd.Flags().String("seal-passphrase-file", "", "file containing seal passphrase")
	_ = verifyCmd.MarkFlagRequired("remote")
	_ = verifyCmd.MarkFlagRequired("key")
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(cmd *cobra.Command, args []string) error {
	remoteName, _ := cmd.Flags().GetString("remote")
	key, _ := cmd.Flags().GetString("key")
	versionID, _ := cmd.Flags().GetString("version-id")
	identityPath, _ := cmd.Flags().GetString("identity")
	sealPassFile, _ := cmd.Flags().GetString("seal-passphrase-file")

	key, err := verify.ValidateKey(key)
	if err != nil {
		return err
	}

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return err
	}

	if identityPath == "" {
		identityPath = cfg.Encryption.IdentityFile
	}
	if identityPath == "" {
		return fmt.Errorf("identity file required (--identity or encryption.identity_file in config)")
	}

	remote, err := config.FindRemote(cfg, remoteName)
	if err != nil {
		return err
	}
	if err := config.ResolveRemoteSecrets(remote); err != nil {
		return err
	}

	client, err := storage.NewClientWithOptions(cmd.Context(), *remote, storage.ClientOptions{
		AttemptTimeout: cfg.Settings.RemoteTimeoutD,
		MaxAttempts:    cfg.Settings.RemoteRetriesN + 1,
	})
	if err != nil {
		return err
	}

	var sealPassphrase string
	if verify.IsSealed(key) {
		var err error
		sealPassphrase, err = resolveSealPassphraseInteractive(sealPassFile)
		if err != nil {
			return err
		}
	}

	allowedSigners, err := crypto.ResolveAllowedSigners(
		cfg.Signing.AllowedSigners, cfg.Signing.AllowedSignersFile, cfg.Signing.HostKey)
	if err != nil {
		return err
	}

	if err := verify.Verify(cmd.Context(), verify.VerifyOptions{
		Client:         client,
		Key:            key,
		VersionID:      versionID,
		IdentityPath:   identityPath,
		SealPassphrase: sealPassphrase,
		AllowedSigners: allowedSigners,
	}); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Verification successful")
	return nil
}
