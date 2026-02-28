package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/kolontsov/rbackup/internal/fsutil"
)

const (
	RecipientMetadataFingerprintKey = "x-rbackup-recipient-fp"
)

func canonicalRecipient(recipient string) (string, error) {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return "", fmt.Errorf("recipient is empty")
	}
	if r, err := age.ParseX25519Recipient(recipient); err == nil {
		return r.String(), nil
	}
	if r, err := age.ParseHybridRecipient(recipient); err == nil {
		return r.String(), nil
	}
	return "", fmt.Errorf("unsupported recipient format")
}

func recipientFingerprint(canonicalRecipient string) string {
	sum := sha256.Sum256([]byte(canonicalRecipient))
	return hex.EncodeToString(sum[:])
}

// RecipientFingerprints returns comma-separated SHA-256 fingerprints for a
// newline-joined recipient string.
func RecipientFingerprints(recipientStr string) (string, error) {
	var fps []string
	for _, line := range strings.Split(recipientStr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		canonical, err := canonicalRecipient(line)
		if err != nil {
			return "", err
		}
		fps = append(fps, recipientFingerprint(canonical))
	}
	if len(fps) == 0 {
		return "", fmt.Errorf("recipient is empty")
	}
	return strings.Join(fps, ","), nil
}

// IdentityFileRecipientFingerprints derives recipient fingerprints from
// AGE-SECRET-KEY* entries in an identity file.
func IdentityFileRecipientFingerprints(identityPath string) ([]string, error) {
	if err := fsutil.ValidateSecureFile(identityPath); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(identityPath)
	if err != nil {
		return nil, fmt.Errorf("reading identity file: %w", err)
	}

	set := make(map[string]struct{})
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(strings.ToUpper(line), "AGE-SECRET-KEY") {
			// Ignore unsupported non-age-secret-key lines.
			continue
		}

		recipient, err := IdentityToRecipient(line)
		if err != nil {
			return nil, fmt.Errorf("parsing identity line %d: %w", i+1, err)
		}
		canonical, err := canonicalRecipient(recipient)
		if err != nil {
			return nil, fmt.Errorf("normalizing recipient line %d: %w", i+1, err)
		}
		set[recipientFingerprint(canonical)] = struct{}{}
	}

	fps := make([]string, 0, len(set))
	for fp := range set {
		fps = append(fps, fp)
	}
	sort.Strings(fps)
	return fps, nil
}
