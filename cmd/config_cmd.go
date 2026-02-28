package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Config operations",
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate config (offline, no secrets)",
	RunE:  runConfigValidate,
}

var configTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test remote connectivity",
	Example: `  rbackup config test --read
  rbackup config test --write
  rbackup config test --read --write`,
	RunE: runConfigTest,
}

func init() {
	configTestCmd.Flags().Bool("read", false, "test HeadBucket for each remote (bucket reachability only)")
	configTestCmd.Flags().Bool("write", false, "upload probe object to each remote")
	configTestCmd.Flags().String("write-key", "", "override probe object key for --write")

	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configTestCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return err
	}

	if warning := runtimeDirWarning("settings.lock_dir", cfg.Settings.LockDir); warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}
	if warning := runtimeDirWarning("settings.spool_dir", cfg.Settings.SpoolDir); warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}

	fmt.Fprintln(os.Stderr, "Config is valid")
	return nil
}

func runtimeDirWarning(field, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Sprintf("warning: %s is empty; backups will fail until it is set", field)
	}

	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Sprintf("warning: %s path %q exists but is not a directory; backups may fail", field, path)
		}
		return ""
	}
	if os.IsNotExist(err) {
		return fmt.Sprintf("warning: %s path %q does not exist; it will be created during backup operations", field, path)
	}
	return fmt.Sprintf("warning: could not stat %s path %q: %v", field, path, err)
}

func runConfigTest(cmd *cobra.Command, args []string) error {
	readFlag, _ := cmd.Flags().GetBool("read")
	writeFlag, _ := cmd.Flags().GetBool("write")
	writeKey, _ := cmd.Flags().GetString("write-key")

	if !readFlag && !writeFlag {
		return fmt.Errorf("at least one mode flag required: --read or --write")
	}

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return err
	}
	if err := config.ResolveSecrets(cfg); err != nil {
		return err
	}

	clients, err := backup.BuildClients(cmd.Context(), cfg)
	if err != nil {
		return err
	}

	hasError := false

	if readFlag {
		for _, remote := range cfg.Remotes {
			client := clients[remote.Name]
			fmt.Fprintf(os.Stderr, "testing bucket reachability (HeadBucket): %s ... ", remote.Name)
			if err := client.HeadBucket(cmd.Context()); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
				hasError = true
			} else {
				fmt.Fprintln(os.Stderr, "OK")
			}
		}
	}

	if writeFlag {
		probeKey := writeKey
		if probeKey == "" {
			hostname, _ := os.Hostname()
			randomBytes := make([]byte, 8)
			if _, err := rand.Read(randomBytes); err != nil {
				return fmt.Errorf("generating probe key suffix: %w", err)
			}
			probeKey = fmt.Sprintf(".rbackup/test/%s/%s-%s",
				cfg.Settings.Namespace, hostname, hex.EncodeToString(randomBytes))
		}

		for _, remote := range cfg.Remotes {
			client := clients[remote.Name]
			fmt.Fprintf(os.Stderr, "testing write access: %s ... ", remote.Name)
			_, err := client.UploadBytes(cmd.Context(), probeKey,
				strings.NewReader("rbackup connectivity test"),
				map[string]string{"x-rbackup-test": "true"})
			if err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
				hasError = true
			} else {
				fmt.Fprintf(os.Stderr, "OK (key: %s)\n", probeKey)
			}
		}
	}

	if hasError {
		return fmt.Errorf("one or more tests failed")
	}
	return nil
}
