package crypto

import (
	"fmt"
	"os"
	"strings"
	"time"

	"eagain.net/go/bech32"
	"filippo.io/age"
	"golang.org/x/crypto/argon2"
)

const (
	ArgonTime    = 4
	ArgonMemory  = 256 * 1024 // 256 MB in KB
	ArgonThreads = 4
	ArgonKeyLen  = 32

	SaltPrefix = "rbackup-key-v1-"

	bech32HRPX25519 = "age-secret-key-"
	bech32HRPPQ     = "age-secret-key-pq-"

	identityPrefixX25519 = "AGE-SECRET-KEY-1"
	identityPrefixPQ     = "AGE-SECRET-KEY-PQ-1"

	MinPassphraseLen = 16
)

// NormalizeOrg trims whitespace and lowercases the org string.
func NormalizeOrg(org string) string {
	return strings.ToLower(strings.TrimSpace(org))
}

// DeriveKey derives 32-byte identity secret material from passphrase and org
// using Argon2id. The same material is used for both X25519 and PQ identities;
// the distinction happens at materialToIdentity time via bech32 HRP selection.
func DeriveKey(passphrase, org string) []byte {
	normalizedOrg := NormalizeOrg(org)
	salt := []byte(SaltPrefix + normalizedOrg)
	return argon2.IDKey([]byte(passphrase), salt, ArgonTime, ArgonMemory, ArgonThreads, ArgonKeyLen)
}

func materialToIdentity(material []byte, pq bool) (string, error) {
	hrp := bech32HRPX25519
	if pq {
		hrp = bech32HRPPQ
	}

	encoded, err := bech32.Encode(hrp, material)
	if err != nil {
		return "", fmt.Errorf("bech32 encode: %w", err)
	}
	identityStr := strings.ToUpper(encoded)

	if pq {
		if _, err := age.ParseHybridIdentity(identityStr); err != nil {
			return "", fmt.Errorf("invalid identity: %w", err)
		}
		return identityStr, nil
	}

	if _, err := age.ParseX25519Identity(identityStr); err != nil {
		return "", fmt.Errorf("invalid identity: %w", err)
	}
	return identityStr, nil
}

// KeyToIdentity converts 32-byte identity secret material to an age X25519
// identity string.
func KeyToIdentity(material []byte) (string, error) {
	return materialToIdentity(material, false)
}

// KeyToPQIdentity converts 32-byte identity secret material to an age
// MLKEM768-X25519 identity string.
func KeyToPQIdentity(material []byte) (string, error) {
	return materialToIdentity(material, true)
}

// IdentityToRecipient returns the public recipient for an identity string.
func IdentityToRecipient(identityStr string) (string, error) {
	identityStr = strings.TrimSpace(identityStr)

	if strings.HasPrefix(identityStr, identityPrefixX25519) {
		id, err := age.ParseX25519Identity(identityStr)
		if err != nil {
			return "", fmt.Errorf("parse identity: %w", err)
		}
		return id.Recipient().String(), nil
	}

	if strings.HasPrefix(identityStr, identityPrefixPQ) {
		id, err := age.ParseHybridIdentity(identityStr)
		if err != nil {
			return "", fmt.Errorf("parse identity: %w", err)
		}
		return id.Recipient().String(), nil
	}

	// Best-effort fallback in case of non-canonical casing/formatting.
	if id, err := age.ParseX25519Identity(identityStr); err == nil {
		return id.Recipient().String(), nil
	}
	if id, err := age.ParseHybridIdentity(identityStr); err == nil {
		return id.Recipient().String(), nil
	}

	return "", fmt.Errorf("unsupported identity format")
}

// ValidateRecipientForMode validates recipient format for selected derive mode.
func ValidateRecipientForMode(recipient string, pq bool) error {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return fmt.Errorf("recipient is empty")
	}

	if pq {
		if _, err := age.ParseHybridRecipient(recipient); err != nil {
			return fmt.Errorf("parse recipient: %w", err)
		}
		return nil
	}

	if _, err := age.ParseX25519Recipient(recipient); err != nil {
		return fmt.Errorf("parse recipient: %w", err)
	}
	return nil
}

func deriveIdentity(passphrase, org string, pq bool) (identity, recipient string, err error) {
	material := DeriveKey(passphrase, org)
	identity, err = materialToIdentity(material, pq)
	if err != nil {
		return "", "", err
	}
	recipient, err = IdentityToRecipient(identity)
	if err != nil {
		return "", "", err
	}
	return identity, recipient, nil
}

// DeriveIdentity derives an age identity from passphrase and org, returning
// the identity string and the recipient string.
func DeriveIdentity(passphrase, org string) (identity, recipient string, err error) {
	return deriveIdentity(passphrase, org, false)
}

// DeriveIdentityPQ derives an age MLKEM768-X25519 identity from passphrase and
// org, returning the identity string and the recipient string.
func DeriveIdentityPQ(passphrase, org string) (identity, recipient string, err error) {
	return deriveIdentity(passphrase, org, true)
}

// WriteIdentityFile writes an age identity to a file with mode 0600.
func WriteIdentityFile(path, identity string) error {
	return WriteIdentityFileWithMetadata(path, identity, "")
}

// WriteIdentityFileWithMetadata writes an age identity to a file with mode 0600
// and age-keygen style header comments.
func WriteIdentityFileWithMetadata(path, identity, org string) error {
	identity = strings.TrimSpace(identity)
	recipient, err := IdentityToRecipient(identity)
	if err != nil {
		return fmt.Errorf("invalid identity: %w", err)
	}
	normalizedOrg := NormalizeOrg(org)

	var b strings.Builder
	fmt.Fprintf(&b, "# created: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "# public key: %s\n", recipient)
	if normalizedOrg != "" {
		fmt.Fprintf(&b, "# org: %s\n", normalizedOrg)
	}
	fmt.Fprintf(&b, "%s\n", identity)
	content := b.String()

	// os.WriteFile only applies mode on create; enforce 0600 on existing files too.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

