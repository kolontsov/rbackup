package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/notify"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Run a backup",
	Long:  "Run a backup: backup @<name> for a single entry, or backup --all for all entries.",
	Example: `  rbackup backup @db
  rbackup backup --all`,
	RunE: runBackup,
}

func init() {
	backupCmd.Flags().Bool("all", false, "backup all configured entries")
	backupCmd.Flags().String("seal-passphrase-file", "", "file containing seal passphrase for sealed entries")
	rootCmd.AddCommand(backupCmd)
}

func runBackup(cmd *cobra.Command, args []string) error {
	allFlag, _ := cmd.Flags().GetBool("all")
	sealPassFile, _ := cmd.Flags().GetString("seal-passphrase-file")

	// Validate exactly one mode
	hasEntry := len(args) == 1 && strings.HasPrefix(args[0], "@")
	if !allFlag && !hasEntry {
		return fmt.Errorf("exactly one target required: backup @<name> or backup --all")
	}
	if allFlag && hasEntry {
		return fmt.Errorf("cannot combine @<name> with --all")
	}

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return err
	}

	var (
		entryName  string
		entry      config.BackupEntry
		remoteRefs []string
	)
	if !allFlag {
		entryName = strings.TrimPrefix(args[0], "@")
		var ok bool
		entry, ok = cfg.Backups[entryName]
		if !ok {
			return fmt.Errorf("backup entry %q not found", entryName)
		}
		remoteRefs = entry.ResolvedRemotes(cfg.Defaults.Remotes)
	} else {
		remoteRefs = allModeRemoteRefs(cfg)
	}

	if err := config.ResolveSelectedRemoteSecrets(cfg, remoteRefs); err != nil {
		return err
	}
	config.ResolveNotificationSecrets(&cfg.Notifications)

	ctx := cmd.Context()
	sigCtx, sigCancel := backup.SignalContext(ctx)
	defer sigCancel()

	// Acquire lock
	lock, err := backup.AcquireLock(cfg.Settings.LockDir, cfg.Settings.Namespace, cfg.Settings.LockWaitD)
	if err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	lockReleased := false
	releaseLock := func() {
		if lockReleased {
			return
		}
		if err := lock.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: releasing lock: %v\n", err)
		}
		lockReleased = true
	}
	defer releaseLock()

	clients, err := backup.BuildClientsForRemotes(sigCtx, cfg, remoteRefs)
	if err != nil {
		return err
	}

	// Resolve seal passphrase if any entries need it
	opts := backup.BackupOptions{}
	if anySealRequired(cfg, allFlag, entry) {
		opts.SealPassphrase, err = resolveSealPassphraseInteractive(sealPassFile)
		if err != nil {
			return err
		}
	}

	var receipts []backup.Receipt

	if allFlag {
		receipts = backup.RunAll(sigCtx, cfg, clients, opts)
	} else {
		receipt := backup.RunBackup(sigCtx, entryName, entry, cfg, clients, opts)
		receipts = []backup.Receipt{receipt}
	}
	if allFlag && len(receipts) == 0 {
		fmt.Fprintln(os.Stderr, "No work done: no backup targets configured")
	}

	// Print receipts to stdout
	if err := backup.PrintReceipts(receipts); err != nil {
		return fmt.Errorf("writing receipts: %w", err)
	}

	// Determine overall status
	overallStatus := backup.StatusSuccess
	var failedIDs []string
	total, succeeded, failed, skipped := 0, 0, 0, 0
	for _, r := range receipts {
		total++
		switch r.Status {
		case backup.StatusSuccess:
			succeeded++
		case backup.StatusFailure:
			failed++
			failedIDs = append(failedIDs, r.BackupID)
			overallStatus = backup.StatusFailure
		case backup.StatusSkipped:
			skipped++
		}
	}

	// Release the backup lock before best-effort notification delivery.
	releaseLock()

	// Send notification
	notify.Send(cfg.Notifications, notify.Payload{
		Status:    overallStatus,
		Total:     total,
		Succeeded: succeeded,
		Failed:    failed,
		Skipped:   skipped,
		FailedIDs: failedIDs,
		Receipts:  receipts,
	})

	if overallStatus == backup.StatusFailure {
		return fmt.Errorf("backup completed with failures")
	}
	return nil
}

func allModeRemoteRefs(cfg *config.Config) []string {
	refsSet := make(map[string]struct{})
	for _, entry := range cfg.Backups {
		if entry.ManualOnly {
			continue
		}
		for _, remoteName := range entry.ResolvedRemotes(cfg.Defaults.Remotes) {
			refsSet[remoteName] = struct{}{}
		}
	}

	refs := make([]string, 0, len(refsSet))
	for name := range refsSet {
		refs = append(refs, name)
	}
	sort.Strings(refs)
	return refs
}

func anySealRequired(cfg *config.Config, allFlag bool, entry config.BackupEntry) bool {
	if allFlag {
		for _, e := range cfg.Backups {
			if e.Seal && !e.ManualOnly {
				return true
			}
		}
		return false
	}
	return entry.Seal
}
