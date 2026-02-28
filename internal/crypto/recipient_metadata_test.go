package crypto

import (
	"os"
	"strings"
	"testing"
)

func TestRecipientFingerprintsX25519(t *testing.T) {
	_, recipient, err := DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}

	fp, err := RecipientFingerprints(recipient)
	if err != nil {
		t.Fatalf("RecipientFingerprints: %v", err)
	}
	if len(fp) != 64 {
		t.Fatalf("fingerprint len = %d, want 64", len(fp))
	}
}

func TestRecipientFingerprintsPQ(t *testing.T) {
	_, recipient, err := DeriveIdentityPQ("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}

	fp, err := RecipientFingerprints(recipient)
	if err != nil {
		t.Fatalf("RecipientFingerprints: %v", err)
	}
	if len(fp) != 64 {
		t.Fatalf("fingerprint len = %d, want 64", len(fp))
	}
}

func TestRecipientFingerprintsMultiRecipient(t *testing.T) {
	_, r1, err := DeriveIdentity("test-passphrase-at-least-16", "org-1")
	if err != nil {
		t.Fatal(err)
	}
	_, r2, err := DeriveIdentityPQ("test-passphrase-at-least-16", "org-2")
	if err != nil {
		t.Fatal(err)
	}

	fp1, err := RecipientFingerprints(r1)
	if err != nil {
		t.Fatal(err)
	}
	fp2, err := RecipientFingerprints(r2)
	if err != nil {
		t.Fatal(err)
	}

	multi := r1 + "\n" + r2
	fp, err := RecipientFingerprints(multi)
	if err != nil {
		t.Fatalf("RecipientFingerprints multi: %v", err)
	}
	if fp != fp1+","+fp2 {
		t.Errorf("fingerprint = %q, want %q", fp, fp1+","+fp2)
	}
}

func TestIdentityFileRecipientFingerprints(t *testing.T) {
	xIdentity, xRecipient, err := DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	pqIdentity, pqRecipient, err := DeriveIdentityPQ("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}
	xFP, err := RecipientFingerprints(xRecipient)
	if err != nil {
		t.Fatal(err)
	}
	pqFP, err := RecipientFingerprints(pqRecipient)
	if err != nil {
		t.Fatal(err)
	}

	path := t.TempDir() + "/identity.txt"
	content := strings.Join([]string{
		"# comment",
		xIdentity,
		pqIdentity,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	fps, err := IdentityFileRecipientFingerprints(path)
	if err != nil {
		t.Fatalf("IdentityFileRecipientFingerprints: %v", err)
	}
	if len(fps) != 2 {
		t.Fatalf("fingerprints len = %d, want 2", len(fps))
	}

	set := map[string]bool{}
	for _, fp := range fps {
		set[fp] = true
	}
	if !set[xFP] {
		t.Fatalf("missing x25519 fingerprint")
	}
	if !set[pqFP] {
		t.Fatalf("missing pq fingerprint")
	}
}
