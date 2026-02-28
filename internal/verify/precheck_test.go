package verify

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kolontsov/rbackup/internal/crypto"
	"golang.org/x/crypto/ssh"
)

func TestPrecheckIdentityRecipient_NoFingerprintMetadata(t *testing.T) {
	path := t.TempDir() + "/identity.txt"
	if err := os.WriteFile(path, []byte("# empty identity file\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := precheckIdentityRecipient(map[string]string{}, path); err != nil {
		t.Fatalf("precheck should be skipped, got %v", err)
	}
}

func TestPrecheckIdentityRecipient_Match(t *testing.T) {
	identity, recipient, err := crypto.DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	fp, err := crypto.RecipientFingerprints(recipient)
	if err != nil {
		t.Fatal(err)
	}

	path := t.TempDir() + "/identity.txt"
	if err := os.WriteFile(path, []byte(identity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	metadata := map[string]string{
		crypto.RecipientMetadataFingerprintKey: fp,
	}
	if err := precheckIdentityRecipient(metadata, path); err != nil {
		t.Fatalf("precheck should pass, got %v", err)
	}
}

func TestPrecheckIdentityRecipient_Mismatch(t *testing.T) {
	identity, _, err := crypto.DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	_, otherRecipient, err := crypto.DeriveIdentityPQ("test-passphrase-at-least-16", "other-org")
	if err != nil {
		t.Fatal(err)
	}
	fp, err := crypto.RecipientFingerprints(otherRecipient)
	if err != nil {
		t.Fatal(err)
	}

	path := t.TempDir() + "/identity.txt"
	if err := os.WriteFile(path, []byte(identity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	metadata := map[string]string{
		crypto.RecipientMetadataFingerprintKey: fp,
	}
	err = precheckIdentityRecipient(metadata, path)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match any object recipient") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func signTestHelper(t *testing.T) (privPath string, pubKey string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privPath = filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(privPath, pem.EncodeToMemory(pemBytes), 0600); err != nil {
		t.Fatal(err)
	}
	pubKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return privPath, pubKey
}

func TestPrecheckSignature_NoAllowedSigners(t *testing.T) {
	if err := precheckSignature(map[string]string{}, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestPrecheckSignature_NoSignatureMetadata(t *testing.T) {
	_, pubKey := signTestHelper(t)
	metadata := map[string]string{"x-rbackup-sha256": "abcd"}
	err := precheckSignature(metadata, []string{pubKey})
	if err == nil {
		t.Fatal("expected error for missing signature metadata")
	}
	if !strings.Contains(err.Error(), "missing x-rbackup-signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrecheckSignature_Valid(t *testing.T) {
	privPath, pubKey := signTestHelper(t)
	sha256Hex := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig, err := crypto.SignSHA256Hex(privPath, sha256Hex)
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]string{
		"x-rbackup-sha256":             sha256Hex,
		crypto.SignatureMetadataKey: sig,
	}
	if err := precheckSignature(metadata, []string{pubKey}); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestPrecheckSignature_Invalid(t *testing.T) {
	privPath, _ := signTestHelper(t)
	_, otherPub := signTestHelper(t)
	sha256Hex := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig, err := crypto.SignSHA256Hex(privPath, sha256Hex)
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]string{
		"x-rbackup-sha256":             sha256Hex,
		crypto.SignatureMetadataKey: sig,
	}
	if err := precheckSignature(metadata, []string{otherPub}); err == nil {
		t.Fatal("expected error for wrong signer")
	}
}

func TestPrecheckSignature_MissingSHA256(t *testing.T) {
	_, pubKey := signTestHelper(t)
	metadata := map[string]string{
		crypto.SignatureMetadataKey: "c29tZXNpZ25hdHVyZQ==",
	}
	err := precheckSignature(metadata, []string{pubKey})
	if err == nil {
		t.Fatal("expected error for missing sha256")
	}
	if !strings.Contains(err.Error(), "x-rbackup-sha256 missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrecheckIdentityRecipient_MultiRecipientMatch(t *testing.T) {
	identity, recipient, err := crypto.DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	_, otherRecipient, err := crypto.DeriveIdentityPQ("test-passphrase-at-least-16", "other-org")
	if err != nil {
		t.Fatal(err)
	}

	multi := recipient + "\n" + otherRecipient
	fp, err := crypto.RecipientFingerprints(multi)
	if err != nil {
		t.Fatal(err)
	}

	path := t.TempDir() + "/identity.txt"
	if err := os.WriteFile(path, []byte(identity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	metadata := map[string]string{
		crypto.RecipientMetadataFingerprintKey: fp,
	}
	if err := precheckIdentityRecipient(metadata, path); err != nil {
		t.Fatalf("precheck should pass when identity matches one of multiple recipients, got %v", err)
	}
}
