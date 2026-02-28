package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultCmdTimeout    = time.Hour
	DefaultRemoteTimeout = 30 * time.Minute
	DefaultRemoteRetries = 2

	DefaultWebhookTimeout = 10 * time.Second
	DefaultWebhookRetries = 2
)

type Config struct {
	Encryption    Encryption             `yaml:"encryption"`
	Signing       Signing                `yaml:"signing"`
	Settings      Settings               `yaml:"settings"`
	Remotes       []Remote               `yaml:"remotes"`
	Notifications Notifications          `yaml:"notifications"`
	Defaults      Defaults               `yaml:"defaults"`
	Backups       map[string]BackupEntry `yaml:"backups"`
}

type Signing struct {
	HostKey            string   `yaml:"host_key"`
	AllowedSigners     []string `yaml:"allowed_signers"`
	AllowedSignersFile string   `yaml:"allowed_signers_file"`
}

type Encryption struct {
	AgeRecipient string `yaml:"age_recipient"`
	IdentityFile string `yaml:"identity_file"`
}

// UnmarshalYAML accepts both a scalar string and a YAML sequence for
// age_recipient, joining list entries with newlines.
func (e *Encryption) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("encryption: expected mapping")
	}
	for i := 0; i < len(value.Content)-1; i += 2 {
		key := value.Content[i]
		val := value.Content[i+1]
		switch key.Value {
		case "age_recipient":
			switch val.Kind {
			case yaml.ScalarNode:
				e.AgeRecipient = val.Value
			case yaml.SequenceNode:
				parts := make([]string, 0, len(val.Content))
				for _, item := range val.Content {
					t := strings.TrimSpace(item.Value)
					if t != "" {
						parts = append(parts, t)
					}
				}
				e.AgeRecipient = strings.Join(parts, "\n")
			default:
				return fmt.Errorf("encryption.age_recipient: expected string or list of strings")
			}
		case "identity_file":
			e.IdentityFile = val.Value
		default:
			return fmt.Errorf("line %d: field %s not found in type config.Encryption", key.Line, key.Value)
		}
	}
	return nil
}

type Settings struct {
	CmdTimeout    string `yaml:"cmd_timeout"`
	RemoteTimeout string `yaml:"remote_timeout"`
	// Deprecated alias for remote_timeout; supported for backward compatibility.
	IOTimeout        string `yaml:"io_timeout"`
	RemoteRetries    *int   `yaml:"remote_retries"`
	LockWait         string `yaml:"lock_wait"`
	LockDir          string `yaml:"lock_dir"`
	SpoolDir         string `yaml:"spool_dir"`
	MaxBackupBytes   int64  `yaml:"max_backup_bytes"`
	MinDiskFreeBytes int64  `yaml:"min_disk_free_bytes"`
	Namespace        string `yaml:"namespace"`

	// Parsed durations (not from YAML)
	CmdTimeoutD    time.Duration `yaml:"-"`
	RemoteTimeoutD time.Duration `yaml:"-"`
	RemoteRetriesN int           `yaml:"-"`
	LockWaitD      time.Duration `yaml:"-"`
}

type Remote struct {
	Name             string `yaml:"name"`
	Type             string `yaml:"type"`
	Region           string `yaml:"region"`
	Bucket           string `yaml:"bucket"`
	Endpoint         string `yaml:"endpoint"`
	AccessKeyID      string `yaml:"access_key_id"`
	SecretAccessKey  string `yaml:"secret_access_key"`
	AWSProfile       string `yaml:"aws_profile"`
	CredentialSource string `yaml:"credential_source"`
}

type Notifications struct {
	SuccessURL      string            `yaml:"success_url"`
	FailureURL      string            `yaml:"failure_url"`
	Headers         map[string]string `yaml:"headers"`
	WebhookTimeout  string            `yaml:"webhook_timeout"`
	WebhookRetries  *int              `yaml:"webhook_retries"`
	WebhookTimeoutD time.Duration     `yaml:"-"`
	WebhookRetriesN int               `yaml:"-"`
}

type Defaults struct {
	Remotes []string `yaml:"remotes"`
}

type BackupEntry struct {
	ObjectName string   `yaml:"object_name"`
	Cmd        string   `yaml:"cmd"`
	File       string   `yaml:"file"`
	Dir        string   `yaml:"dir"`
	Seal       bool     `yaml:"seal"`
	ManualOnly bool     `yaml:"manual_only"`
	Remotes    []string `yaml:"remotes"`
}

// Suffix returns the encryption suffix for this entry.
func (e *BackupEntry) Suffix() string {
	if e.Seal {
		return ".age2"
	}
	return ".age"
}

// DerivedKey returns the full S3 object key for this entry.
func (e *BackupEntry) DerivedKey(namespace string) string {
	return namespace + "/" + e.ObjectName + e.Suffix()
}

// ResolvedRemotes returns the effective remote names for a backup entry,
// falling back to defaults if the entry doesn't specify remotes.
func (e *BackupEntry) ResolvedRemotes(defaults []string) []string {
	if len(e.Remotes) > 0 {
		return e.Remotes
	}
	return defaults
}

// FindRemote finds a remote by name in the config.
func FindRemote(cfg *Config, name string) (*Remote, error) {
	for i := range cfg.Remotes {
		if cfg.Remotes[i].Name == name {
			return &cfg.Remotes[i], nil
		}
	}
	return nil, fmt.Errorf("remote %q not found", name)
}
