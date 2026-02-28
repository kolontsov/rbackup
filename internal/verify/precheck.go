package verify

import (
	"fmt"
	"strings"

	"github.com/kolontsov/rbackup/internal/crypto"
)

func precheckIdentityRecipient(metadata map[string]string, identityPath string) error {
	expectedFPs := strings.TrimSpace(metadata[crypto.RecipientMetadataFingerprintKey])
	if expectedFPs == "" {
		// Backward compatibility for objects created before recipient metadata.
		return nil
	}

	identityFPs, err := crypto.IdentityFileRecipientFingerprints(identityPath)
	if err != nil {
		return fmt.Errorf("recipient precheck: %w", err)
	}
	if len(identityFPs) == 0 {
		// Identity file contained no supported AGE-SECRET-KEY entries.
		return nil
	}

	expected := strings.Split(expectedFPs, ",")
	for _, idFP := range identityFPs {
		for _, expFP := range expected {
			if idFP == strings.TrimSpace(expFP) {
				return nil
			}
		}
	}

	return fmt.Errorf("identity file does not match any object recipient: %s=%s",
		crypto.RecipientMetadataFingerprintKey, expectedFPs)
}

func precheckSignature(metadata map[string]string, allowedSigners []string) error {
	if len(allowedSigners) == 0 {
		return nil
	}

	sig := strings.TrimSpace(metadata[crypto.SignatureMetadataKey])
	if sig == "" {
		return fmt.Errorf("signature precheck: backup is unsigned (missing %s metadata)", crypto.SignatureMetadataKey)
	}

	sha256Hex := strings.TrimSpace(metadata["x-rbackup-sha256"])
	if sha256Hex == "" {
		return fmt.Errorf("signature precheck: x-rbackup-signature present but x-rbackup-sha256 missing")
	}

	if err := crypto.VerifySignature(sha256Hex, sig, allowedSigners); err != nil {
		return fmt.Errorf("signature precheck: %w", err)
	}
	return nil
}
