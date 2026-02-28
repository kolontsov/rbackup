package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/kolontsov/rbackup/internal/config"
)

// Payload is the webhook notification payload.
type Payload struct {
	Status    string      `json:"status"`
	Total     int         `json:"total"`
	Succeeded int         `json:"succeeded"`
	Failed    int         `json:"failed"`
	Skipped   int         `json:"skipped"`
	FailedIDs []string    `json:"failed_ids"`
	Receipts  any `json:"receipts"`
}

// Send sends a webhook notification. Best effort: errors are logged to stderr.
func Send(notif config.Notifications, payload Payload) {
	url := notif.SuccessURL
	if payload.Status == "failure" {
		url = notif.FailureURL
	}
	if url == "" {
		return
	}

	if payload.FailedIDs == nil {
		payload.FailedIDs = []string{}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: notification marshal error: %v\n", err)
		return
	}

	timeout := notif.WebhookTimeoutD
	if timeout == 0 {
		timeout = config.DefaultWebhookTimeout
	}

	retries := notif.WebhookRetriesN

	client := &http.Client{Timeout: timeout}

	attempts := retries + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: notification request error: %v\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		for k, v := range notif.Headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			if attempt == attempts {
				fmt.Fprintf(os.Stderr, "warning: notification delivery error after %d attempts: %v\n", attempts, err)
				return
			}
			time.Sleep(webhookRetryDelay(attempt))
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		// 2xx/3xx = delivered. 4xx = permanent failure. 5xx = retryable.
		if resp.StatusCode < 400 {
			return
		}
		if resp.StatusCode < 500 {
			fmt.Fprintf(os.Stderr, "warning: notification returned status %d\n", resp.StatusCode)
			return
		}
		if attempt == attempts {
			fmt.Fprintf(os.Stderr, "warning: notification returned status %d after %d attempts\n", resp.StatusCode, attempts)
			return
		}
		time.Sleep(webhookRetryDelay(attempt))
	}
}

func webhookRetryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return time.Second
	}
	d := time.Second << (attempt - 1)
	if d > 10*time.Second {
		return 10 * time.Second
	}
	return d
}
