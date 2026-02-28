//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kolontsov/rbackup/internal/backup"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/notify"
)

// ---------------------------------------------------------------------------
// config test --read (HeadBucket)
// ---------------------------------------------------------------------------

func TestConfigTestRead(t *testing.T) {
	cfg := testConfig(t)
	clients := testClients(t, cfg)

	for name, client := range clients {
		if err := client.HeadBucket(context.Background()); err != nil {
			t.Errorf("HeadBucket %s: %v", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// config test --write (probe upload)
// ---------------------------------------------------------------------------

func TestConfigTestWrite(t *testing.T) {
	cfg := testConfig(t)
	clients := testClients(t, cfg)

	probeKey := ".rbackup/test/integ/probe-test"
	for name, client := range clients {
		_, err := client.UploadBytes(context.Background(), probeKey,
			strings.NewReader("rbackup connectivity test"),
			map[string]string{"x-rbackup-test": "true"})
		if err != nil {
			t.Errorf("write probe %s: %v", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// config test --write with custom key
// ---------------------------------------------------------------------------

func TestConfigTestWriteKey(t *testing.T) {
	cfg := testConfig(t)
	client := testClient(t, cfg.Remotes[0])

	customKey := ".rbackup/test/custom-key-test"
	_, err := client.UploadBytes(context.Background(), customKey,
		strings.NewReader("custom key probe"),
		map[string]string{"x-rbackup-test": "true"})
	if err != nil {
		t.Fatalf("write probe with custom key: %v", err)
	}

	// Verify it exists
	head := headObject(t, minio1Endpoint, minio1Bucket, customKey)
	if head.Metadata["x-rbackup-test"] != "true" {
		t.Error("probe metadata missing")
	}
}

// ---------------------------------------------------------------------------
// Lock contention
// ---------------------------------------------------------------------------

func TestLockContention(t *testing.T) {
	lockDir := t.TempDir()

	lock1, err := backup.AcquireLock(lockDir, "contention-test", 0)
	if err != nil {
		t.Fatalf("acquiring lock1: %v", err)
	}
	defer lock1.Release()

	// Second lock should fail immediately
	_, err = backup.AcquireLock(lockDir, "contention-test", 0)
	if err == nil {
		t.Fatal("expected lock contention error")
	}
	if !strings.Contains(err.Error(), "lock already held") {
		t.Errorf("unexpected error: %v", err)
	}

	// Lock with wait should eventually succeed after release
	doneCh := make(chan error, 1)
	go func() {
		_, err := backup.AcquireLock(lockDir, "contention-test", 2*time.Second)
		doneCh <- err
	}()

	time.Sleep(200 * time.Millisecond)
	lock1.Release()

	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("lock2 with wait should succeed after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock2 timed out")
	}
}

// ---------------------------------------------------------------------------
// Lock contention during concurrent backup
// ---------------------------------------------------------------------------

func TestLockContentionConcurrentBackup(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "lock-test.txt", "lock test data")

	cfg.Settings.LockDir = filepath.Join(tmpDir, "locks")
	cfg.Backups["lock-test"] = config.BackupEntry{
		ObjectName: "lock-test.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	// Hold a lock
	lock, err := backup.AcquireLock(cfg.Settings.LockDir, cfg.Settings.Namespace, 0)
	if err != nil {
		t.Fatalf("acquiring pre-lock: %v", err)
	}

	// Attempt backup (which tries to acquire the same lock) — should fail
	// We simulate this by trying to acquire the lock again
	_, err = backup.AcquireLock(cfg.Settings.LockDir, cfg.Settings.Namespace, 0)
	if err == nil {
		t.Error("expected lock contention")
	}

	lock.Release()

	// Now backup should work
	receipt := backup.RunBackup(context.Background(), "lock-test", cfg.Backups["lock-test"], cfg, clients, backup.BackupOptions{})
	if receipt.Status != "success" {
		t.Errorf("backup after lock release should succeed: %+v", receipt.Results)
	}
}

// ---------------------------------------------------------------------------
// Notification: success URL routing
// ---------------------------------------------------------------------------

func TestNotificationSuccessRouting(t *testing.T) {
	var mu sync.Mutex
	var received *notify.Payload
	var receivedURL string

	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedURL = "success"
		body, _ := io.ReadAll(r.Body)
		var p notify.Payload
		json.Unmarshal(body, &p)
		received = &p
		w.WriteHeader(200)
	}))
	defer successServer.Close()

	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedURL = "failure"
		w.WriteHeader(200)
	}))
	defer failureServer.Close()

	notif := config.Notifications{
		SuccessURL:      successServer.URL,
		FailureURL:      failureServer.URL,
		WebhookTimeoutD: 5 * time.Second,
	}

	notify.Send(notif, notify.Payload{
		Status:    "success",
		Total:     2,
		Succeeded: 1,
		Skipped:   1,
		FailedIDs: []string{},
		Receipts:  []backup.Receipt{{BackupID: "test", Status: "success"}},
	})

	mu.Lock()
	defer mu.Unlock()
	if receivedURL != "success" {
		t.Errorf("expected success URL, got %q", receivedURL)
	}
	if received == nil {
		t.Fatal("no payload received")
	}
	if received.Status != "success" {
		t.Errorf("payload status = %q", received.Status)
	}
	if received.Total != 2 {
		t.Errorf("payload total = %d", received.Total)
	}
}

// ---------------------------------------------------------------------------
// Notification: failure URL routing
// ---------------------------------------------------------------------------

func TestNotificationFailureRouting(t *testing.T) {
	var mu sync.Mutex
	var received *notify.Payload

	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		var p notify.Payload
		json.Unmarshal(body, &p)
		received = &p
		w.WriteHeader(200)
	}))
	defer failureServer.Close()

	notif := config.Notifications{
		SuccessURL:      "http://should-not-be-called",
		FailureURL:      failureServer.URL,
		WebhookTimeoutD: 5 * time.Second,
	}

	notify.Send(notif, notify.Payload{
		Status:    "failure",
		Total:     1,
		Failed:    1,
		FailedIDs: []string{"broken-backup"},
	})

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("failure webhook not received")
	}
	if received.Status != "failure" {
		t.Errorf("status = %q", received.Status)
	}
	if len(received.FailedIDs) != 1 || received.FailedIDs[0] != "broken-backup" {
		t.Errorf("failed_ids = %v", received.FailedIDs)
	}
}

// ---------------------------------------------------------------------------
// Notification: payload includes receipts
// ---------------------------------------------------------------------------

func TestNotificationPayloadReceipts(t *testing.T) {
	var rawBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer server.Close()

	notif := config.Notifications{
		SuccessURL:      server.URL,
		WebhookTimeoutD: 5 * time.Second,
	}

	notify.Send(notif, notify.Payload{
		Status:    "success",
		Total:     1,
		Succeeded: 1,
		FailedIDs: []string{},
		Receipts: []backup.Receipt{{
			BackupID: "test",
			Status:   "success",
			Results: []backup.RemoteResult{{
				Remote: "minio1",
				Key:    "integ/test.age",
				Status: "success",
			}},
		}},
	})

	if len(rawBody) == 0 {
		t.Fatal("no body received")
	}
	// Parse and check receipts field exists
	var parsed map[string]interface{}
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		t.Fatalf("parsing payload: %v", err)
	}
	if _, ok := parsed["receipts"]; !ok {
		t.Error("payload missing 'receipts' field")
	}
}

// ---------------------------------------------------------------------------
// Notification: custom headers
// ---------------------------------------------------------------------------

func TestNotificationCustomHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(200)
	}))
	defer server.Close()

	notif := config.Notifications{
		SuccessURL:      server.URL,
		Headers:         map[string]string{"Authorization": "Bearer test-token", "X-Custom": "value"},
		WebhookTimeoutD: 5 * time.Second,
	}

	notify.Send(notif, notify.Payload{Status: "success", FailedIDs: []string{}})

	if receivedHeaders.Get("Authorization") != "Bearer test-token" {
		t.Errorf("Authorization = %q", receivedHeaders.Get("Authorization"))
	}
	if receivedHeaders.Get("X-Custom") != "value" {
		t.Errorf("X-Custom = %q", receivedHeaders.Get("X-Custom"))
	}
}

// ---------------------------------------------------------------------------
// Notification: delivery error is best-effort (exit code unchanged)
// ---------------------------------------------------------------------------

func TestNotificationDeliveryErrorBestEffort(t *testing.T) {
	notif := config.Notifications{
		SuccessURL:      "http://127.0.0.1:1", // connection refused
		WebhookTimeoutD: 1 * time.Second,
	}

	// Should not panic
	notify.Send(notif, notify.Payload{Status: "success", FailedIDs: []string{}})
}

// ---------------------------------------------------------------------------
// Canonical receipt schema validation
// ---------------------------------------------------------------------------

func TestReceiptSchema(t *testing.T) {
	cfg := testConfig(t)
	tmpDir := t.TempDir()
	srcFile := writeTestFile(t, tmpDir, "schema.txt", "schema test")

	cfg.Backups["schema-test"] = config.BackupEntry{
		ObjectName: "schema.txt",
		File:       srcFile,
	}
	clients := testClients(t, cfg)

	receipt := backup.RunBackup(context.Background(), "schema-test", cfg.Backups["schema-test"], cfg, clients, backup.BackupOptions{})

	// Marshal and verify JSON structure
	data, err := json.Marshal([]backup.Receipt{receipt})
	if err != nil {
		t.Fatal(err)
	}

	var parsed []map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshaling receipt: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(parsed))
	}

	r := parsed[0]
	// Required fields
	for _, field := range []string{"backup_id", "status", "created_at", "results"} {
		if _, ok := r[field]; !ok {
			t.Errorf("missing field %q in receipt", field)
		}
	}
	// "reason" should not be present for non-skipped
	if _, ok := r["reason"]; ok {
		t.Error("'reason' should be omitted for non-skipped receipts")
	}

	// Check results sub-objects
	results := r["results"].([]interface{})
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	result := results[0].(map[string]interface{})
	for _, field := range []string{"remote", "key", "version_id", "status", "error"} {
		if _, ok := result[field]; !ok {
			t.Errorf("missing field %q in result", field)
		}
	}
}

// ---------------------------------------------------------------------------
// Receipt: skipped entry schema
// ---------------------------------------------------------------------------

func TestReceiptSkippedSchema(t *testing.T) {
	receipt := backup.Receipt{
		BackupID:  "manual",
		Status:    "skipped",
		CreatedAt: "",
		Reason:    "manual_only",
		Results:   []backup.RemoteResult{},
	}

	data, _ := json.Marshal(receipt)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["status"] != "skipped" {
		t.Errorf("status = %v", parsed["status"])
	}
	if parsed["reason"] != "manual_only" {
		t.Errorf("reason = %v", parsed["reason"])
	}
	if parsed["created_at"] != "" {
		t.Errorf("created_at should be empty for skipped")
	}
}
