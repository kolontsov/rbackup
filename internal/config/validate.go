package config

import (
	"fmt"
	"regexp"
	"strings"

	"filippo.io/age"
	"github.com/kolontsov/rbackup/internal/crypto"
)

var identRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Validate checks all config rules from the spec.
func Validate(cfg *Config) error {
	if err := validateEncryption(&cfg.Encryption); err != nil {
		return err
	}
	if err := validateSigning(&cfg.Signing); err != nil {
		return err
	}
	if err := validateSettings(&cfg.Settings); err != nil {
		return err
	}
	if err := validateRemotes(cfg.Remotes); err != nil {
		return err
	}
	if err := validateBackups(cfg); err != nil {
		return err
	}
	return nil
}

func validateEncryption(enc *Encryption) error {
	enc.AgeRecipient = strings.TrimSpace(enc.AgeRecipient)
	if enc.AgeRecipient == "" {
		return fmt.Errorf("encryption.age_recipient is required")
	}

	recipients, err := age.ParseRecipients(strings.NewReader(enc.AgeRecipient + "\n"))
	if err != nil {
		return fmt.Errorf("encryption.age_recipient is invalid: %w", err)
	}
	if len(recipients) == 0 {
		return fmt.Errorf("encryption.age_recipient must contain at least one recipient")
	}

	seen := make(map[string]bool)
	for _, line := range strings.Split(enc.AgeRecipient, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if seen[line] {
			return fmt.Errorf("encryption.age_recipient contains duplicate: %s", line)
		}
		seen[line] = true
	}
	return nil
}

func validateSigning(s *Signing) error {
	seen := make(map[string]bool)
	var validated []string
	for _, key := range s.AllowedSigners {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, err := crypto.ParseSSHEd25519PublicKey(key); err != nil {
			return fmt.Errorf("signing.allowed_signers: %w", err)
		}
		if seen[key] {
			return fmt.Errorf("signing.allowed_signers contains duplicate key")
		}
		seen[key] = true
		validated = append(validated, key)
	}
	s.AllowedSigners = validated
	return nil
}

func validateSettings(s *Settings) error {
	ns := strings.TrimSpace(s.Namespace)
	if ns == "" {
		return fmt.Errorf("settings.namespace is required")
	}
	if !identRe.MatchString(ns) {
		return fmt.Errorf("settings.namespace contains invalid characters: %q", ns)
	}
	if s.MaxBackupBytes < 0 {
		return fmt.Errorf("settings.max_backup_bytes must be >= 0")
	}
	if s.MinDiskFreeBytes < 0 {
		return fmt.Errorf("settings.min_disk_free_bytes must be >= 0")
	}
	if s.RemoteRetriesN < 0 {
		return fmt.Errorf("settings.remote_retries must be >= 0")
	}
	s.Namespace = ns
	return nil
}

func validateRemotes(remotes []Remote) error {
	names := make(map[string]bool)
	for i := range remotes {
		r := &remotes[i]
		r.Name = strings.TrimSpace(r.Name)
		r.Type = strings.TrimSpace(r.Type)
		r.Bucket = strings.TrimSpace(r.Bucket)
		r.AWSProfile = strings.TrimSpace(r.AWSProfile)
		r.CredentialSource = strings.TrimSpace(r.CredentialSource)
		if r.Name == "" {
			return fmt.Errorf("remotes[%d].name is required", i)
		}
		if !identRe.MatchString(r.Name) {
			return fmt.Errorf("remote name %q contains invalid characters", r.Name)
		}
		if names[r.Name] {
			return fmt.Errorf("duplicate remote name: %q", r.Name)
		}
		names[r.Name] = true

		if r.Type != "s3" {
			return fmt.Errorf("remote %q: type must be 's3', got %q", r.Name, r.Type)
		}
		if r.Bucket == "" {
			return fmt.Errorf("remote %q: bucket is required", r.Name)
		}
		if r.CredentialSource != "" && r.CredentialSource != "default_chain" {
			return fmt.Errorf("remote %q: unsupported credential_source %q (allowed: default_chain)", r.Name, r.CredentialSource)
		}

		// Exactly one credential method
		methods := 0
		if r.AccessKeyID != "" || r.SecretAccessKey != "" {
			methods++
			if r.AccessKeyID == "" || r.SecretAccessKey == "" {
				return fmt.Errorf("remote %q: access_key_id and secret_access_key must both be set", r.Name)
			}
		}
		if r.AWSProfile != "" {
			methods++
		}
		if r.CredentialSource == "default_chain" {
			methods++
		}
		if methods != 1 {
			return fmt.Errorf("remote %q: exactly one credential method required (explicit keys, aws_profile, or credential_source: default_chain)", r.Name)
		}
	}
	return nil
}

func validateBackups(cfg *Config) error {
	remoteNames := make(map[string]bool)
	for _, r := range cfg.Remotes {
		remoteNames[r.Name] = true
	}

	// Validate default remote references
	defaultRemoteRefs := make(map[string]struct{}, len(cfg.Defaults.Remotes))
	for _, name := range cfg.Defaults.Remotes {
		if !remoteNames[name] {
			return fmt.Errorf("defaults.remotes references unknown remote: %q", name)
		}
		if _, exists := defaultRemoteRefs[name]; exists {
			return fmt.Errorf("defaults.remotes contains duplicate remote: %q", name)
		}
		defaultRemoteRefs[name] = struct{}{}
	}

	backupIDs := make(map[string]bool)
	derivedKeys := make(map[string]bool)
	normalizedBackups := make(map[string]BackupEntry, len(cfg.Backups))

	for id, entry := range cfg.Backups {
		trimID := strings.TrimSpace(id)
		if trimID == "" {
			return fmt.Errorf("backup entry has empty id")
		}
		if !identRe.MatchString(trimID) {
			return fmt.Errorf("backup_id %q contains invalid characters", trimID)
		}
		if backupIDs[trimID] {
			return fmt.Errorf("duplicate backup_id: %q", trimID)
		}
		backupIDs[trimID] = true

		// Exactly one source
		sources := 0
		if entry.Cmd != "" {
			sources++
		}
		if entry.File != "" {
			sources++
		}
		if entry.Dir != "" {
			sources++
		}
		if sources != 1 {
			return fmt.Errorf("backup %q: exactly one source (cmd, file, or dir) is required", trimID)
		}

		// Object name validation
		objName := strings.TrimSpace(entry.ObjectName)
		if objName == "" {
			return fmt.Errorf("backup %q: object_name is required", trimID)
		}
		if !identRe.MatchString(objName) {
			return fmt.Errorf("backup %q: object_name %q contains invalid characters", trimID, objName)
		}
		if strings.HasSuffix(objName, ".age") || strings.HasSuffix(objName, ".age2") {
			return fmt.Errorf("backup %q: object_name must not end with .age or .age2", trimID)
		}
		entry.ObjectName = objName

		// Derived key uniqueness
		dk := entry.DerivedKey(cfg.Settings.Namespace)
		if derivedKeys[dk] {
			return fmt.Errorf("duplicate derived key: %q", dk)
		}
		derivedKeys[dk] = true

		// Remote references
		entryRemotes := entry.ResolvedRemotes(cfg.Defaults.Remotes)
		if len(entryRemotes) == 0 {
			return fmt.Errorf("backup %q: no remotes configured (entry or defaults)", trimID)
		}
		entryRemoteRefs := make(map[string]struct{}, len(entryRemotes))
		for _, rn := range entryRemotes {
			if !remoteNames[rn] {
				return fmt.Errorf("backup %q: references unknown remote: %q", trimID, rn)
			}
			if _, exists := entryRemoteRefs[rn]; exists {
				return fmt.Errorf("backup %q: duplicate remote reference: %q", trimID, rn)
			}
			entryRemoteRefs[rn] = struct{}{}
		}

		normalizedBackups[trimID] = entry
	}

	cfg.Backups = normalizedBackups
	return nil
}
