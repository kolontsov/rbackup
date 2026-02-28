//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	sTypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
)

// Globals set by TestMain.
var (
	minio1Endpoint  string
	minio2Endpoint  string
	minio1Container string
	minio2Container string

	testIdentityFile string
	testRecipient    string
	testSealPass     = "integration-seal-passphrase"

	testDir string // temp dir for the entire suite
)

const (
	minio1Bucket = "rbackup-test-1"
	minio2Bucket = "rbackup-test-2"
	minioUser    = "minioadmin"
	minioPass    = "minioadmin"
)

func TestMain(m *testing.M) {
	if !dockerAvailable() {
		fmt.Fprintln(os.Stderr, "SKIP: docker not available, skipping integration tests")
		os.Exit(0)
	}

	var err error
	testDir, err = os.MkdirTemp("", "rbackup-integ-*")
	if err != nil {
		log.Fatalf("creating temp dir: %v", err)
	}

	// Start two MinIO containers
	port1 := findFreePort()
	port2 := findFreePort()

	minio1Container, minio1Endpoint, err = startMinIO("rbackup-minio1", port1)
	if err != nil {
		log.Fatalf("starting minio1: %v", err)
	}
	minio2Container, minio2Endpoint, err = startMinIO("rbackup-minio2", port2)
	if err != nil {
		stopMinIO(minio1Container)
		log.Fatalf("starting minio2: %v", err)
	}

	// Wait for MinIO to be ready
	if err := waitForMinIO(minio1Endpoint); err != nil {
		cleanup()
		log.Fatalf("minio1 not ready: %v", err)
	}
	if err := waitForMinIO(minio2Endpoint); err != nil {
		cleanup()
		log.Fatalf("minio2 not ready: %v", err)
	}

	// Create buckets
	if err := createBucket(minio1Endpoint, minio1Bucket); err != nil {
		cleanup()
		log.Fatalf("creating bucket1: %v", err)
	}
	if err := createBucket(minio2Endpoint, minio2Bucket); err != nil {
		cleanup()
		log.Fatalf("creating bucket2: %v", err)
	}

	// Generate test identity
	testIdentityFile = filepath.Join(testDir, "identity.txt")
	identity, recipient, err := crypto.DeriveIdentity("integration-test-passphrase-16", "test-org")
	if err != nil {
		cleanup()
		log.Fatalf("deriving identity: %v", err)
	}
	testRecipient = recipient
	if err := crypto.WriteIdentityFile(testIdentityFile, identity); err != nil {
		cleanup()
		log.Fatalf("writing identity: %v", err)
	}

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func cleanup() {
	stopMinIO(minio1Container)
	stopMinIO(minio2Container)
	os.RemoveAll(testDir)
}

func dockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("finding free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func startMinIO(name string, port int) (containerID, endpoint string, err error) {
	// Remove any existing container with the same name
	exec.Command("docker", "rm", "-f", name).Run()

	endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := exec.Command("docker", "run", "-d",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:9000", port),
		"-e", "MINIO_ROOT_USER="+minioUser,
		"-e", "MINIO_ROOT_PASSWORD="+minioPass,
		"pgsty/minio", "server", "/data",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("docker run: %w", err)
	}
	containerID = strings.TrimSpace(out.String())
	return containerID, endpoint, nil
}

func stopMinIO(containerID string) {
	if containerID == "" {
		return
	}
	exec.Command("docker", "rm", "-f", containerID).Run()
}

func waitForMinIO(endpoint string) error {
	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		client, err := newRawS3Client(ctx, endpoint)
		if err == nil {
			_, err = client.ListBuckets(ctx, &s3.ListBucketsInput{})
			if err == nil {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for MinIO at %s", endpoint)
}

func newRawS3Client(ctx context.Context, endpoint string) (*s3.Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(minioUser, minioPass, ""),
		),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	}), nil
}

func createBucket(endpoint, bucket string) error {
	ctx := context.Background()
	client, err := newRawS3Client(ctx, endpoint)
	if err != nil {
		return err
	}
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	return err
}

func enableVersioning(endpoint, bucket string) error {
	ctx := context.Background()
	client, err := newRawS3Client(ctx, endpoint)
	if err != nil {
		return err
	}
	_, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &sTypes.VersioningConfiguration{
			Status: sTypes.BucketVersioningStatusEnabled,
		},
	})
	return err
}

// testConfig builds a config pointing at the two MinIO instances.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	tmpDir := t.TempDir()
	return &config.Config{
		Encryption: config.Encryption{
			AgeRecipient: testRecipient,
			IdentityFile: testIdentityFile,
		},
		Settings: config.Settings{
			Namespace:      "integ",
			CmdTimeout:     "30s",
			RemoteTimeout:  "30s",
			RemoteRetriesN: 1,
			CmdTimeoutD:    30 * time.Second,
			RemoteTimeoutD: 30 * time.Second,
			LockDir:        filepath.Join(tmpDir, "locks"),
			SpoolDir:       filepath.Join(tmpDir, "spool"),
			MaxBackupBytes:  50 * 1024 * 1024, // 50 MiB for tests
		},
		Remotes: []config.Remote{
			{
				Name:            "minio1",
				Type:            "s3",
				Region:          "us-east-1",
				Bucket:          minio1Bucket,
				Endpoint:        minio1Endpoint,
				AccessKeyID:     minioUser,
				SecretAccessKey: minioPass,
			},
			{
				Name:            "minio2",
				Type:            "s3",
				Region:          "us-east-1",
				Bucket:          minio2Bucket,
				Endpoint:        minio2Endpoint,
				AccessKeyID:     minioUser,
				SecretAccessKey: minioPass,
			},
		},
		Defaults: config.Defaults{
			Remotes: []string{"minio1", "minio2"},
		},
		Backups: map[string]config.BackupEntry{},
	}
}

// testClient creates a storage.Client for the given remote config.
func testClient(t *testing.T, remote config.Remote) *storage.Client {
	t.Helper()
	client, err := storage.NewClient(context.Background(), remote)
	if err != nil {
		t.Fatalf("creating test client: %v", err)
	}
	return client
}

// testClients creates all storage clients for a config.
func testClients(t *testing.T, cfg *config.Config) map[string]*storage.Client {
	t.Helper()
	clients := make(map[string]*storage.Client)
	for _, r := range cfg.Remotes {
		clients[r.Name] = testClient(t, r)
	}
	return clients
}

// headObject fetches metadata for an object directly via raw S3 client.
func headObject(t *testing.T, endpoint, bucket, key string) *s3.HeadObjectOutput {
	t.Helper()
	ctx := context.Background()
	client, err := newRawS3Client(ctx, endpoint)
	if err != nil {
		t.Fatalf("creating raw client: %v", err)
	}
	result, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("head object %s/%s: %v", bucket, key, err)
	}
	return result
}

// writeTestFile creates a file with content and returns its path.
func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}
	return p
}

// writeTestDir creates a directory with files and returns its path.
func writeTestDir(t *testing.T, parent, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for fname, content := range files {
		sub := filepath.Join(dir, fname)
		os.MkdirAll(filepath.Dir(sub), 0755)
		if err := os.WriteFile(sub, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// writeSealPassFile creates a passphrase file with 0600 perms.
func writeSealPassFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "seal-pass.txt")
	if err := os.WriteFile(p, []byte(testSealPass+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
