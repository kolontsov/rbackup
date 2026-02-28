# Configuration Reference

Config file resolution order: `--config`, `$RBACKUP_CONFIG`, `/etc/rbackup/config.yaml`.

Secret interpolation: `${ENV_VAR}` is supported in secret-bearing string fields. Missing required env vars fail the command that needs them.

## Top-Level Sections

| Section | Required | Notes |
| --- | --- | --- |
| `encryption` | yes | Backup recipients and optional default identity file for `restore`/`verify`. |
| `signing` | no | Optional SSH host-key signing and trusted public keys for `restore`/`verify`. |
| `settings` | yes | Namespace, local runtime paths, timeouts, retries, size limits. |
| `remotes` | yes | One or more S3 remotes. |
| `notifications` | no | Optional success/failure webhooks. |
| `defaults` | no | Default remote set for backup entries. |
| `backups` | yes | Backup entries keyed by backup id. |

## `encryption`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `age_recipient` | `string` or `string[]` | yes | One or more public recipients (`age1...` or `age1pq...`). YAML lists are joined internally. |
| `identity_file` | `string` | no | Default identity file for `restore`/`verify`. CLI `--identity` overrides it. |

See [Encryption Key Model](./encryption.md) for recipient types and identity tradeoffs.

## `signing`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `host_key` | `string` | no | SSH Ed25519 private key used to sign ciphertext hashes at backup time. |
| `allowed_signers` | `string[]` | no | Explicit trusted SSH Ed25519 public keys for `restore`/`verify`. |
| `allowed_signers_file` | `string` | no | Path to a `known_hosts`-format trust file. Trusts more keys than an inline list. |

If `host_key` is set and no trusted public keys are configured, rbackup also reads `<host_key>.pub` as a single-host shortcut.

## `settings`

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | yes | none | S3 key prefix for backups. |
| `lock_dir` | `string` | backup | none | Directory for `flock` lock files. Must be set for backup commands. `config validate` warns if it is empty or invalid; missing directories are created during backup operations. |
| `spool_dir` | `string` | backup | none | Directory for local encrypted spool files. Must be set for backup commands. One full ciphertext must fit here. `config validate` warns if it is empty or invalid; missing directories are created during backup operations. |
| `cmd_timeout` | duration | no | `1h` | Timeout for `cmd` sources. |
| `remote_timeout` | duration | no | `30m` | Per-attempt timeout for remote S3 operations. |
| `io_timeout` | duration | no | none | Deprecated alias for `remote_timeout`. |
| `remote_retries` | int | no | `2` | Extra retry attempts per remote operation. Total attempts = retries + 1. |
| `lock_wait` | duration | no | `0` | Lock acquisition timeout. `0` means fail immediately. |
| `max_backup_bytes` | int64 | no | `0` | Maximum ciphertext size. `0` disables the limit. |
| `min_disk_free_bytes` | int64 | no | `0` | Fail preflight if `spool_dir` free space is below this threshold. `0` disables the check. |

## `remotes[]`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | `string` | yes | Remote identifier referenced by backup entries. |
| `type` | `string` | yes | Must be `s3`. |
| `bucket` | `string` | yes | Target bucket name. |
| `region` | `string` | no | AWS region when applicable. |
| `endpoint` | `string` | no | Custom S3 endpoint for non-AWS providers. |
| `access_key_id` | `string` | conditional | Static credential mode. Must be paired with `secret_access_key`. |
| `secret_access_key` | `string` | conditional | Static credential mode. Must be paired with `access_key_id`. |
| `aws_profile` | `string` | conditional | Named profile credential mode. |
| `credential_source` | `string` | conditional | Must be `default_chain`. |

Exactly one credential mode is allowed per remote:

- `access_key_id` + `secret_access_key`
- `aws_profile`
- `credential_source: default_chain`

See [S3 Configuration](./s3.md) for IAM scope, versioning, lifecycle, and Object Lock guidance.

## `notifications`

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `success_url` | `string` | no | none | Called when all entries succeed or are skipped. |
| `failure_url` | `string` | no | none | Called when any entry has a remote failure. |
| `headers` | `map[string]string` | no | none | Extra HTTP headers for webhook requests. |
| `webhook_timeout` | duration | no | `10s` | Per-attempt webhook timeout. |
| `webhook_retries` | int | no | `2` | Extra retry attempts. Total attempts = retries + 1. |

Webhook JSON format is in [Technical Reference](./reference.md#webhook-payload).

## `defaults`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `remotes` | `string[]` | no | Default remotes used when a backup entry omits its own `remotes`. |

## `backups.<id>`

Each map key under `backups` is the backup id used in `backup @<id>` and in receipts.

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `object_name` | `string` | yes | File name only; rbackup appends `.age` or `.age2`. |
| `cmd` | `string` | conditional | Command source. Exactly one of `cmd`, `file`, or `dir` is required. |
| `file` | `string` | conditional | File source. Exactly one of `cmd`, `file`, or `dir` is required. |
| `dir` | `string` | conditional | Directory source. Archived as `tar.gz` before encryption. |
| `seal` | `bool` | no | Adds an outer passphrase layer (`.age2`). |
| `manual_only` | `bool` | no | Skipped by `backup --all`. |
| `remotes` | `string[]` | no | Per-entry remote override. Falls back to `defaults.remotes`. |

## Source Contract

- `cmd`: executed as `/bin/sh -c <cmd>`. Only stdout becomes backup data; stderr is passed through to the operator. Restore output is the raw stdout byte stream produced by the command.
- `file`: copies the file's raw bytes as-is. Restore output is byte-for-byte file content.
- `dir`: archives the directory as `tar.gz` before encryption. The archive root is `basename(dir)`, and symlinks are preserved as symlinks in the tar stream.

## Validation Rules

- `settings.namespace`, remote names, backup ids, and `object_name` use `[A-Za-z0-9._-]+`.
- `object_name` must not contain path separators and must not end with `.age` or `.age2`.
- Each backup entry must resolve to at least one remote.
- Duplicate derived keys are rejected after namespace + suffix expansion.
