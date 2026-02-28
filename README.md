# rbackup

Opinionated single-binary tool for encrypted offsite backups of files, directories, and command output to multiple S3-compatible remotes.

Canonical repository: <https://github.com/kolontsov/rbackup>

Status: hobby project, provided as-is with no guarantee. Developed with Claude Code and Codex.

## Fit

Use rbackup when:

- You already have the data you want to back up (`pg_dump` output, exported files, config directories) and want it encrypted and pushed to one or more S3 remotes.
- You want restore to use an exact remote and object key, with machine-readable receipts, not browse/discovery logic at restore time.
- One full encrypted object fits in local `spool_dir`; rbackup stages ciphertext locally before upload.

Do not use rbackup when:

- You need snapshots, deduplication, pruning, browse-and-restore, or partial-file restore.
- You cannot afford local staging and need direct streaming from source to remote storage.
- You need to run lots of backups in parallel; `backup --all` runs sequentially by design.

## Minimal Path

Generate an identity, put the printed recipient into config, run a backup, then restore by explicit key:

```bash
mkdir -p ~/.config/rbackup
age-keygen -o ~/.config/rbackup/identity.txt
age-keygen -y ~/.config/rbackup/identity.txt
chmod 600 ~/.config/rbackup/identity.txt
```

```yaml
encryption:
  age_recipient: "age1..."
  identity_file: /abs/path/to/identity.txt

signing:
  host_key: /etc/ssh/ssh_host_ed25519_key

settings:
  namespace: host01
  lock_dir: /var/tmp/rbackup-lock
  spool_dir: /var/tmp/rbackup-spool

remotes:
  - name: primary
    type: s3
    bucket: my-backups
    region: us-east-1
    credential_source: default_chain

defaults:
  remotes: [primary]

backups:
  postgres:
    object_name: postgres.sql.gz
    cmd: "pg_dumpall --clean | gzip"
```

Save that YAML as `/etc/rbackup/config.yaml`, or pass `--config /path/to/config.yaml` in the commands below.

The `host_key` example above uses the common Linux host-key path. On the same host, rbackup also reads `/etc/ssh/ssh_host_ed25519_key.pub` as a trusted signer. For cross-host or long-term recovery, prefer explicit `signing.allowed_signers` or a dedicated `signing.allowed_signers_file`.

```bash
rbackup config validate
rbackup backup @postgres
rbackup restore --remote primary --key host01/postgres.sql.gz.age \
  --identity ~/.config/rbackup/identity.txt \
  -o /restore/postgres.sql.gz
```

For `cmd` sources, stdout becomes the backup data; stderr stays operator-visible. See [docs/config.md](./docs/config.md#source-contract) for the exact source contract.

## Features

- Config-defined sources: `cmd`, `file`, `dir`.
- Age recipient encryption (X25519 and ML-KEM-768/X25519; see [Encryption Key Model](./docs/encryption.md)).
- Optional backup signing with the host's SSH key.
- Optional seal layer (`.age2`): passphrase over age-encrypted data, via `--seal-passphrase-file` or interactive prompt.
- JSON receipts with stable field names and per-remote status (see [Reference](./docs/reference.md#automation-contract)).
- S3 object metadata for checks before restore (see [Object Metadata](./docs/reference.md#object-metadata)).
- Optional `derive-key` mode for recovery-focused setups, with `--validate`.
- `recovery-txt` for recovery instructions, with optional best-effort upload to all remotes (`--upload`).
- `config validate` and `config test` (`--read`, `--write`).

See also: [Opinionated Choices](./docs/design.md#opinionated-choices), [Non-Goals](./docs/design.md#non-goals).

## Build and Install

### Install from release (recommended)

Download the latest prebuilt binary from GitHub Releases:

- <https://github.com/kolontsov/rbackup/releases/latest>

Each release includes the platform binaries plus a signed checksum manifest:

- `rbackup-<os>-<arch>`
- `SHA256SUMS`
- `SHA256SUMS.sigstore.json`

Recommended: verify `SHA256SUMS` with Cosign keyless policy first (replace tag):

```bash
TAG=v1.2.3
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "https://github.com/kolontsov/rbackup/.github/workflows/release.yml@refs/tags/${TAG}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  SHA256SUMS
```

Then verify the downloaded binary against the signed manifest:

```bash
FILE=rbackup-linux-amd64
grep " ${FILE}$" SHA256SUMS | sha256sum -c -
```

Then place the binary in your `PATH`:

```bash
sudo install -m 0755 ./rbackup-linux-amd64 /usr/local/bin/rbackup
```

On macOS, replace `sha256sum` with `shasum -a 256` in the command above.

### Build from source

With `just`:

```bash
just build
```

This generates `rbackup` and `SHA256SUMS`.

With Go directly:

```bash
CGO_ENABLED=0 go build -o rbackup .
sha256sum rbackup > SHA256SUMS
```

On macOS, replace `sha256sum` with `shasum -a 256`.

Build all release targets:

```bash
just build-all
```

This generates all target binaries plus `SHA256SUMS`.

After building, install it to `/usr/local/bin`:

```bash
sudo install -m 0755 rbackup /usr/local/bin/rbackup
```

### Run tests

Unit tests:

```bash
just test
```

Integration tests (requires Docker):

```bash
just test-integration
```

All tests:

```bash
just test-all
```

### Coverage

Unit coverage:

```bash
just coverage-unit
```

Integration coverage (`-coverpkg=./...`):

```bash
just coverage-integration
```

Run both coverage suites:

```bash
just coverage
```

## Prerequisites

- S3-compatible bucket and credentials for each remote. Each remote uses exactly one credential mode: explicit keys, `aws_profile`, or `credential_source: default_chain`.
- S3 Versioning required for `--version-id` restore of an exact object version; recommended even without it for rollback safety. See [S3 Configuration](./docs/s3.md) for IAM, Object Lock, and Lifecycle guidance.
- Writable `spool_dir` and `lock_dir` (set in config).
- Enough free space in `spool_dir` for one full encrypted object plus headroom; uploads happen after local spooling, and total remote failure retains the ciphertext spool for investigation.
- Secret env vars set for `${ENV_VAR}` fields.

## Configuration

YAML config file, resolved in order: `--config` flag, `$RBACKUP_CONFIG` env var, `/etc/rbackup/config.yaml`.
See [docs/config.md](./docs/config.md) for schema, defaults, and validation rules.

## Commands

- `backup @<name>` / `backup --all`: encrypt and upload.
- `list [@name]`: query remotes for existing backup objects. JSONL output by default; `--pretty` for table, `--quiet` for exit-code only. Supports `--remote`, `--max-age`, `--all-versions`. Exit code 1 when no results pass filters.
- `restore --remote <remote> --key <key> --identity <identity> --output <path>`: decrypt and restore (optional `--version-id` to restore an exact object version, `--force` to overwrite existing output).
- `verify --remote <remote> --key <key> --identity <identity>`: verify by downloading and decrypting to an internal sink (`/dev/null` equivalent; optional `--version-id`).
- `derive-key --org <org>`: derive the same key from the same passphrase + org each time (optional `--pq`, `--validate`).
- `recovery-txt`: generate recovery instructions (optional `--upload`).
- `config validate` / `config test`: check config and remote connectivity.

Where applicable, flags fall back to config equivalents; `--identity` overrides `encryption.identity_file`.
Target selectors remain explicit (`--remote`, `--key`, `--output` for restore).

See [docs/runbook.md](./docs/runbook.md) for operational procedures and command examples.

## Troubleshooting

| Symptom | Likely Cause | Fix |
| --- | --- | --- |
| `source produced 0 bytes` failure | broken export/input | fix source command/file generation |
| secure file permission error | key/passphrase file too open or symlink | use regular file, restrict to owner (`600`) |
| lock contention | another backup running in the same namespace (`flock` lock) | wait, tune `lock_wait`, or run in different namespace |
| missing secret env var | `${ENV_VAR}` not set | export required variable before run |
| insufficient disk space in spool_dir | filesystem full or below `min_disk_free_bytes` threshold | free disk space or adjust threshold |
| `warning: all remotes failed, keeping spool file: ...` | every selected remote upload failed; ciphertext spool was retained locally | inspect/retry, then remove retained spool file when no longer needed |
| restore mismatch across remotes | used `latest` after partial failure history | restore with receipt `--version-id` |

## Related Docs

- [docs/runbook.md](./docs/runbook.md) — operational procedures and command examples
- [docs/config.md](./docs/config.md) — config schema, defaults, validation rules
- [docs/encryption.md](./docs/encryption.md) — encryption key model
- [docs/reference.md](./docs/reference.md) — webhook payload, automation contract, object metadata, key naming, failure model
- [docs/s3.md](./docs/s3.md) — S3 IAM policies, versioning, lifecycle
- [docs/design.md](./docs/design.md) — opinionated choices and non-goals
- [examples/](./examples/README.md) — reference config, cron example, and systemd notes

## License

MIT — see [LICENSE](./LICENSE).
