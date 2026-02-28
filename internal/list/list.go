package list

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/aws/smithy-go"
	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/storage"
)

// Entry represents a single backup object found on a remote.
type Entry struct {
	Backup    string    `json:"backup"`
	Remote    string    `json:"remote"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int64     `json:"size_bytes"`
	VersionID string    `json:"version_id,omitempty"`
}

// Options controls list behavior.
type Options struct {
	MaxAge      time.Duration
	AllVersions bool
}

// List queries remotes for backup objects and returns matching entries.
// Missing objects are silently skipped.
func List(ctx context.Context, cfg *config.Config, clients map[string]*storage.Client,
	entryFilter string, remoteFilter string, opts Options) ([]Entry, error) {

	now := time.Now()
	var entries []Entry

	backupNames := sortedBackupNames(cfg)
	for _, name := range backupNames {
		if entryFilter != "" && name != entryFilter {
			continue
		}
		be := cfg.Backups[name]
		key := be.DerivedKey(cfg.Settings.Namespace)

		remoteNames := be.ResolvedRemotes(cfg.Defaults.Remotes)
		for _, remoteName := range remoteNames {
			if remoteFilter != "" && remoteName != remoteFilter {
				continue
			}
			client, ok := clients[remoteName]
			if !ok {
				continue
			}

			if opts.AllVersions {
				vEntries, err := listVersions(ctx, client, name, remoteName, key, now, opts)
				if err != nil {
					return nil, err
				}
				entries = append(entries, vEntries...)
			} else {
				e, err := headLatest(ctx, client, name, remoteName, key, now, opts)
				if err != nil {
					return nil, err
				}
				if e != nil {
					entries = append(entries, *e)
				}
			}
		}
	}

	return entries, nil
}

func headLatest(ctx context.Context, client *storage.Client, backupName, remoteName, key string,
	now time.Time, opts Options) (*Entry, error) {

	hr, err := client.Head(ctx, key, "")
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	createdAt := extractCreatedAt(hr)
	if opts.MaxAge > 0 && now.Sub(createdAt) > opts.MaxAge {
		return nil, nil
	}

	return &Entry{
		Backup:    backupName,
		Remote:    remoteName,
		Key:       key,
		CreatedAt: createdAt,
		SizeBytes: hr.ContentLength,
		VersionID: hr.VersionID,
	}, nil
}

func listVersions(ctx context.Context, client *storage.Client, backupName, remoteName, key string,
	now time.Time, opts Options) ([]Entry, error) {

	versions, err := client.ListVersions(ctx, key)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, v := range versions {
		hr, err := client.Head(ctx, key, v.VersionID)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}

		createdAt := extractCreatedAt(hr)
		if opts.MaxAge > 0 && now.Sub(createdAt) > opts.MaxAge {
			continue
		}

		entries = append(entries, Entry{
			Backup:    backupName,
			Remote:    remoteName,
			Key:       key,
			CreatedAt: createdAt,
			SizeBytes: hr.ContentLength,
			VersionID: hr.VersionID,
		})
	}
	return entries, nil
}

func extractCreatedAt(hr *storage.HeadResult) time.Time {
	if v, ok := hr.Metadata["x-rbackup-created-at"]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	if !hr.LastModified.IsZero() {
		return hr.LastModified
	}
	return time.Time{}
}

func isNotFound(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		code := ae.ErrorCode()
		return code == "NotFound" || code == "NoSuchKey" || code == "404"
	}
	return false
}

func sortedBackupNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Backups))
	for name := range cfg.Backups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
