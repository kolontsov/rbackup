# Design Decisions

## Opinionated Choices

- Config-first model: no ad-hoc backup source flags, because checked-in config ensures reproducibility and reduces operator error.
- Public-key-first encryption model: [age](https://age-encryption.org) recipients (see [Encryption Key Model](./encryption.md)). Backup hosts already hold secrets (S3 credentials); the goal isn't zero secrets on hosts but reducing what a leak can expose: S3 credentials can be limited to write-only access to one bucket and rotated or revoked independently, while a leaked age decryption key exposes all past and future backup contents.
- Public-key encryption keeps the decryption key off backup hosts while allowing recipients to live in cleartext — config files, Ansible, Salt, git.
- Optional SSH host key signing: covers the threat where an attacker has S3 write access and the public age recipient — without signing, they can forge a backup that decrypts successfully. Signing uses the host's existing SSH Ed25519 key, so there is no additional key management. When trusted public keys are configured (explicitly or via `<host_key>.pub` auto-resolution), restore/verify is fail-closed: it requires object metadata and a valid signature from one of those public keys. See [Backup Signing](./encryption.md#backup-signing) for the key-selection rules.
- Fixed `derive-key` Argon2id profile (`t=4`, `m=256 MiB`, `p=4`) with deterministic salt (`rbackup-key-v1-<org>`), because deterministic re-derivation is an operator-chosen recovery path when identity-file loss is a material risk; derivation is a recovery-time operation, so the cost is acceptable. The `v1` salt prefix allows future cost profiles without breaking existing derivations.
- Encrypt once, upload in parallel: produce one local spool file, then upload to each remote, because this preserves hash verification and isolated per-remote retries. The spool file is removed after upload when at least one remote succeeds or on pre-upload failure (source, encryption, or disk); on total remote failure, the spool is retained for operator investigation.
- Backup fails on zero-byte source output, because empty payloads usually signal source failure.
- Partial remote success is kept (exit code is still `1`; already-uploaded objects are not rolled back), because already-uploaded copies are valid and usable, and destructive rollback is riskier than explicit retry.
- Retention and object locking belong in the bucket, because object policy belongs in bucket controls.
- Minimal S3 key-path validation for restore/verify, because object-path constraints are left to bucket policy and IAM.
- Auto-resolve `<host_key>.pub` as allowed signer when no explicit signers are configured, because single-host setups (same machine backs up and restores) should work without manually extracting the SSH public key. The `.pub` file follows standard SSH convention and is already present on any host with an SSH key.
- `backup --all` runs entries sequentially, because parallel entry execution adds scheduling complexity and spool-disk contention without meaningful benefit at the intended scale.

## Non-Goals

- No deduplication, pruning engine, or retention DSL, because dedupe needs its own cleanup and garbage-collection rules, complicates manual restore, and full-object overhead is acceptable at the intended scale (typically sub-GB backups like database dumps, config exports, and vault exports).
- No implicit key/source discovery at restore time, because explicit `--remote` and `--key` keep incident recovery predictable.
