package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func writeSSHEd25519Key(t *testing.T, dir string) (privPath string, pubKey string) {
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
	privPath = filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(privPath, pem.EncodeToMemory(pemBytes), 0600); err != nil {
		t.Fatal(err)
	}
	pubKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return privPath, pubKey
}

func writeSSHRSAKey(t *testing.T, dir string) string {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := ssh.MarshalPrivateKey(rsaKey, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBytes), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSignAndVerify_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	privPath, pubKey := writeSSHEd25519Key(t, dir)

	sha256Hex := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig, err := SignSHA256Hex(privPath, sha256Hex)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	raw, _ := base64.StdEncoding.DecodeString(sig)
	if len(raw) != ed25519.SignatureSize {
		t.Fatalf("signature length = %d, want %d", len(raw), ed25519.SignatureSize)
	}

	if err := VerifySignature(sha256Hex, sig, []string{pubKey}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestSign_NotEd25519(t *testing.T) {
	dir := t.TempDir()
	rsaPath := writeSSHRSAKey(t, dir)

	_, err := SignSHA256Hex(rsaPath, "deadbeef")
	if err == nil {
		t.Fatal("expected error for RSA key")
	}
	if !strings.Contains(err.Error(), "Ed25519") {
		t.Fatalf("error should mention Ed25519, got: %v", err)
	}
}

func TestSign_InsecurePermissions(t *testing.T) {
	dir := t.TempDir()
	privPath, _ := writeSSHEd25519Key(t, dir)
	if err := os.Chmod(privPath, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := SignSHA256Hex(privPath, "deadbeef")
	if err == nil {
		t.Fatal("expected error for insecure permissions")
	}
	if !strings.Contains(err.Error(), "insecure permissions") {
		t.Fatalf("error should mention permissions, got: %v", err)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	dir := t.TempDir()
	privPathA, _ := writeSSHEd25519Key(t, dir)

	dirB := t.TempDir()
	_, pubKeyB := writeSSHEd25519Key(t, dirB)

	sha256Hex := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	sig, err := SignSHA256Hex(privPathA, sha256Hex)
	if err != nil {
		t.Fatal(err)
	}

	err = VerifySignature(sha256Hex, sig, []string{pubKeyB})
	if err == nil {
		t.Fatal("expected verification failure with wrong key")
	}
}

func TestVerify_MultiSigner(t *testing.T) {
	dir := t.TempDir()
	privPath, pubKey := writeSSHEd25519Key(t, dir)

	dirOther := t.TempDir()
	_, otherPubKey := writeSSHEd25519Key(t, dirOther)

	sha256Hex := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	sig, err := SignSHA256Hex(privPath, sha256Hex)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifySignature(sha256Hex, sig, []string{otherPubKey, pubKey}); err != nil {
		t.Fatalf("should match second signer: %v", err)
	}
}

func TestVerify_TamperedHash(t *testing.T) {
	dir := t.TempDir()
	privPath, pubKey := writeSSHEd25519Key(t, dir)

	sig, err := SignSHA256Hex(privPath, "aaa")
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifySignature("bbb", sig, []string{pubKey}); err == nil {
		t.Fatal("expected verification failure with tampered hash")
	}
}

func TestVerify_BadBase64(t *testing.T) {
	dir := t.TempDir()
	_, pubKey := writeSSHEd25519Key(t, dir)

	err := VerifySignature("aaa", "not-valid-base64!!!", []string{pubKey})
	if err == nil {
		t.Fatal("expected error for bad base64")
	}
}

func TestVerify_WrongSignatureLength(t *testing.T) {
	dir := t.TempDir()
	_, pubKey := writeSSHEd25519Key(t, dir)

	shortSig := base64.StdEncoding.EncodeToString([]byte("tooshort"))
	err := VerifySignature("aaa", shortSig, []string{pubKey})
	if err == nil {
		t.Fatal("expected error for wrong signature length")
	}
	if !strings.Contains(err.Error(), "expected 64") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseSSHEd25519PublicKey(t *testing.T) {
	dir := t.TempDir()
	_, pubKey := writeSSHEd25519Key(t, dir)

	key, err := ParseSSHEd25519PublicKey(pubKey)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(key) != ed25519.PublicKeySize {
		t.Fatalf("key size = %d, want %d", len(key), ed25519.PublicKeySize)
	}
}

func TestParseSSHEd25519PublicKey_NonEd25519(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	authKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))

	_, err = ParseSSHEd25519PublicKey(authKey)
	if err == nil {
		t.Fatal("expected error for RSA key")
	}
	if !strings.Contains(err.Error(), "not ssh-ed25519") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveAllowedSigners_InlineOnly(t *testing.T) {
	dir := t.TempDir()
	_, pubKey := writeSSHEd25519Key(t, dir)

	result, err := ResolveAllowedSigners([]string{pubKey}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0] != pubKey {
		t.Fatalf("got %v, want [%s]", result, pubKey)
	}
}

func TestResolveAllowedSigners_FileOnly(t *testing.T) {
	dir := t.TempDir()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	knownHostsLine := "example.com " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(knownHostsLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveAllowedSigners(nil, khPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(result))
	}
}

func TestResolveAllowedSigners_FileSkipsBlankAndCommentLines(t *testing.T) {
	dir := t.TempDir()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	knownHostsLine := "example.com " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	khPath := filepath.Join(dir, "known_hosts")
	content := "\n# comment\n" + knownHostsLine + "\n"
	if err := os.WriteFile(khPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveAllowedSigners(nil, khPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(result))
	}
}

func TestResolveAllowedSigners_FileParseError(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte("not-a-known-hosts-line\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveAllowedSigners(nil, khPath, "")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line number in error, got %v", err)
	}
}

func TestResolveAllowedSigners_Merged(t *testing.T) {
	dir := t.TempDir()
	_, inlinePubKey := writeSSHEd25519Key(t, dir)

	dir2 := t.TempDir()
	pub2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub2, err := ssh.NewPublicKey(pub2)
	if err != nil {
		t.Fatal(err)
	}
	knownHostsLine := "host2.example.com " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub2)))
	khPath := filepath.Join(dir2, "known_hosts")
	if err := os.WriteFile(khPath, []byte(knownHostsLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveAllowedSigners([]string{inlinePubKey}, khPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 signers, got %d", len(result))
	}
}

func TestResolveAllowedSigners_Deduplicated(t *testing.T) {
	dir := t.TempDir()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	authKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))

	knownHostsLine := "host.example.com " + authKey
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(knownHostsLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveAllowedSigners([]string{authKey}, khPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 deduplicated signer, got %d", len(result))
	}
}

func TestResolveAllowedSigners_NonEd25519Skipped(t *testing.T) {
	dir := t.TempDir()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	knownHostsLine := "host.example.com " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(knownHostsLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveAllowedSigners(nil, khPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 signers (RSA skipped), got %d", len(result))
	}
}

func TestResolveAllowedSigners_HostKeyPubFallback(t *testing.T) {
	dir := t.TempDir()
	privPath, pubKey := writeSSHEd25519Key(t, dir)

	// Write .pub file next to private key (SSH convention).
	if err := os.WriteFile(privPath+".pub", []byte(pubKey+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// No inline or file signers — should auto-resolve from host_key.pub.
	result, err := ResolveAllowedSigners(nil, "", privPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 auto-resolved signer, got %d", len(result))
	}
	if result[0] != pubKey {
		t.Fatalf("got %q, want %q", result[0], pubKey)
	}
}

func TestResolveAllowedSigners_HostKeyPubNotUsedWhenFileConfiguredButEmpty(t *testing.T) {
	dir := t.TempDir()
	privPath, pubKey := writeSSHEd25519Key(t, dir)

	if err := os.WriteFile(privPath+".pub", []byte(pubKey+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveAllowedSigners(nil, khPath, privPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 signers when file is configured but empty, got %d", len(result))
	}
}

func TestResolveAllowedSigners_HostKeyPubNotUsedWhenSignersExist(t *testing.T) {
	dir := t.TempDir()
	privPath, pubKey := writeSSHEd25519Key(t, dir)

	if err := os.WriteFile(privPath+".pub", []byte(pubKey+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Inline signers provided — host_key.pub should NOT be added.
	result, err := ResolveAllowedSigners([]string{pubKey}, "", privPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 signer (inline only), got %d", len(result))
	}
}

func TestResolveAllowedSigners_HostKeyPubMissing(t *testing.T) {
	dir := t.TempDir()
	privPath, _ := writeSSHEd25519Key(t, dir)

	// No .pub file exists — should gracefully return empty.
	result, err := ResolveAllowedSigners(nil, "", privPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 signers (no .pub), got %d", len(result))
	}
}
