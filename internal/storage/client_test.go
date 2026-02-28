package storage

import (
	"testing"

	"github.com/kolontsov/rbackup/internal/config"
)

func TestNewClientPathStyle(t *testing.T) {
	remote := config.Remote{
		Name:            "test",
		Type:            "s3",
		Bucket:          "test-bucket",
		Endpoint:        "http://localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey:  "minioadmin",
	}

	client, err := NewClient(t.Context(), remote)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.Bucket != "test-bucket" {
		t.Errorf("bucket = %q", client.Bucket)
	}
	if client.Name != "test" {
		t.Errorf("name = %q", client.Name)
	}
}

func TestNewClientDefaultChain(t *testing.T) {
	remote := config.Remote{
		Name:             "dc-test",
		Type:             "s3",
		Region:           "us-east-1",
		Bucket:           "my-bucket",
		CredentialSource: "default_chain",
	}

	client, err := NewClient(t.Context(), remote)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.Bucket != "my-bucket" {
		t.Errorf("bucket = %q", client.Bucket)
	}
}
