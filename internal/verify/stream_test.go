package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
)

type fakeObjectClient struct {
	headMetadata map[string]string
	downloadData []byte
}

func (f fakeObjectClient) Head(_ context.Context, _, _ string) (*storage.HeadResult, error) {
	return &storage.HeadResult{Metadata: f.headMetadata}, nil
}

func (f fakeObjectClient) Download(_ context.Context, _, _ string, dst io.Writer) error {
	_, err := dst.Write(f.downloadData)
	return err
}

func TestStreamDecrypt_RejectsCiphertextHashMismatch(t *testing.T) {
	identity, recipient, err := crypto.DeriveIdentity("test-passphrase-at-least-16", "test-org")
	if err != nil {
		t.Fatal(err)
	}

	identityPath := t.TempDir() + "/identity.txt"
	if err := os.WriteFile(identityPath, []byte(identity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	originalCiphertext := encryptForTest(t, recipient, []byte("original plaintext"))
	forgedCiphertext := encryptForTest(t, recipient, []byte("forged plaintext"))

	expectedSHA := sha256Hex(originalCiphertext)
	privPath, pubKey := signTestHelper(t)
	sig, err := crypto.SignSHA256Hex(privPath, expectedSHA)
	if err != nil {
		t.Fatal(err)
	}

	client := fakeObjectClient{
		headMetadata: map[string]string{
			"x-rbackup-sha256":          expectedSHA,
			crypto.SignatureMetadataKey: sig,
		},
		downloadData: forgedCiphertext,
	}

	err = streamDecrypt(context.Background(), client, "host01/test.age", "", identityPath, "", "verifying", []string{pubKey}, io.Discard)
	if err == nil {
		t.Fatal("expected ciphertext hash mismatch")
	}
	if !strings.Contains(err.Error(), "ciphertext hash mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func encryptForTest(t *testing.T, recipient string, plaintext []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := crypto.Encrypt(&buf, bytes.NewReader(plaintext), recipient); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
