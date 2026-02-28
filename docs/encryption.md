# Encryption Key Model

The backup host has plaintext access to source data but should not hold decryption secrets; [age](https://age-encryption.org) public-key encryption enforces this separation.

- `encryption.age_recipient` accepts one or more public keys used for backup encryption (YAML string or list). Multiple recipients allow decryption with any corresponding identity (e.g. primary admin key + recovery key).
- Supports X25519 (`age1...`) and hybrid ML-KEM-768/X25519 (`age1pq...`) recipients.
- The identity secret (`AGE-SECRET-KEY-...` or `AGE-SECRET-KEY-PQ-...`) is required only for `restore` and `verify`.
- For X25519 identities, the secret encodes private key material.
- For ML-KEM-768/X25519 identities, the secret encodes seed material expanded by age.
- Public recipient keys are safe to check into config or distribute in automation.
- Keep identity files owner-only (`chmod 600`).
- `derive-key` deterministically derives either X25519 (default) or ML-KEM-768/X25519 (`--pq`) from passphrase+org. It is an optional key-management mode for recovery-focused setups, not a stronger substitute for a random key file.
- `derive-key` uses a deterministic, non-secret salt; security is bounded by passphrase entropy and Argon2id cost. Because derivation is deterministic per org, the same passphrase and org always produce the same identity; compromise of one backup's passphrase exposes all backups under that org.
- `derive-key --pq` changes the recipient/identity format, but the security boundary is still the human passphrase plus Argon2id. If quantum margin of the recovery secret matters, prefer random hybrid keys from `age-keygen -pq`.
- Key rotation is the operator's responsibility; `x-rbackup-recipient-fp` object metadata can detect a recipient mismatch before restore/verify.
- There is no passphrase-only encryption mode. `seal: true` adds a second passphrase layer over age-encrypted data; both the age identity and the seal passphrase are required for decryption. Seal is intended for interactive workstation use where the passphrase is kept in human memory or a separate vault, adding another layer if the identity file is compromised.

## Choosing an Identity Model

- Random X25519 key file: simplest default if you can reliably store the identity file as recovery material.
- Random ML-KEM-768/X25519 key file (`age-keygen -pq`): same basic workflow with age's hybrid recipient format.
- Deterministic `derive-key`: useful when recovery from passphrase + org meaningfully lowers the risk of losing access to backups.
- There is no universal default. The tradeoff is secret-file management risk versus offline passphrase-guessing risk.

## Generate a Random Age Keypair

```bash
age-keygen -o ~/.config/rbackup/identity.txt
age-keygen -y ~/.config/rbackup/identity.txt
```

## Generate a Random Post-Quantum Hybrid Keypair

```bash
age-keygen -pq -o ~/.config/rbackup/identity-pq.txt
age-keygen -y ~/.config/rbackup/identity-pq.txt
```

Use the printed recipient (`age1...` or `age1pq...`) as `encryption.age_recipient` (single string or YAML list for multiple recipients). Set `encryption.identity_file` for restore/verify workflows.

## Backup Signing

Encryption does not prove who created a backup. If someone has S3 write access and the public age recipient, they can upload a fake backup that decrypts cleanly. Signing closes that gap.

Signing uses the backup host's SSH Ed25519 key (`/etc/ssh/ssh_host_ed25519_key`), requiring no new key management. The ciphertext's SHA-256 hex string (already stored in `x-rbackup-sha256` metadata) is signed, and the raw 64-byte Ed25519 signature is stored base64-encoded in `x-rbackup-signature` metadata.

Signing is optional at backup time: omitting `signing.host_key` produces unsigned backups. When `x-rbackup-sha256` metadata is present, `restore`/`verify` recomputes the downloaded ciphertext SHA-256 and requires a match before success. Verification is strict when trusted public keys are configured (`signing.allowed_signers`, `signing.allowed_signers_file`, or auto-resolved `<host_key>.pub`): restore/verify requires object metadata, a matching downloaded ciphertext hash, and a valid `x-rbackup-signature` matching one of those keys. Unsigned objects (or metadata check failures) are rejected before decryption. This is strict by design; restoring legacy unsigned objects requires running without configured signers.

### Which Public Keys Are Trusted

- **`signing.allowed_signers`** (inline list of `ssh-ed25519 AAAA...` keys): only the public keys you list are accepted. This is the recommended configuration.
- **`signing.allowed_signers_file`** (SSH `known_hosts` format; ideally a dedicated archived file, not a general workstation file): any Ed25519 host key in that file is accepted. Bare `authorized_keys` lines are not parsed here. The file on the restore machine is what matters, so if it changes, verification behavior changes too. If one of those host keys is compromised, an attacker with S3 access can forge signed backups.

Both can be set — keys are merged and deduplicated. No signer fingerprint is stored in S3 metadata because metadata is attacker-controlled; security comes entirely from the public keys you choose to trust. For repeatable recovery on another host, prefer `signing.allowed_signers` or a dedicated versioned `allowed_signers_file` archived with your recovery docs. Treat a general workstation `known_hosts` file as a convenience, not long-term policy.

When `host_key` is configured and no allowed signers are set (neither `allowed_signers` nor `allowed_signers_file`), rbackup reads `<host_key>.pub` as an allowed signer (standard SSH convention: `/etc/ssh/ssh_host_ed25519_key` → `/etc/ssh/ssh_host_ed25519_key.pub`). This is a single-host shortcut, not a portable list of trusted keys. If restore happens on another machine, that auto-resolution does not carry over unless the same public key is present there. If the `.pub` file does not exist, no auto-signer is added and signature verification remains disabled unless other signers are configured.
