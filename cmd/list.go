package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/list"
)

var listCmd = &cobra.Command{
	Use:   "list [@name]",
	Short: "List backups on remotes",
	Long: `Query remotes for existing backup objects.

Outputs JSONL by default (one JSON object per line), suitable for scripting.
Use --pretty for a human-readable table, or --quiet for exit-code-only mode.

Exit code 1 when no results pass filters, enabling alerting:
  rbackup list @postgres --max-age 25h || alert`,
	Example: `  rbackup list
  rbackup list @postgres
  rbackup list @postgres --remote s3-primary --max-age 25h
  rbackup list --pretty
  rbackup list @postgres --all-versions
  rbackup list @postgres --max-age 1d --quiet`,
	RunE: runList,
}

func init() {
	listCmd.Flags().String("remote", "", "filter to a single remote")
	listCmd.Flags().String("max-age", "", "only show objects newer than this duration (e.g. 25h, 3d)")
	listCmd.Flags().Bool("all-versions", false, "show all S3 versions, not just latest")
	listCmd.Flags().Bool("quiet", false, "no output, exit code only")
	listCmd.Flags().Bool("pretty", false, "human-readable table output")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	remoteFlag, _ := cmd.Flags().GetString("remote")
	maxAgeStr, _ := cmd.Flags().GetString("max-age")
	allVersions, _ := cmd.Flags().GetBool("all-versions")
	quiet, _ := cmd.Flags().GetBool("quiet")
	pretty, _ := cmd.Flags().GetBool("pretty")

	var entryFilter string
	if len(args) == 1 && strings.HasPrefix(args[0], "@") {
		entryFilter = strings.TrimPrefix(args[0], "@")
	} else if len(args) > 0 {
		return fmt.Errorf("unexpected arguments; use @<name> to filter by backup entry")
	}

	var maxAge = list.Options{}
	if maxAgeStr != "" {
		d, err := parseDuration(maxAgeStr)
		if err != nil {
			return fmt.Errorf("invalid --max-age: %w", err)
		}
		maxAge.MaxAge = d
	}
	maxAge.AllVersions = allVersions

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return err
	}

	if entryFilter != "" {
		if _, ok := cfg.Backups[entryFilter]; !ok {
			return fmt.Errorf("backup entry %q not found", entryFilter)
		}
	}

	remoteRefs := collectRemoteRefs(cfg, entryFilter, remoteFlag)
	if err := config.ResolveSelectedRemoteSecrets(cfg, remoteRefs); err != nil {
		return err
	}

	ctx := cmd.Context()
	clients, err := backup.BuildClientsForRemotes(ctx, cfg, remoteRefs)
	if err != nil {
		return err
	}

	entries, err := list.List(ctx, cfg, clients, entryFilter, remoteFlag, maxAge)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		if !quiet {
			fmt.Fprintln(os.Stderr, "no backups found")
		}
		os.Exit(1)
	}

	if quiet {
		return nil
	}

	if pretty {
		return printPretty(os.Stdout, entries)
	}
	return printJSONL(os.Stdout, entries)
}

func collectRemoteRefs(cfg *config.Config, entryFilter, remoteFilter string) []string {
	if remoteFilter != "" {
		return []string{remoteFilter}
	}

	seen := make(map[string]struct{})
	var refs []string
	for name, entry := range cfg.Backups {
		if entryFilter != "" && name != entryFilter {
			continue
		}
		for _, r := range entry.ResolvedRemotes(cfg.Defaults.Remotes) {
			if _, ok := seen[r]; !ok {
				seen[r] = struct{}{}
				refs = append(refs, r)
			}
		}
	}
	return refs
}

func printJSONL(w io.Writer, entries []list.Entry) error {
	enc := json.NewEncoder(w)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

func printPretty(w io.Writer, entries []list.Entry) error {
	hasVersion := false
	for _, e := range entries {
		if e.VersionID != "" {
			hasVersion = true
			break
		}
	}

	tw := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
	if hasVersion {
		fmt.Fprintln(tw, "BACKUP\tREMOTE\tCREATED AT\tSIZE\tAGE\tVERSION")
	} else {
		fmt.Fprintln(tw, "BACKUP\tREMOTE\tCREATED AT\tSIZE\tAGE")
	}

	for _, e := range entries {
		created := e.CreatedAt.UTC().Format("2006-01-02 15:04 UTC")
		if e.CreatedAt.IsZero() {
			created = "unknown"
		}
		if hasVersion {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				e.Backup, e.Remote, created, formatSize(e.SizeBytes), formatAge(e.CreatedAt), truncateVersion(e.VersionID))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				e.Backup, e.Remote, created, formatSize(e.SizeBytes), formatAge(e.CreatedAt))
		}
	}

	return tw.Flush()
}
