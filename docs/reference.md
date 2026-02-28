# Technical Reference

Config schema and defaults: [Configuration Reference](./config.md).

## Failure Model

- Exit codes: `0` for success, `1` for failure.
- Any selected remote failure triggers exit `1`; already-uploaded objects are preserved (see [Opinionated Choices](./design.md#opinionated-choices)).
- Notification delivery is best effort and does not change exit code.

## Webhook Payload

Notifications are JSON `POST` requests to `success_url` or `failure_url`. Use the `headers` config field to pass authentication tokens or custom headers with webhook requests. `success_url` is called when all entries succeed or are skipped. `failure_url` is called when any entry has at least one remote failure. Partial success (some remotes succeed, some fail) triggers `failure_url`. Only one URL is called per run.

| Field | Type | Description |
| --- | --- | --- |
| `status` | string | `"success"` or `"failure"` |
| `total` | int | total backup entries attempted |
| `succeeded` | int | entries fully uploaded |
| `failed` | int | entries with at least one remote failure |
| `skipped` | int | entries skipped |
| `failed_ids` | string[] | backup IDs that failed |
| `receipts` | array | full receipt array (same shape as stdout) |

## Automation Contract

- Stdout: receipt JSON only (stable schema).
- Receipt shape is always a JSON array (`backup @<name>` returns array length `1`; `backup --all` returns one entry per source).
- On preflight failure (for example config parse/validation), no receipt JSON is printed.
- Stderr: logs/progress/errors.
- Use receipt fields (`backup_id` and per-result `remote` + `key` + `version_id`) for correlation; use `version_id` when you need to restore an exact object version.
- Field names are stable. Human-readable `error` strings are not a compatibility surface; automation should branch on `status`, not parse `error`.

Receipt object:

| Field | Type | Always Present | Notes |
| --- | --- | --- | --- |
| `backup_id` | string | yes | Backup entry id from config. |
| `status` | string | yes | `"success"`, `"failure"`, or `"skipped"`. |
| `created_at` | string | yes | RFC3339 UTC timestamp for the receipt. |
| `reason` | string | no | Present for skipped receipts. Current value: `"manual_only"`. |
| `results` | array | yes | Per-remote result objects. Usually one entry per selected remote. |

Per-remote result object:

| Field | Type | Always Present | Notes |
| --- | --- | --- | --- |
| `remote` | string | yes | Remote name. |
| `key` | string | yes | Object key used for upload. |
| `version_id` | string | yes | Returned object version id. Empty when versioning is unavailable, provider does not return one, upload failed, or receipt is skipped. |
| `status` | string | yes | `"success"`, `"failure"`, or `"skipped"`. |
| `error` | string | yes | Empty on success. Human-readable failure/skipped reason otherwise. Not a stable enum. |

Example receipt:

```json
[{
  "backup_id": "postgres",
  "status": "success",
  "created_at": "2025-01-15T03:00:00Z",
  "results": [
    {"remote": "s3-primary", "key": "host01/postgres.sql.gz.age", "version_id": "abc123", "status": "success", "error": ""},
    {"remote": "b2-secondary", "key": "host01/postgres.sql.gz.age", "version_id": "", "status": "success", "error": ""}
  ]
}]
```

## Object Metadata

Each uploaded backup object includes:

- `x-rbackup-sha256`: full SHA-256 of uploaded ciphertext (hex).
- `x-rbackup-created-at`: backup creation timestamp (RFC3339 UTC).
- `x-rbackup-recipient-fp`: SHA-256 fingerprint(s) (hex) of the age recipient string(s); comma-separated when multiple recipients are configured.
- `x-rbackup-signature`: base64-encoded raw Ed25519 signature (64 bytes) of the `x-rbackup-sha256` hex string. Present only when `signing.host_key` is configured.

`restore` and `verify` use recipient metadata as an early check (when available). This recipient check is only a hint. When `x-rbackup-sha256` metadata is present, `restore`/`verify` recompute the downloaded ciphertext SHA-256 and require a match before success. Signature verification behavior: see [Backup Signing](./encryption.md#backup-signing).

## Key Naming Rules

- Backup key format: `<namespace>/<object_name><suffix>`.
- Suffix: `.age` (default) or `.age2` (`seal: true`).
- `object_name` is basename only (no path separators) and must not end with `.age`/`.age2`.
- Restore/verify key is explicit input (`--key`).

## List

`rbackup list` queries remotes for existing backup objects using `HeadObject` (no `ListObjects` permission needed for default mode).

**Output formats:**
- Default: JSONL (one JSON object per line).
- `--pretty`: human-readable table.
- `--quiet`: no output, exit code only.

**Exit codes:** `0` when at least one result passes filters, `1` when no results found. This enables cron alerting: `rbackup list @postgres --max-age 25h || alert`.

**JSONL schema:**

| Field | Type | Description |
| --- | --- | --- |
| `backup` | string | Backup entry name from config. |
| `remote` | string | Remote name. |
| `key` | string | S3 object key. |
| `created_at` | string | RFC3339 UTC timestamp (from `x-rbackup-created-at` metadata, fallback to S3 `LastModified`). |
| `size_bytes` | int | Object size in bytes. |
| `version_id` | string | S3 version ID (omitted when empty). |

**Flags:**

| Flag | Description |
| --- | --- |
| `--remote <name>` | Filter to a single remote. |
| `--max-age <duration>` | Only show objects newer than this (e.g. `25h`, `3d`). |
| `--all-versions` | Show all S3 versions (requires `ListObjectVersions` permission). |
| `--quiet` | No output, exit code only. |
| `--pretty` | Human-readable table output. |

## Recovery

Operational recovery steps live in [Runbook](./runbook.md#incident-recovery).
Generate `RECOVERY.txt` with `rbackup recovery-txt` and archive the public keys or `known_hosts` file you plan to use for signature checks if signing is part of your policy.
