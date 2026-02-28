package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/kolontsov/rbackup/internal/fsutil"
	"golang.org/x/term"
)

// resolveSealPassphraseInteractive resolves a seal passphrase from a file,
// TTY prompt, or returns an error. Used by restore and verify commands.
func resolveSealPassphraseInteractive(sealPassFile string) (string, error) {
	if sealPassFile != "" {
		data, err := fsutil.ReadSecureFile(sealPassFile)
		if err != nil {
			return "", fmt.Errorf("reading seal passphrase file: %w", err)
		}
		return strings.TrimRight(string(data), "\n\r"), nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Enter seal passphrase: ")
		pass, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("reading passphrase: %w", err)
		}
		return string(pass), nil
	}
	return "", fmt.Errorf("sealed object requires passphrase (no TTY available, use --seal-passphrase-file)")
}
