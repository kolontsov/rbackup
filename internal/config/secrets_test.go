package config

import (
	"strings"
	"testing"
)

func TestResolveSecretsHappyPath(t *testing.T) {
	t.Setenv("TEST_KEY", "resolved-key")
	t.Setenv("TEST_SECRET", "resolved-secret")
	t.Setenv("TEST_URL", "https://example.com/hook")
	t.Setenv("TEST_TOKEN", "bearer-token")

	cfg := &Config{
		Remotes: []Remote{
			{
				Name:            "r1",
				AccessKeyID:     "${TEST_KEY}",
				SecretAccessKey: "${TEST_SECRET}",
			},
		},
		Notifications: Notifications{
			SuccessURL: "${TEST_URL}",
			FailureURL: "${TEST_URL}",
			Headers: map[string]string{
				"Authorization": "Bearer ${TEST_TOKEN}",
			},
		},
	}

	if err := ResolveSecrets(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Remotes[0].AccessKeyID != "resolved-key" {
		t.Errorf("access_key_id = %q", cfg.Remotes[0].AccessKeyID)
	}
	if cfg.Remotes[0].SecretAccessKey != "resolved-secret" {
		t.Errorf("secret_access_key = %q", cfg.Remotes[0].SecretAccessKey)
	}
	if cfg.Notifications.SuccessURL != "https://example.com/hook" {
		t.Errorf("success_url = %q", cfg.Notifications.SuccessURL)
	}
	if cfg.Notifications.Headers["Authorization"] != "Bearer bearer-token" {
		t.Errorf("header = %q", cfg.Notifications.Headers["Authorization"])
	}
}

func TestResolveSecretsRemoteCredError(t *testing.T) {
	cfg := &Config{
		Remotes: []Remote{
			{
				Name:            "r1",
				AccessKeyID:     "${NONEXISTENT_KEY}",
				SecretAccessKey: "literal",
			},
		},
	}

	err := ResolveSecrets(cfg)
	if err == nil {
		t.Fatal("expected error for unresolved remote cred")
	}
	if !strings.Contains(err.Error(), "NONEXISTENT_KEY") {
		t.Errorf("error should mention var name: %v", err)
	}
}

func TestResolveSecretsNotificationURLCleared(t *testing.T) {
	cfg := &Config{
		Notifications: Notifications{
			SuccessURL: "${UNSET_URL}",
			FailureURL: "https://literal.com",
		},
	}

	if err := ResolveSecrets(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Notifications.SuccessURL != "" {
		t.Errorf("success_url should be cleared, got %q", cfg.Notifications.SuccessURL)
	}
	if cfg.Notifications.FailureURL != "https://literal.com" {
		t.Errorf("failure_url = %q", cfg.Notifications.FailureURL)
	}
}

func TestResolveSecretsHeaderRemoved(t *testing.T) {
	cfg := &Config{
		Notifications: Notifications{
			Headers: map[string]string{
				"Authorization": "Bearer ${UNSET_TOKEN}",
				"X-Custom":      "literal-value",
			},
		},
	}

	if err := ResolveSecrets(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.Notifications.Headers["Authorization"]; ok {
		t.Error("Authorization header should be removed")
	}
	if cfg.Notifications.Headers["X-Custom"] != "literal-value" {
		t.Errorf("X-Custom = %q", cfg.Notifications.Headers["X-Custom"])
	}
}

func TestResolveSecretsPassthrough(t *testing.T) {
	cfg := &Config{
		Remotes: []Remote{
			{
				Name:       "r1",
				AWSProfile: "myprofile",
			},
		},
		Notifications: Notifications{
			SuccessURL: "https://literal.com",
		},
	}

	if err := ResolveSecrets(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Notifications.SuccessURL != "https://literal.com" {
		t.Errorf("literal URL should pass through, got %q", cfg.Notifications.SuccessURL)
	}
}

func TestResolveRemoteSecretsOnlySelectedRemote(t *testing.T) {
	t.Setenv("GOOD_KEY", "resolved-good-key")
	t.Setenv("GOOD_SECRET", "resolved-good-secret")

	remote := &Remote{
		Name:            "good-remote",
		AccessKeyID:     "${GOOD_KEY}",
		SecretAccessKey: "${GOOD_SECRET}",
	}

	if err := ResolveRemoteSecrets(remote); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remote.AccessKeyID != "resolved-good-key" {
		t.Errorf("access_key_id = %q", remote.AccessKeyID)
	}
	if remote.SecretAccessKey != "resolved-good-secret" {
		t.Errorf("secret_access_key = %q", remote.SecretAccessKey)
	}
}

func TestResolveSelectedRemoteSecretsIgnoresUnselected(t *testing.T) {
	t.Setenv("GOOD_KEY", "resolved-good-key")
	t.Setenv("GOOD_SECRET", "resolved-good-secret")

	cfg := &Config{
		Remotes: []Remote{
			{
				Name:            "selected",
				AccessKeyID:     "${GOOD_KEY}",
				SecretAccessKey: "${GOOD_SECRET}",
			},
			{
				Name:            "unused-broken",
				AccessKeyID:     "${MISSING_KEY}",
				SecretAccessKey: "literal",
			},
		},
	}

	if err := ResolveSelectedRemoteSecrets(cfg, []string{"selected"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Remotes[0].AccessKeyID != "resolved-good-key" {
		t.Errorf("selected access_key_id = %q", cfg.Remotes[0].AccessKeyID)
	}
}

func TestResolveSelectedRemoteSecretsUnknownRemote(t *testing.T) {
	cfg := &Config{
		Remotes: []Remote{
			{Name: "known"},
		},
	}

	if err := ResolveSelectedRemoteSecrets(cfg, []string{"unknown"}); err == nil {
		t.Fatal("expected unknown remote error")
	}
}
