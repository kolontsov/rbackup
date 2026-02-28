package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/kolontsov/rbackup/internal/config"
	"github.com/kolontsov/rbackup/internal/crypto"
	"github.com/kolontsov/rbackup/internal/storage"
)

// Status constants for receipt status fields.
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
	StatusSkipped = "skipped"
)

// Receipt is the canonical JSON receipt for a backup operation.
type Receipt struct {
	BackupID  string         `json:"backup_id"`
	Status    string         `json:"status"`
	CreatedAt string         `json:"created_at"`
	Reason    string         `json:"reason,omitempty"`
	Results   []RemoteResult `json:"results"`
}

// RemoteResult is the per-remote upload result.
type RemoteResult struct {
	Remote    string `json:"remote"`
	Key       string `json:"key"`
	VersionID string `json:"version_id"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

// BackupOptions holds runtime parameters for a backup invocation.
type BackupOptions struct {
	SealPassphrase string // required for seal:true entries
}

func failReceipt(backupID, createdAt, key string, remoteNames []string, errMsg string) Receipt {
	results := make([]RemoteResult, 0, len(remoteNames))
	if len(remoteNames) == 0 {
		results = append(results, RemoteResult{
			Key:    key,
			Status: StatusFailure,
			Error:  errMsg,
		})
	} else {
		for _, remoteName := range remoteNames {
			results = append(results, RemoteResult{
				Remote: remoteName,
				Key:    key,
				Status: StatusFailure,
				Error:  errMsg,
			})
		}
	}

	return Receipt{
		BackupID:  backupID,
		Status:    StatusFailure,
		CreatedAt: createdAt,
		Results:   results,
	}
}

// RunBackup executes a single backup entry and returns a receipt.
func RunBackup(ctx context.Context, backupID string, entry config.BackupEntry, cfg *config.Config, clients map[string]*storage.Client, opts BackupOptions) Receipt {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	key := entry.DerivedKey(cfg.Settings.Namespace)
	remoteNames := entry.ResolvedRemotes(cfg.Defaults.Remotes)

	if entry.Seal && opts.SealPassphrase == "" {
		return failReceipt(backupID, createdAt, key, remoteNames, "sealed backup requires passphrase")
	}

	// Create spool directory
	if err := os.MkdirAll(cfg.Settings.SpoolDir, 0700); err != nil {
		return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("creating spool dir: %v", err))
	}

	// Disk space preflight check
	if cfg.Settings.MinDiskFreeBytes > 0 {
		avail, err := DiskFreeBytes(cfg.Settings.SpoolDir)
		if err != nil {
			return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("checking disk space: %v", err))
		}
		if avail < uint64(cfg.Settings.MinDiskFreeBytes) {
			return failReceipt(backupID, createdAt, key, remoteNames,
				fmt.Sprintf("insufficient disk space in spool_dir: %d bytes available, min_disk_free_bytes requires %d", avail, cfg.Settings.MinDiskFreeBytes))
		}
	}

	// Create spool file
	spoolFile, err := os.CreateTemp(cfg.Settings.SpoolDir, "rbackup-*.spool")
	if err != nil {
		return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("creating spool file: %v", err))
	}
	spoolPath := spoolFile.Name()
	keepSpool := false
	defer func() {
		if !keepSpool {
			os.Remove(spoolPath)
		}
	}()

	if err := spoolFile.Chmod(0600); err != nil {
		spoolFile.Close()
		return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("setting spool permissions: %v", err))
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Build pipeline: source → encrypt (→ seal) → SHA-256 tee → spool file
	hasher := sha256.New()
	ciphertextDst := io.Writer(spoolFile)
	if cfg.Settings.MaxBackupBytes > 0 {
		ciphertextDst = &maxBytesWriter{
			w:         spoolFile,
			remaining: cfg.Settings.MaxBackupBytes,
			limit:     cfg.Settings.MaxBackupBytes,
		}
	}
	tee := io.MultiWriter(ciphertextDst, hasher)

	// Pipe: source goroutine → encrypt reader
	sourceR, sourceW := io.Pipe()
	sourceErrCh := make(chan error, 1)

	go func() {
		_, err := RunSource(runCtx, entry, cfg.Settings, sourceW)
		sourceW.CloseWithError(err)
		sourceErrCh <- err
	}()

	cr := &countReader{r: sourceR}

	var encryptErr error
	if entry.Seal {
		// Double encrypt: source → encrypt → seal → tee(sha256, spool)
		encR, encW := io.Pipe()
		encErrCh := make(chan error, 1)
		go func() {
			encErr := crypto.Encrypt(encW, cr, cfg.Encryption.AgeRecipient)
			encW.CloseWithError(encErr)
			encErrCh <- encErr
		}()
		encryptErr = crypto.Seal(tee, encR, opts.SealPassphrase)
		if encryptErr != nil {
			cancel()
			sourceR.CloseWithError(encryptErr)
			encR.CloseWithError(encryptErr)
		}
		if innerErr := <-encErrCh; innerErr != nil && encryptErr == nil {
			encryptErr = innerErr
		}
	} else {
		// Single encrypt: source → encrypt → tee(sha256, spool)
		encryptErr = crypto.Encrypt(tee, cr, cfg.Encryption.AgeRecipient)
		if encryptErr != nil {
			cancel()
			sourceR.CloseWithError(encryptErr)
		}
	}

	sourceErr := <-sourceErrCh

	if closeErr := spoolFile.Close(); closeErr != nil && encryptErr == nil {
		encryptErr = fmt.Errorf("closing spool file: %w", closeErr)
	}

	if encryptErr != nil {
		return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("encrypting: %v", encryptErr))
	}

	if sourceErr != nil {
		return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("source: %v", sourceErr))
	}

	// Zero-byte guard
	if cr.n == 0 {
		return failReceipt(backupID, createdAt, key, remoteNames, "source produced 0 bytes")
	}

	// Compute SHA-256
	sha256Hex := hex.EncodeToString(hasher.Sum(nil))

	// Sign ciphertext hash if host key is configured
	var signatureB64 string
	if cfg.Signing.HostKey != "" {
		sig, err := crypto.SignSHA256Hex(cfg.Signing.HostKey, sha256Hex)
		if err != nil {
			return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("signing: %v", err))
		}
		signatureB64 = sig
	}

	receipt := Receipt{
		BackupID:  backupID,
		Status:    StatusSuccess,
		CreatedAt: createdAt,
	}

	recipientFP, err := crypto.RecipientFingerprints(cfg.Encryption.AgeRecipient)
	if err != nil {
		return failReceipt(backupID, createdAt, key, remoteNames, fmt.Sprintf("recipient metadata: %v", err))
	}

	// Upload metadata
	metadata := map[string]string{
		"x-rbackup-sha256":                     sha256Hex,
		"x-rbackup-created-at":                 createdAt,
		crypto.RecipientMetadataFingerprintKey: recipientFP,
	}
	if signatureB64 != "" {
		metadata[crypto.SignatureMetadataKey] = signatureB64
	}

	// Parallel upload to remotes
	results := make([]RemoteResult, len(remoteNames))
	var wg sync.WaitGroup
	var mu sync.Mutex
	hasFailure := false

	for i, remoteName := range remoteNames {
		wg.Add(1)
		go func(idx int, rn string) {
			defer wg.Done()
			client, ok := clients[rn]
			result := RemoteResult{
				Remote: rn,
				Key:    key,
				Status: StatusSuccess,
			}
			if !ok {
				result.Status = StatusFailure
				result.Error = fmt.Sprintf("no client for remote %q", rn)
				mu.Lock()
				hasFailure = true
				mu.Unlock()
			} else {
				versionID, err := client.Upload(ctx, key, spoolPath, metadata)
				if err != nil {
					result.Status = StatusFailure
					result.Error = err.Error()
					mu.Lock()
					hasFailure = true
					mu.Unlock()
				} else {
					result.VersionID = versionID
				}
			}
			results[idx] = result
		}(i, remoteName)
	}
	wg.Wait()

	// Keep spool file for investigation when all remotes fail
	allFailed := len(remoteNames) > 0
	for _, r := range results {
		if r.Status == StatusSuccess {
			allFailed = false
			break
		}
	}
	if allFailed {
		keepSpool = true
		fmt.Fprintf(os.Stderr, "warning: all remotes failed, keeping spool file: %s\n", spoolPath)
	}

	receipt.Results = results
	if hasFailure {
		receipt.Status = StatusFailure
	}

	return receipt
}

// RunAll runs all configured backups sequentially, skipping manual_only entries.
func RunAll(ctx context.Context, cfg *config.Config, clients map[string]*storage.Client, opts BackupOptions) []Receipt {
	// Sort keys for deterministic order
	ids := make([]string, 0, len(cfg.Backups))
	for id := range cfg.Backups {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var receipts []Receipt
	for _, id := range ids {
		entry := cfg.Backups[id]
		if entry.ManualOnly {
			createdAt := time.Now().UTC().Format(time.RFC3339)
			key := entry.DerivedKey(cfg.Settings.Namespace)
			remoteNames := entry.ResolvedRemotes(cfg.Defaults.Remotes)
			results := make([]RemoteResult, len(remoteNames))
			for i, remoteName := range remoteNames {
				results[i] = RemoteResult{
					Remote: remoteName,
					Key:    key,
					Status: StatusSkipped,
					Error:  "manual_only",
				}
			}
			receipts = append(receipts, Receipt{
				BackupID:  id,
				Status:    StatusSkipped,
				CreatedAt: createdAt,
				Reason:    "manual_only",
				Results:   results,
			})
			continue
		}
		receipt := RunBackup(ctx, id, entry, cfg, clients, opts)
		receipts = append(receipts, receipt)
	}
	return receipts
}

// PrintReceipts outputs the canonical JSON receipt array to stdout.
func PrintReceipts(receipts []Receipt) error {
	if receipts == nil {
		receipts = []Receipt{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(receipts)
}

type countReader struct {
	r io.Reader
	n int64
}

func (cr *countReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}

type maxBytesWriter struct {
	w         io.Writer
	remaining int64
	limit     int64
}

func (mw *maxBytesWriter) Write(p []byte) (int, error) {
	if mw.remaining <= 0 {
		return 0, fmt.Errorf("max_backup_bytes exceeded (limit=%d)", mw.limit)
	}

	if int64(len(p)) <= mw.remaining {
		n, err := mw.w.Write(p)
		mw.remaining -= int64(n)
		return n, err
	}

	allowed := int(mw.remaining)
	n, err := mw.w.Write(p[:allowed])
	mw.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	if n < allowed {
		return n, io.ErrShortWrite
	}

	return n, fmt.Errorf("max_backup_bytes exceeded (limit=%d)", mw.limit)
}

// BuildClientsForRemotes creates S3 clients for the selected remote names.
func BuildClientsForRemotes(ctx context.Context, cfg *config.Config, remoteNames []string) (map[string]*storage.Client, error) {
	clients := make(map[string]*storage.Client)
	clientOpts := storage.ClientOptions{
		AttemptTimeout: cfg.Settings.RemoteTimeoutD,
		MaxAttempts:    cfg.Settings.RemoteRetriesN + 1,
	}

	seen := make(map[string]struct{}, len(remoteNames))
	for _, remoteName := range remoteNames {
		if _, ok := seen[remoteName]; ok {
			continue
		}
		seen[remoteName] = struct{}{}

		remote, err := config.FindRemote(cfg, remoteName)
		if err != nil {
			return nil, err
		}
		client, err := storage.NewClientWithOptions(ctx, *remote, clientOpts)
		if err != nil {
			return nil, fmt.Errorf("creating client for remote %q: %w", remoteName, err)
		}
		clients[remoteName] = client
	}
	return clients, nil
}

// BuildClients creates S3 clients for all configured remotes.
func BuildClients(ctx context.Context, cfg *config.Config) (map[string]*storage.Client, error) {
	remoteNames := make([]string, 0, len(cfg.Remotes))
	for _, remote := range cfg.Remotes {
		remoteNames = append(remoteNames, remote.Name)
	}
	return BuildClientsForRemotes(ctx, cfg, remoteNames)
}
