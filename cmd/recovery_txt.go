package cmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
	"github.com/spf13/cobra"
)

//go:embed recovery.txt.tmpl
var recoveryTmplStr string

var recoveryTmpl = template.Must(template.New("recovery").Parse(recoveryTmplStr))

var recoveryTxtCmd = &cobra.Command{
	Use:   "recovery-txt",
	Short: "Generate recovery instructions",
	Example: `  rbackup recovery-txt --org myco
  rbackup recovery-txt --org myco -o RECOVERY.txt --upload`,
	RunE: runRecoveryTxt,
}

func init() {
	recoveryTxtCmd.Flags().String("org", "", "organization name (omit for template with placeholders)")
	recoveryTxtCmd.Flags().StringP("output", "o", "", "output file path (default: stdout)")
	recoveryTxtCmd.Flags().Bool("upload", false, "upload generated recovery text to all configured remotes (best effort)")
	recoveryTxtCmd.Flags().String("upload-key", "", "object key for --upload (default: <namespace>/RECOVERY.txt)")
	rootCmd.AddCommand(recoveryTxtCmd)
}

func runRecoveryTxt(cmd *cobra.Command, args []string) error {
	org, _ := cmd.Flags().GetString("org")
	output, _ := cmd.Flags().GetString("output")
	upload, _ := cmd.Flags().GetBool("upload")
	uploadKey, _ := cmd.Flags().GetString("upload-key")

	text := recoveryText(org)

	if output != "" {
		if err := os.WriteFile(output, []byte(text), 0644); err != nil {
			return fmt.Errorf("writing recovery file: %w", err)
		}
	} else {
		fmt.Print(text)
	}

	if upload {
		uploadRecoveryText(cmd, text, org, uploadKey)
	}

	return nil
}

func recoveryText(org string) string {
	normalizedOrg := "<your-org>"
	if org != "" {
		normalizedOrg = crypto.NormalizeOrg(org)
	}
	var buf bytes.Buffer
	_ = recoveryTmpl.Execute(&buf, struct {
		Org          string
		ArgonTime    uint32
		ArgonMemory  uint32
		ArgonMemoryMB uint32
		ArgonThreads uint8
		ArgonKeyLen  uint32
	}{
		Org:          normalizedOrg,
		ArgonTime:    crypto.ArgonTime,
		ArgonMemory:  crypto.ArgonMemory,
		ArgonMemoryMB: crypto.ArgonMemory / 1024,
		ArgonThreads: crypto.ArgonThreads,
		ArgonKeyLen:  crypto.ArgonKeyLen,
	})
	return buf.String()
}

func uploadRecoveryText(cmd *cobra.Command, text, org, uploadKey string) {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: recovery-txt upload skipped: loading config: %v\n", err)
		return
	}

	key := strings.TrimSpace(uploadKey)
	if key == "" {
		key = cfg.Settings.Namespace + "/RECOVERY.txt"
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "warning: recovery-txt upload skipped: empty upload key")
		return
	}

	if len(cfg.Remotes) == 0 {
		fmt.Fprintln(os.Stderr, "warning: recovery-txt upload skipped: no remotes configured")
		return
	}

	metadata := map[string]string{}

	for _, remote := range cfg.Remotes {
		resolved := remote
		if err := config.ResolveRemoteSecrets(&resolved); err != nil {
			fmt.Fprintf(os.Stderr, "warning: recovery-txt upload skipped for remote %q: %v\n", remote.Name, err)
			continue
		}

		client, err := storage.NewClientWithOptions(cmd.Context(), resolved, storage.ClientOptions{
			AttemptTimeout: cfg.Settings.RemoteTimeoutD,
			MaxAttempts:    cfg.Settings.RemoteRetriesN + 1,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: recovery-txt upload skipped for remote %q: creating client: %v\n", resolved.Name, err)
			continue
		}

		versionID, err := client.UploadBytes(cmd.Context(), key, strings.NewReader(text), metadata)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: recovery-txt upload failed for remote %q: %v\n", resolved.Name, err)
			continue
		}

		if versionID == "" {
			fmt.Fprintf(os.Stderr, "uploaded recovery-txt: remote=%s key=%s\n", resolved.Name, key)
		} else {
			fmt.Fprintf(os.Stderr, "uploaded recovery-txt: remote=%s key=%s version_id=%s\n", resolved.Name, key, versionID)
		}
	}
}
