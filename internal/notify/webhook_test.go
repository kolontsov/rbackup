package notify

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kolontsov/rbackup/internal/config"
)

func TestSendSuccess(t *testing.T) {
	var receivedPayload Payload
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notif := config.Notifications{
		SuccessURL:      server.URL,
		FailureURL:      "http://should-not-be-called",
		Headers:         map[string]string{"X-Custom": "test-value"},
		WebhookTimeoutD: 5 * time.Second,
	}

	Send(notif, Payload{
		Status:    "success",
		Total:     3,
		Succeeded: 2,
		Failed:    0,
		Skipped:   1,
		FailedIDs: []string{},
		Receipts:  []map[string]string{{"backup_id": "test"}},
	})

	if receivedPayload.Status != "success" {
		t.Errorf("status = %q", receivedPayload.Status)
	}
	if receivedPayload.Total != 3 {
		t.Errorf("total = %d", receivedPayload.Total)
	}
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", receivedHeaders.Get("Content-Type"))
	}
	if receivedHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("X-Custom = %q", receivedHeaders.Get("X-Custom"))
	}
}

func TestSendFailureURL(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		var p Payload
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &p)
		if p.Status != "failure" {
			t.Errorf("expected failure status, got %q", p.Status)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notif := config.Notifications{
		SuccessURL:      "http://should-not-be-called",
		FailureURL:      server.URL,
		WebhookTimeoutD: 5 * time.Second,
	}

	Send(notif, Payload{
		Status:    "failure",
		Total:     1,
		Failed:    1,
		FailedIDs: []string{"test-backup"},
	})

	if !called {
		t.Error("failure URL was not called")
	}
}

func TestSendEmptyURL(t *testing.T) {
	// Should not panic or error when URLs are empty
	notif := config.Notifications{}
	Send(notif, Payload{Status: "success"})
}

func TestSendServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notif := config.Notifications{
		SuccessURL:      server.URL,
		WebhookTimeoutD: 5 * time.Second,
	}

	// Should not panic; error is logged to stderr
	Send(notif, Payload{Status: "success"})
}

func TestSendNilFailedIDs(t *testing.T) {
	var receivedPayload Payload
	var rawBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ = io.ReadAll(r.Body)
		json.Unmarshal(rawBody, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notif := config.Notifications{
		SuccessURL:      server.URL,
		WebhookTimeoutD: 5 * time.Second,
	}

	Send(notif, Payload{
		Status:    "success",
		FailedIDs: nil, // should be serialized as []
	})

	if len(rawBody) == 0 {
		t.Fatal("expected webhook body")
	}
	if bytes.Contains(rawBody, []byte(`"failed_ids":null`)) {
		t.Fatalf("failed_ids serialized as null: %s", string(rawBody))
	}
	if !bytes.Contains(rawBody, []byte(`"failed_ids":[]`)) {
		t.Fatalf("failed_ids not serialized as []: %s", string(rawBody))
	}
	if receivedPayload.FailedIDs == nil {
		t.Fatal("received payload failed_ids should be non-nil")
	}
}
