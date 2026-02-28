package crypto

import (
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

// Encrypt encrypts src to dst using the given age recipient string.
func Encrypt(dst io.Writer, src io.Reader, recipientStr string) error {
	recipients, err := age.ParseRecipients(strings.NewReader(strings.TrimSpace(recipientStr) + "\n"))
	if err != nil {
		return fmt.Errorf("parsing recipient: %w", err)
	}
	if len(recipients) == 0 {
		return fmt.Errorf("parsing recipient: no recipients found")
	}

	w, err := age.Encrypt(dst, recipients...)
	if err != nil {
		return fmt.Errorf("creating encryptor: %w", err)
	}

	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("encrypting: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("finalizing encryption: %w", err)
	}

	return nil
}

// Seal encrypts src to dst using scrypt passphrase-based encryption.
func Seal(dst io.Writer, src io.Reader, passphrase string) error {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return fmt.Errorf("creating scrypt recipient: %w", err)
	}

	w, err := age.Encrypt(dst, recipient)
	if err != nil {
		return fmt.Errorf("creating seal encryptor: %w", err)
	}

	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("sealing: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("finalizing seal: %w", err)
	}

	return nil
}
