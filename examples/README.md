# Examples

- `config.yaml`: reference config with all settings documented (defaults commented out).
- `cron/crontab.example`: daily cron entry (`/etc/cron.d` style, one receipt JSON file per run).

For systemd deployments, use a `rbackup-backup.service` oneshot unit plus a matching
`rbackup-backup.timer`. Those unit files are not currently checked into this directory;
adjust user, paths, and schedule to your host.
