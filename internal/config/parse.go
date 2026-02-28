package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Load reads and parses a config file, then validates it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return Parse(data)
}

// Parse parses YAML bytes into a Config and validates it.
func Parse(data []byte) (*Config, error) {
	var cfg Config

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parsing config: multiple YAML documents are not supported")
		}
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := parseDurations(&cfg); err != nil {
		return nil, err
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func parseDurations(cfg *Config) error {
	var err error

	if cfg.Settings.CmdTimeout == "" {
		cfg.Settings.CmdTimeoutD = DefaultCmdTimeout
	} else {
		cfg.Settings.CmdTimeoutD, err = time.ParseDuration(cfg.Settings.CmdTimeout)
		if err != nil {
			return fmt.Errorf("invalid cmd_timeout: %w", err)
		}
	}
	if cfg.Settings.CmdTimeoutD <= 0 {
		return fmt.Errorf("invalid cmd_timeout: must be > 0")
	}

	switch {
	case cfg.Settings.RemoteTimeout != "" && cfg.Settings.IOTimeout != "":
		return fmt.Errorf("remote_timeout and io_timeout cannot both be set")
	case cfg.Settings.RemoteTimeout != "":
		cfg.Settings.RemoteTimeoutD, err = time.ParseDuration(cfg.Settings.RemoteTimeout)
		if err != nil {
			return fmt.Errorf("invalid remote_timeout: %w", err)
		}
	case cfg.Settings.IOTimeout != "":
		cfg.Settings.RemoteTimeoutD, err = time.ParseDuration(cfg.Settings.IOTimeout)
		if err != nil {
			return fmt.Errorf("invalid io_timeout: %w", err)
		}
	default:
		cfg.Settings.RemoteTimeoutD = DefaultRemoteTimeout
	}
	if cfg.Settings.RemoteTimeoutD <= 0 {
		return fmt.Errorf("invalid remote_timeout: must be > 0")
	}

	if cfg.Settings.RemoteRetries == nil {
		cfg.Settings.RemoteRetriesN = DefaultRemoteRetries
	} else {
		cfg.Settings.RemoteRetriesN = *cfg.Settings.RemoteRetries
	}
	if cfg.Settings.RemoteRetriesN < 0 {
		return fmt.Errorf("invalid remote_retries: must be >= 0")
	}

	if cfg.Settings.LockWait != "" {
		cfg.Settings.LockWaitD, err = time.ParseDuration(cfg.Settings.LockWait)
		if err != nil {
			// lock_wait: 0 is valid shorthand for "0s"
			return fmt.Errorf("invalid lock_wait: %w", err)
		}
	}
	if cfg.Settings.LockWaitD < 0 {
		return fmt.Errorf("invalid lock_wait: must be >= 0")
	}

	if cfg.Notifications.WebhookTimeout == "" {
		cfg.Notifications.WebhookTimeoutD = DefaultWebhookTimeout
	} else {
		cfg.Notifications.WebhookTimeoutD, err = time.ParseDuration(cfg.Notifications.WebhookTimeout)
		if err != nil {
			return fmt.Errorf("invalid webhook_timeout: %w", err)
		}
	}
	if cfg.Notifications.WebhookTimeoutD <= 0 {
		return fmt.Errorf("invalid webhook_timeout: must be > 0")
	}

	if cfg.Notifications.WebhookRetries == nil {
		cfg.Notifications.WebhookRetriesN = DefaultWebhookRetries
	} else {
		cfg.Notifications.WebhookRetriesN = *cfg.Notifications.WebhookRetries
	}
	if cfg.Notifications.WebhookRetriesN < 0 {
		return fmt.Errorf("invalid webhook_retries: must be >= 0")
	}

	return nil
}
