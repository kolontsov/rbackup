package crypto

import (
	"fmt"
	"io"
	"os"

	"filippo.io/age"
	"github.com/kolontsov/rbackup/internal/fsutil"
)

// Decrypt decrypts src to dst using identities from the given file.
func Decrypt(dst io.Writer, src io.Reader, identityPath string) error {
	if err := fsutil.ValidateSecureFile(identityPath); err != nil {
		return err
	}

	f, err := os.Open(identityPath)
	if err != nil {
		return fmt.Errorf("opening identity file: %w", err)
	}
	defer f.Close()

	identities, err := age.ParseIdentities(f)
	if err != nil {
		return fmt.Errorf("parsing identities: %w", err)
	}

	r, err := age.Decrypt(src, identities...)
	if err != nil {
		return fmt.Errorf("decrypting: %w", err)
	}

	if _, err := io.Copy(dst, r); err != nil {
		return fmt.Errorf("reading decrypted data: %w", err)
	}

	return nil
}

// Unseal decrypts scrypt passphrase-encrypted src to dst.
func Unseal(dst io.Writer, src io.Reader, passphrase string) error {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return fmt.Errorf("creating scrypt identity: %w", err)
	}

	r, err := age.Decrypt(src, identity)
	if err != nil {
		return fmt.Errorf("unsealing: %w", err)
	}

	if _, err := io.Copy(dst, r); err != nil {
		return fmt.Errorf("reading unsealed data: %w", err)
	}

	return nil
}

// DecryptSealed decrypts a double-encrypted (.age2) stream: first unseal
// (scrypt), then decrypt (age identity). Uses io.Pipe to stream without temp files.
func DecryptSealed(dst io.Writer, src io.Reader, identityPath, passphrase string) error {
	if err := fsutil.ValidateSecureFile(identityPath); err != nil {
		return err
	}

	// Set up pipe: unseal writes to pw, decrypt reads from pr
	pr, pw := io.Pipe()

	errCh := make(chan error, 1)

	// Unseal in a goroutine, writing the inner (age-encrypted) stream to pw
	go func() {
		err := Unseal(pw, src, passphrase)
		pw.CloseWithError(err)
		errCh <- err
	}()

	// Decrypt the inner stream
	f, err := os.Open(identityPath)
	if err != nil {
		pr.Close()
		return fmt.Errorf("opening identity file: %w", err)
	}
	defer f.Close()

	identities, err := age.ParseIdentities(f)
	if err != nil {
		pr.Close()
		return fmt.Errorf("parsing identities: %w", err)
	}

	r, err := age.Decrypt(pr, identities...)
	if err != nil {
		pr.Close()
		return fmt.Errorf("decrypting inner layer: %w", err)
	}

	if _, err := io.Copy(dst, r); err != nil {
		pr.Close()
		return fmt.Errorf("reading decrypted data: %w", err)
	}

	// Check if unseal goroutine reported an error
	if unsealErr := <-errCh; unsealErr != nil {
		return fmt.Errorf("unseal failed: %w", unsealErr)
	}

	return nil
}
