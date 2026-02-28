# rbackup Runbook

Operational commands for the single static rbackup binary, using [age](https://age-encryption.org) public-key encryption (`.age`) and optional seal layer (`.age2`) across configured remotes.

Install note:
- Recommended: use the latest release binary: <https://github.com/kolontsov/rbackup/releases/latest>
- Alternative: build from source with `just`/`go build` from [README Build and Install](../README.md#build-and-install)

## Setup

1. Choose how you will manage the decryption key — see [Encryption Key Model](./encryption.md) for rationale.

Random key file:

```bash
mkdir -p ~/.config/rbackup
age-keygen -o ~/.config/rbackup/identity.txt
age-keygen -y ~/.config/rbackup/identity.txt
chmod 600 ~/.config/rbackup/identity.txt
```

Derived key file (recoverable from passphrase + org):

```bash
mkdir -p ~/.config/rbackup
rbackup derive-key --org acme-prod -o ~/.config/rbackup/identity.txt
chmod 600 ~/.config/rbackup/identity.txt
```

Use `age-keygen -pq` or `rbackup derive-key --pq` for hybrid ML-KEM-768/X25519 recipients.
`derive-key --pq` keeps the same recovery tradeoff: the passphrase and Argon2id remain the security boundary.

Set `encryption.age_recipient` to one or more public keys (`age1...` or `age1pq...`, YAML string or list) and keep the identity secret file(s) private.
Only `restore`/`verify` need the identity secret; backups need the public recipient.

2. Generate recovery instructions (and optionally upload them to all remotes).

```bash
rbackup recovery-txt --org acme-prod -o RECOVERY.txt
rbackup recovery-txt --org acme-prod --upload
# optional custom object key:
rbackup recovery-txt --org acme-prod --upload --upload-key host01/RECOVERY.txt
```

`recovery-txt --upload` is best effort: upload errors are printed as warnings and do not change exit code.

3. Validate config and remote access.

```bash
rbackup config validate
rbackup config test --write
```

`config test --write` uploads a probe object to each remote (it does not delete probes).
Probe objects are not deleted automatically; remove manually or via S3 lifecycle rule.
Use `--write-key` to override the default probe object key.

If your backup role also has bucket-level read access and you want to test basic bucket reachability:

```bash
rbackup config test --read --write
```

`config test --read` only performs `HeadBucket`. It does not validate `GetObject`, `GetObjectVersion`, prefix-scoped object permissions, or actual `restore`/`verify` behavior.

## Backup

Single entry:

```bash
rbackup backup @postgres
```

All entries:

```bash
rbackup backup --all
```

Sealed entry (passphrase from file):

```bash
rbackup backup @vault-export --seal-passphrase-file /path/to/pass.txt
```

Notes:
- `backup --all` skips `manual_only: true`.
- `backup --all` runs entries sequentially; total time is sum of all entry backups.
- Source contract: `cmd` runs via `/bin/sh -c` and backs up stdout only; `file` copies raw bytes; `dir` restores as `tar.gz` rooted at the source basename and preserves symlinks.
- If exit code is `1`, inspect receipt `results[]`: successful remotes are already committed.
- Use receipt `remote + key + version_id` to restore the exact object version and retry only the failed remotes.
- If all selected remotes fail, rbackup keeps the local ciphertext spool file in `spool_dir` for investigation; repeated failures can accumulate disk usage until you clean retained spools.
- Automation contract details: [Automation Contract](./reference.md#automation-contract).
- Timeout/retry defaults: [Configuration Reference](./config.md#settings).

## Verify

```bash
rbackup verify --remote s3-primary --key host01/postgres.sql.gz.age --identity ~/.config/rbackup/identity.txt
```

If trusted public keys are configured (`signing.allowed_signers`, `signing.allowed_signers_file`, or auto-resolved `<host_key>.pub`), verify/restore is strict: object metadata must be present, the downloaded ciphertext SHA-256 must match `x-rbackup-sha256`, and `x-rbackup-signature` must match one of those keys. Unsigned objects, hash mismatch, signature mismatch, or metadata check failure abort before success.
For cross-host or long-term recovery, prefer explicit `allowed_signers` or a dedicated archived `known_hosts` file. A generic workstation `known_hosts` file is a shortcut, not deterministic policy.

Sealed object (`.age2`):

```bash
rbackup verify --remote s3-primary --key host01/vault-export.json.age2 \
  --identity ~/.config/rbackup/identity.txt \
  --seal-passphrase-file /path/to/pass.txt
```

## Restore

```bash
rbackup restore --remote s3-primary --key host01/postgres.sql.gz.age \
  --identity ~/.config/rbackup/identity.txt \
  -o /restore/postgres.sql.gz
```

Deterministic restore (preferred in incidents):

```bash
rbackup restore --remote s3-primary --key host01/postgres.sql.gz.age \
  --version-id <from-receipt> \
  --identity ~/.config/rbackup/identity.txt \
  -o /restore/postgres.sql.gz
```

Overwrite an existing output file:

```bash
rbackup restore --remote s3-primary --key host01/postgres.sql.gz.age \
  --identity ~/.config/rbackup/identity.txt \
  --force -o /restore/postgres.sql.gz
```

For backups created from `dir` sources, the restored file is a `tar.gz` archive; extract it after restore.

## Passphrase Validation (No Identity File Write)

This applies to deterministic `derive-key` workflows.

```bash
rbackup derive-key --org acme-prod --validate --recipient age1...
rbackup derive-key --org acme-prod --pq --validate --recipient age1pq...
```

## List and Monitor

Check what backups exist on remotes:

```bash
rbackup list
rbackup list @postgres
rbackup list --pretty
rbackup list @postgres --remote s3-primary --max-age 25h
rbackup list @postgres --all-versions
```

Cron-based monitoring (alert if no backup younger than 25 hours):

```bash
# crontab entry
0 * * * * rbackup list @postgres --max-age 25h --quiet || curl -s https://alerts.example.com/backup-stale
```

## Incident Recovery

1. Identify `remote + key + version_id` from receipt or bucket inspection.
2. Obtain the identity: use the stored file, or re-derive/validate if you use `derive-key`.
3. Restore with explicit `--remote --key --version-id`.
4. Check the restored data in the app that uses it.
5. Preserve the config snapshot, receipts, and the public keys or `known_hosts` file you used to verify signatures.

Manual bucket download is valid, but rbackup cannot enforce these checks in that path.
If signing is part of your policy, compare the downloaded ciphertext SHA-256 to `x-rbackup-sha256`, then verify `x-rbackup-signature` over that hash before decryption.
For `dir` backups, extract the resulting `tar.gz` after restore or manual decryption.
