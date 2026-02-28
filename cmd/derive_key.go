package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/kolontsov/rbackup/internal/crypto"
	"golang.org/x/term"
)

var deriveKeyCmd = &cobra.Command{
	Use:   "derive-key",
	Short: "Derive age identity secret material from a passphrase",
	Example: `  rbackup derive-key --org myco -o key.txt
  rbackup derive-key --org myco --validate --recipient age1...`,
	RunE: runDeriveKey,
}

func init() {
	deriveKeyCmd.Flags().String("org", "", "organization name (required)")
	deriveKeyCmd.Flags().StringP("output", "o", "", "output identity file path (required unless --validate)")
	deriveKeyCmd.Flags().Bool("validate", false, "validate mode: compare derived recipient without writing identity file")
	deriveKeyCmd.Flags().String("recipient", "", "expected age recipient for --validate")
	deriveKeyCmd.Flags().Bool("pq", false, "derive ML-KEM-768/X25519 identity secret material (age1pq... recipient)")
	_ = deriveKeyCmd.MarkFlagRequired("org")
	rootCmd.AddCommand(deriveKeyCmd)
}

func runDeriveKey(cmd *cobra.Command, args []string) error {
	org, _ := cmd.Flags().GetString("org")
	output, _ := cmd.Flags().GetString("output")
	validate, _ := cmd.Flags().GetBool("validate")
	recipient, _ := cmd.Flags().GetString("recipient")
	pq, _ := cmd.Flags().GetBool("pq")

	if validate {
		if recipient == "" {
			return fmt.Errorf("--validate requires --recipient")
		}
		if output != "" {
			return fmt.Errorf("--validate does not allow -o")
		}
		if err := crypto.ValidateRecipientForMode(recipient, pq); err != nil {
			if pq {
				return fmt.Errorf("invalid --recipient for --pq: %w", err)
			}
			return fmt.Errorf("invalid --recipient: %w", err)
		}
	} else {
		if output == "" {
			return fmt.Errorf("write mode requires -o")
		}
	}

	passphrase, err := readPassphrase(validate)
	if err != nil {
		return err
	}

	if len(passphrase) < crypto.MinPassphraseLen {
		return fmt.Errorf("passphrase must be at least %d characters", crypto.MinPassphraseLen)
	}

	var (
		identitySecret   string
		derivedRecipient string
	)
	if pq {
		identitySecret, derivedRecipient, err = crypto.DeriveIdentityPQ(string(passphrase), org)
	} else {
		identitySecret, derivedRecipient, err = crypto.DeriveIdentity(string(passphrase), org)
	}
	if err != nil {
		return fmt.Errorf("deriving key: %w", err)
	}

	if validate {
		if derivedRecipient != recipient {
			fmt.Fprintln(os.Stderr, "Recipient mismatch")
			return fmt.Errorf("derived recipient does not match")
		}
		fmt.Fprintln(os.Stderr, "Recipient matches")
		return nil
	}

	if err := crypto.WriteIdentityFileWithMetadata(output, identitySecret, org); err != nil {
		return fmt.Errorf("writing identity file: %w", err)
	}
	absPath, err := filepath.Abs(output)
	if err != nil {
		absPath = output
	}
	fmt.Printf("Identity saved to %s\n", absPath)
	fmt.Printf("Public key: %s\n", derivedRecipient)
	return nil
}

func readPassphrase(skipConfirm bool) ([]byte, error) {
	fmt.Fprint(os.Stderr, "Enter passphrase: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("reading passphrase: %w", err)
	}

	if !skipConfirm {
		fmt.Fprint(os.Stderr, "Confirm passphrase: ")
		confirm, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return nil, fmt.Errorf("reading passphrase confirmation: %w", err)
		}
		if string(pass) != string(confirm) {
			return nil, fmt.Errorf("passphrases do not match")
		}
	}

	return pass, nil
}
