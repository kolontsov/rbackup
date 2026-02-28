package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"strings"

	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
)

type objectClient interface {
	Head(ctx context.Context, key, versionID string) (*storage.HeadResult, error)
	Download(ctx context.Context, key, versionID string, dst io.Writer) error
}

func streamDecrypt(ctx context.Context, client objectClient, key, versionID, identityPath, sealPassphrase, action string, allowedSigners []string, dst io.Writer) error {
	if identityPath == "" {
		return fmt.Errorf("identity file is required")
	}

	// Precheck metadata before streaming download/decrypt.
	// Recipient match is best-effort, but signature verification is strict when
	// allowed signers are configured.
	expectedCipherSHA := ""
	head, err := client.Head(ctx, key, versionID)
	if err != nil {
		if len(allowedSigners) > 0 {
			return fmt.Errorf("signature precheck requires object metadata: %w", err)
		}
	} else if head != nil {
		expectedCipherSHA = strings.TrimSpace(head.Metadata["x-rbackup-sha256"])
		if err := precheckIdentityRecipient(head.Metadata, identityPath); err != nil {
			return err
		}
		if err := precheckSignature(head.Metadata, allowedSigners); err != nil {
			return err
		}
	} else if len(allowedSigners) > 0 {
		return fmt.Errorf("signature precheck requires object metadata: empty HEAD response")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	downloadErrCh := make(chan error, 1)
	var ciphertextHash hash.Hash
	downloadDst := io.Writer(pw)
	if expectedCipherSHA != "" {
		ciphertextHash = sha256.New()
		downloadDst = io.MultiWriter(pw, ciphertextHash)
	}
	go func() {
		err := client.Download(runCtx, key, versionID, downloadDst)
		pw.CloseWithError(err)
		downloadErrCh <- err
	}()

	fail := func(primary error) error {
		_ = pr.CloseWithError(primary)
		cancel()
		<-downloadErrCh
		return primary
	}

	if IsSealed(key) {
		if sealPassphrase == "" {
			return fail(fmt.Errorf("sealed object requires passphrase"))
		}
		if err := crypto.DecryptSealed(dst, pr, identityPath, sealPassphrase); err != nil {
			return fail(fmt.Errorf("%s sealed backup: %w", action, err))
		}
	} else {
		if err := crypto.Decrypt(dst, pr, identityPath); err != nil {
			return fail(fmt.Errorf("%s backup: %w", action, err))
		}
	}

	if err := <-downloadErrCh; err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	if expectedCipherSHA != "" {
		actualCipherSHA := hex.EncodeToString(ciphertextHash.Sum(nil))
		if !strings.EqualFold(actualCipherSHA, expectedCipherSHA) {
			return fmt.Errorf("ciphertext hash mismatch: x-rbackup-sha256=%s, downloaded=%s",
				expectedCipherSHA, actualCipherSHA)
		}
	}

	return nil
}
