package crypto

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestNormalizeOrg(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"acme-prod", "acme-prod"},
		{"  ACME-Prod  ", "acme-prod"},
		{"  Hello World  ", "hello world"},
	}
	for _, tt := range tests {
		got := NormalizeOrg(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeOrg(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// Fixed test vector: a known passphrase + org must always produce the same recipient.
// This ensures cross-build / cross-platform determinism.
func TestDeriveKeyFixedVector(t *testing.T) {
	passphrase := "test-passphrase-at-least-16"
	org := "test-org"
	wantRecipient := "age1777nrfztd60hpwyyufrquelk4uc6se5x6tumfzh39zpznym2pp6s5h6n26"

	identity, recipient, err := DeriveIdentity(passphrase, org)
	if err != nil {
		t.Fatalf("DeriveIdentity: %v", err)
	}

	if !strings.HasPrefix(identity, "AGE-SECRET-KEY-1") {
		t.Errorf("identity should start with AGE-SECRET-KEY-1, got %q", identity[:20])
	}
	if !strings.HasPrefix(recipient, "age1") {
		t.Errorf("recipient should start with age1, got %q", recipient)
	}
	if recipient != wantRecipient {
		t.Fatalf("recipient changed: got %q, want %q", recipient, wantRecipient)
	}

	// Derive again to ensure determinism
	identity2, recipient2, err := DeriveIdentity(passphrase, org)
	if err != nil {
		t.Fatalf("second DeriveIdentity: %v", err)
	}
	if identity != identity2 {
		t.Error("identity not deterministic")
	}
	if recipient != recipient2 {
		t.Error("recipient not deterministic")
	}

}

func TestDeriveKeyPQDeterministic(t *testing.T) {
	passphrase := "test-passphrase-at-least-16"
	org := "test-org"

	identity, recipient, err := DeriveIdentityPQ(passphrase, org)
	if err != nil {
		t.Fatalf("DeriveIdentityPQ: %v", err)
	}

	if !strings.HasPrefix(identity, "AGE-SECRET-KEY-PQ-1") {
		t.Fatalf("identity should start with AGE-SECRET-KEY-PQ-1, got %q", identity)
	}
	if !strings.HasPrefix(recipient, "age1pq") {
		t.Fatalf("recipient should start with age1pq, got %q", recipient)
	}

	identity2, recipient2, err := DeriveIdentityPQ(passphrase, org)
	if err != nil {
		t.Fatalf("second DeriveIdentityPQ: %v", err)
	}
	if identity != identity2 {
		t.Error("PQ identity not deterministic")
	}
	if recipient != recipient2 {
		t.Error("PQ recipient not deterministic")
	}
}

func TestDeriveKeyOrgNormalization(t *testing.T) {
	_, r1, err := DeriveIdentity("test-passphrase-at-least-16", "Test-Org")
	if err != nil {
		t.Fatal(err)
	}
	_, r2, err := DeriveIdentity("test-passphrase-at-least-16", "  test-org  ")
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Errorf("org normalization should produce same key: %q vs %q", r1, r2)
	}
}

func TestDeriveKeyDifferentOrgs(t *testing.T) {
	_, r1, err := DeriveIdentity("test-passphrase-at-least-16", "org-a")
	if err != nil {
		t.Fatal(err)
	}
	_, r2, err := DeriveIdentity("test-passphrase-at-least-16", "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if r1 == r2 {
		t.Error("different orgs should produce different keys")
	}
}

func TestDeriveKeyPQOrgNormalization(t *testing.T) {
	_, r1, err := DeriveIdentityPQ("test-passphrase-at-least-16", "Test-Org")
	if err != nil {
		t.Fatal(err)
	}
	_, r2, err := DeriveIdentityPQ("test-passphrase-at-least-16", "  test-org  ")
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Errorf("org normalization should produce same PQ key: %q vs %q", r1, r2)
	}
}

func TestDeriveKeyPQDifferentOrgs(t *testing.T) {
	_, r1, err := DeriveIdentityPQ("test-passphrase-at-least-16", "org-a")
	if err != nil {
		t.Fatal(err)
	}
	_, r2, err := DeriveIdentityPQ("test-passphrase-at-least-16", "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if r1 == r2 {
		t.Error("different orgs should produce different PQ keys")
	}
}

func TestDeriveKeySameMaterialForBothModes(t *testing.T) {
	k1 := DeriveKey("test-passphrase-at-least-16", "test-org")
	k2 := DeriveKey("test-passphrase-at-least-16", "test-org")
	if string(k1) != string(k2) {
		t.Fatalf("expected deterministic Argon output")
	}
}

func TestValidateRecipientForMode(t *testing.T) {
	_, xRecipient, err := DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	_, pqRecipient, err := DeriveIdentityPQ("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateRecipientForMode(xRecipient, false); err != nil {
		t.Fatalf("x25519 recipient should validate: %v", err)
	}
	if err := ValidateRecipientForMode(pqRecipient, true); err != nil {
		t.Fatalf("pq recipient should validate: %v", err)
	}
	if err := ValidateRecipientForMode(pqRecipient, false); err == nil {
		t.Fatalf("pq recipient should fail x25519 validation")
	}
	if err := ValidateRecipientForMode(xRecipient, true); err == nil {
		t.Fatalf("x25519 recipient should fail pq validation")
	}
}

func TestWriteIdentityFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/identity.txt"
	identity, recipient, err := DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	err = WriteIdentityFile(path, identity)
	if err != nil {
		t.Fatal(err)
	}

	// Check file exists with correct permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %04o, want 0600", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 3 {
		t.Fatalf("unexpected identity file format: %q", text)
	}
	if !strings.HasPrefix(lines[0], "# created: ") {
		t.Fatalf("missing created header: %q", lines[0])
	}
	createdVal := strings.TrimPrefix(lines[0], "# created: ")
	if _, err := time.Parse(time.RFC3339, createdVal); err != nil {
		t.Fatalf("invalid created header %q: %v", createdVal, err)
	}
	if lines[1] != "# public key: "+recipient {
		t.Fatalf("public key header = %q, want %q", lines[1], "# public key: "+recipient)
	}
	if lines[2] != identity {
		t.Fatalf("identity line = %q, want %q", lines[2], identity)
	}
}

func TestWriteIdentityFileOverwritesExistingMode(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/identity.txt"
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	identity, _, err := DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteIdentityFile(path, identity); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestWriteIdentityFileWithMetadataIncludesNormalizedOrg(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/identity.txt"
	identity, recipient, err := DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteIdentityFileWithMetadata(path, identity, "  Acme-Prod  "); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# public key: "+recipient+"\n") {
		t.Fatalf("missing public key header in %q", text)
	}
	if !strings.Contains(text, "# org: acme-prod\n") {
		t.Fatalf("missing normalized org header in %q", text)
	}
}

func TestWriteIdentityFileWithMetadataPQ(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/identity-pq.txt"
	identity, recipient, err := DeriveIdentityPQ("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteIdentityFileWithMetadata(path, identity, "Acme-Prod"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# public key: "+recipient+"\n") {
		t.Fatalf("missing public key header in %q", text)
	}
	if !strings.Contains(text, identity+"\n") {
		t.Fatalf("missing identity line in %q", text)
	}
}
