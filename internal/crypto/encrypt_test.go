package crypto

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// Generate a test identity
	passphrase := "test-passphrase-at-least-16"
	org := "test-org"
	identityStr, recipientStr, err := DeriveIdentity(passphrase, org)
	if err != nil {
		t.Fatal(err)
	}

	// Write identity file
	dir := t.TempDir()
	idFile := filepath.Join(dir, "identity.txt")
	if err := WriteIdentityFile(idFile, identityStr); err != nil {
		t.Fatal(err)
	}

	// Encrypt
	plaintext := []byte("hello world, this is a test of the age encryption")
	var ciphertext bytes.Buffer
	if err := Encrypt(&ciphertext, bytes.NewReader(plaintext), recipientStr); err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Decrypt
	var decrypted bytes.Buffer
	if err := Decrypt(&decrypted, &ciphertext, idFile); err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(decrypted.Bytes(), plaintext) {
		t.Errorf("decrypted data mismatch: got %q, want %q", decrypted.String(), string(plaintext))
	}
}

func TestSealUnsealRoundTrip(t *testing.T) {
	passphrase := "seal-test-passphrase"
	plaintext := []byte("sealed secret data")

	var sealed bytes.Buffer
	if err := Seal(&sealed, bytes.NewReader(plaintext), passphrase); err != nil {
		t.Fatalf("seal: %v", err)
	}

	var unsealed bytes.Buffer
	if err := Unseal(&unsealed, &sealed, passphrase); err != nil {
		t.Fatalf("unseal: %v", err)
	}

	if !bytes.Equal(unsealed.Bytes(), plaintext) {
		t.Errorf("unsealed data mismatch: got %q", unsealed.String())
	}
}

func TestDecryptSealedRoundTrip(t *testing.T) {
	// Generate identity
	passphrase := "test-passphrase-at-least-16"
	org := "test-org"
	identityStr, recipientStr, err := DeriveIdentity(passphrase, org)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	idFile := filepath.Join(dir, "identity.txt")
	if err := WriteIdentityFile(idFile, identityStr); err != nil {
		t.Fatal(err)
	}

	sealPassphrase := "seal-passphrase-test"
	plaintext := []byte("double encrypted secret")

	// Encrypt (inner layer)
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader(plaintext), recipientStr); err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Seal (outer layer)
	var sealed bytes.Buffer
	if err := Seal(&sealed, &encrypted, sealPassphrase); err != nil {
		t.Fatalf("seal: %v", err)
	}

	// DecryptSealed
	var decrypted bytes.Buffer
	if err := DecryptSealed(&decrypted, &sealed, idFile, sealPassphrase); err != nil {
		t.Fatalf("decrypt sealed: %v", err)
	}

	if !bytes.Equal(decrypted.Bytes(), plaintext) {
		t.Errorf("decrypted data mismatch: got %q", decrypted.String())
	}
}

func TestDecryptWrongIdentity(t *testing.T) {
	// Encrypt with one identity
	_, recipientStr, err := DeriveIdentity("test-passphrase-at-least-16", "org-a")
	if err != nil {
		t.Fatal(err)
	}

	var ciphertext bytes.Buffer
	if err := Encrypt(&ciphertext, bytes.NewReader([]byte("secret")), recipientStr); err != nil {
		t.Fatal(err)
	}

	// Decrypt with a different identity
	wrongIdentity, _, err := DeriveIdentity("different-passphrase-16", "org-b")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	idFile := filepath.Join(dir, "wrong.txt")
	if err := WriteIdentityFile(idFile, wrongIdentity); err != nil {
		t.Fatal(err)
	}

	var decrypted bytes.Buffer
	err = Decrypt(&decrypted, &ciphertext, idFile)
	if err == nil {
		t.Fatal("expected error decrypting with wrong identity")
	}
}

func TestUnsealWrongPassphrase(t *testing.T) {
	var sealed bytes.Buffer
	if err := Seal(&sealed, bytes.NewReader([]byte("secret")), "correct-passphrase"); err != nil {
		t.Fatal(err)
	}

	var unsealed bytes.Buffer
	err := Unseal(&unsealed, &sealed, "wrong-passphrase")
	if err == nil {
		t.Fatal("expected error unsealing with wrong passphrase")
	}
}

func TestDecryptInsecureIdentityFile(t *testing.T) {
	dir := t.TempDir()
	idFile := filepath.Join(dir, "insecure.txt")
	if err := os.WriteFile(idFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := Decrypt(&buf, strings.NewReader("dummy"), idFile)
	if err == nil {
		t.Fatal("expected error for insecure identity file")
	}
	if !strings.Contains(err.Error(), "insecure permissions") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEncryptDecryptHybridRecipientRoundTrip(t *testing.T) {
	hybridID, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatalf("generating hybrid identity: %v", err)
	}
	hybridRecipient := hybridID.Recipient().String()
	identityStr := hybridID.String()

	dir := t.TempDir()
	idFile := filepath.Join(dir, "hybrid-identity.txt")
	if err := os.WriteFile(idFile, []byte(identityStr+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("hello hybrid recipient")
	var ciphertext bytes.Buffer
	if err := Encrypt(&ciphertext, bytes.NewReader(plaintext), hybridRecipient); err != nil {
		t.Fatalf("encrypt hybrid: %v", err)
	}

	var decrypted bytes.Buffer
	if err := Decrypt(&decrypted, &ciphertext, idFile); err != nil {
		t.Fatalf("decrypt hybrid: %v", err)
	}

	if !bytes.Equal(decrypted.Bytes(), plaintext) {
		t.Errorf("decrypted data mismatch: got %q, want %q", decrypted.String(), string(plaintext))
	}
}

func TestEncryptMultiRecipientRoundTrip(t *testing.T) {
	identity1, recipient1, err := DeriveIdentity("test-passphrase-at-least-16", "org-1")
	if err != nil {
		t.Fatal(err)
	}
	identity2, recipient2, err := DeriveIdentity("different-passphrase-16", "org-2")
	if err != nil {
		t.Fatal(err)
	}

	multiRecipient := recipient1 + "\n" + recipient2
	plaintext := []byte("hello multi-recipient encryption")
	var ciphertext bytes.Buffer
	if err := Encrypt(&ciphertext, bytes.NewReader(plaintext), multiRecipient); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ciphertextBytes := ciphertext.Bytes()

	dir := t.TempDir()

	// Decrypt with first identity
	idFile1 := filepath.Join(dir, "identity1.txt")
	if err := WriteIdentityFile(idFile1, identity1); err != nil {
		t.Fatal(err)
	}
	var decrypted1 bytes.Buffer
	if err := Decrypt(&decrypted1, bytes.NewReader(ciphertextBytes), idFile1); err != nil {
		t.Fatalf("decrypt with identity1: %v", err)
	}
	if !bytes.Equal(decrypted1.Bytes(), plaintext) {
		t.Errorf("identity1 decrypted mismatch: got %q", decrypted1.String())
	}

	// Decrypt with second identity
	idFile2 := filepath.Join(dir, "identity2.txt")
	if err := WriteIdentityFile(idFile2, identity2); err != nil {
		t.Fatal(err)
	}
	var decrypted2 bytes.Buffer
	if err := Decrypt(&decrypted2, bytes.NewReader(ciphertextBytes), idFile2); err != nil {
		t.Fatalf("decrypt with identity2: %v", err)
	}
	if !bytes.Equal(decrypted2.Bytes(), plaintext) {
		t.Errorf("identity2 decrypted mismatch: got %q", decrypted2.String())
	}
}

func TestEncryptDecryptPostQuantumFromAgeKeygen(t *testing.T) {
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skipf("age-keygen not found: %v", err)
	}

	dir := t.TempDir()
	idFile := filepath.Join(dir, "pq-identity.txt")

	cmd := exec.Command("age-keygen", "-pq", "-o", idFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Older age-keygen versions might not support -pq.
		if strings.Contains(string(out), "-pq") || strings.Contains(string(out), "unknown flag") {
			t.Skipf("age-keygen -pq not supported by local binary: %s", strings.TrimSpace(string(out)))
		}
		t.Fatalf("running age-keygen -pq: %v\noutput:\n%s", err, string(out))
	}

	identityData, err := os.ReadFile(idFile)
	if err != nil {
		t.Fatalf("reading generated identity file: %v", err)
	}
	if !strings.Contains(string(identityData), "AGE-SECRET-KEY-PQ-1") {
		t.Fatalf("identity file is not PQ format:\n%s", string(identityData))
	}

	re := regexp.MustCompile(`Public key:\s*(age1pq[0-9a-z]+)`)
	m := re.FindStringSubmatch(string(out))
	if len(m) != 2 {
		t.Fatalf("failed to parse PQ public key from age-keygen output:\n%s", string(out))
	}
	recipient := m[1]

	plaintext := []byte("post-quantum recipient compatibility test")
	var ciphertext bytes.Buffer
	if err := Encrypt(&ciphertext, bytes.NewReader(plaintext), recipient); err != nil {
		t.Fatalf("encrypt with PQ recipient: %v", err)
	}

	var decrypted bytes.Buffer
	if err := Decrypt(&decrypted, &ciphertext, idFile); err != nil {
		t.Fatalf("decrypt with PQ identity file: %v", err)
	}
	if !bytes.Equal(decrypted.Bytes(), plaintext) {
		t.Errorf("decrypted mismatch: got %q, want %q", decrypted.String(), string(plaintext))
	}
}
