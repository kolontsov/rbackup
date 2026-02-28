package crypto

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/kolontsov/rbackup/internal/fsutil"
	"golang.org/x/crypto/ssh"
)

const SignatureMetadataKey = "x-rbackup-signature"

// SignSHA256Hex signs a SHA-256 hex string with an SSH Ed25519 host key.
// Returns base64-encoded raw 64-byte Ed25519 signature.
func SignSHA256Hex(hostKeyPath, sha256Hex string) (string, error) {
	keyBytes, err := fsutil.ReadSecureFile(hostKeyPath)
	if err != nil {
		return "", fmt.Errorf("reading host key: %w", err)
	}

	raw, err := ssh.ParseRawPrivateKey(keyBytes)
	if err != nil {
		return "", fmt.Errorf("parsing host key: %w", err)
	}

	edKey, ok := raw.(*ed25519.PrivateKey)
	if !ok {
		return "", fmt.Errorf("host key is %T, only Ed25519 keys are supported", raw)
	}

	sig := ed25519.Sign(*edKey, []byte(sha256Hex))
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifySignature verifies a base64 Ed25519 signature of sha256Hex against
// a list of allowed signers (authorized_keys format). Returns nil on first match.
func VerifySignature(sha256Hex, signatureB64 string, allowedSigners []string) error {
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature is %d bytes, expected %d", len(sig), ed25519.SignatureSize)
	}

	for _, signer := range allowedSigners {
		pub, err := ParseSSHEd25519PublicKey(signer)
		if err != nil {
			continue
		}
		if ed25519.Verify(pub, []byte(sha256Hex), sig) {
			return nil
		}
	}
	return fmt.Errorf("signature does not match any allowed signer")
}

// ParseSSHEd25519PublicKey parses an authorized_keys-format string and returns
// the Ed25519 public key. Returns error for non-Ed25519 key types.
func ParseSSHEd25519PublicKey(authorizedKey string) (ed25519.PublicKey, error) {
	sshPub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return nil, fmt.Errorf("parsing public key: %w", err)
	}
	if sshPub.Type() != "ssh-ed25519" {
		return nil, fmt.Errorf("key type %q is not ssh-ed25519", sshPub.Type())
	}
	cryptoPub, ok := sshPub.(ssh.CryptoPublicKey)
	if !ok {
		return nil, fmt.Errorf("key does not implement CryptoPublicKey")
	}
	edPub, ok := cryptoPub.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not ed25519")
	}
	return edPub, nil
}

// ResolveAllowedSigners merges inline keys with keys from a known_hosts file.
// Non-Ed25519 keys are silently skipped. Returns deduplicated list in
// authorized_keys format.
//
// When hostKeyPath is set and no inline or file signers are provided,
// <hostKeyPath>.pub is read as a fallback (standard SSH convention).
// This covers single-host setups where backup and restore run on the same machine.
// If the .pub file does not exist, no error is returned.
func ResolveAllowedSigners(inline []string, filePath, hostKeyPath string) ([]string, error) {
	seen := make(map[string]bool)
	var result []string

	for _, key := range inline {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if !seen[key] {
			seen[key] = true
			result = append(result, key)
		}
	}

	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("reading allowed signers file: %w", err)
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			_, hosts, sshPub, _, _, err := ssh.ParseKnownHosts([]byte(line + "\n"))
			if err != nil {
				return nil, fmt.Errorf("parsing allowed signers file line %d: %w", i+1, err)
			}
			_ = hosts

			if sshPub.Type() != "ssh-ed25519" {
				continue
			}
			authKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
			if !seen[authKey] {
				seen[authKey] = true
				result = append(result, authKey)
			}
		}
	}

	// Auto-resolve host_key.pub for single-host setups.
	if len(result) == 0 && filePath == "" && hostKeyPath != "" {
		pubPath := hostKeyPath + ".pub"
		data, err := os.ReadFile(pubPath)
		if err == nil {
			authKey := strings.TrimSpace(string(data))
			if authKey != "" {
				result = append(result, authKey)
			}
		}
	}

	return result, nil
}
