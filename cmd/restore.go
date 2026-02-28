package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
	"github.com/kolontsov/rbackup/internal/verify"
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Download, verify, and decrypt a backup to a local file",
	Long: `Download, verify, and decrypt a backup to a local file.

Performs the same integrity checks as "verify" (hash, signature, decryption),
then writes the decrypted output atomically to the specified path.`,
	Example: `  rbackup restore --remote minio --key ns/db/2025-01-15.sql.age -o db.sql
  rbackup restore --remote s3 --key ns/db/2025-01-15.sql.age -o db.sql --force`,
	RunE: runRestore,
}

func init() {
	restoreCmd.Flags().String("remote", "", "remote name (required)")
	restoreCmd.Flags().String("key", "", "S3 object key (required)")
	restoreCmd.Flags().StringP("output", "o", "", "output file path (required)")
	restoreCmd.Flags().String("version-id", "", "S3 version ID (optional, defaults to latest)")
	restoreCmd.Flags().String("identity", "", "identity file path (overrides config)")
	restoreCmd.Flags().String("seal-passphrase-file", "", "file containing seal passphrase")
	restoreCmd.Flags().Bool("force", false, "overwrite existing output file")
	_ = restoreCmd.MarkFlagRequired("remote")
	_ = restoreCmd.MarkFlagRequired("key")
	_ = restoreCmd.MarkFlagRequired("output")
	rootCmd.AddCommand(restoreCmd)
}

func runRestore(cmd *cobra.Command, args []string) error {
	remoteName, _ := cmd.Flags().GetString("remote")
	key, _ := cmd.Flags().GetString("key")
	output, _ := cmd.Flags().GetString("output")
	versionID, _ := cmd.Flags().GetString("version-id")
	identityPath, _ := cmd.Flags().GetString("identity")
	sealPassFile, _ := cmd.Flags().GetString("seal-passphrase-file")
	force, _ := cmd.Flags().GetBool("force")

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

	// Handle seal passphrase
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

	return verify.Restore(cmd.Context(), verify.RestoreOptions{
		Client:         client,
		Key:            key,
		VersionID:      versionID,
		IdentityPath:   identityPath,
		SealPassphrase: sealPassphrase,
		AllowedSigners: allowedSigners,
		OutputPath:     output,
		Force:          force,
	})
}
