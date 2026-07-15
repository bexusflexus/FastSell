# Backup and Restore

FastSell protects PostgreSQL with logical custom-format dumps. It never treats the raw PostgreSQL data directory as an application backup.

## Backup storage

FastSell uses one fixed host root:

```text
/srv/fastsell/backups
```

The API sees only `/app/backups`. Completed database sets are stored in `database`, media archives in `media`, sanitized job state in `jobs`, and temporary restore state in `restore-staging`. Backup directories are root-owned with mode `0700`; generated artifacts use mode `0600`.

Do not move this root through the Admin interface and do not add arbitrary mounts. Configure Restic, Borg, Btrbk, rsync, NAS software, or another host backup system to copy `/srv/fastsell/backups` to separate, preferably encrypted storage. A backup remaining only on the FastSell host is not sufficient disaster protection.

Never copy or restore these as FastSell application backups:

```text
/srv/fastsell/data/postgres
/srv/fastsell/config/.env
```

The PostgreSQL directory is a server-managed physical data directory, not a portable logical backup. The environment file is installation-specific secret configuration and must be recreated securely on a replacement host.

## Automatic database backups

Open **Admin → Backup & Restore**. New installations default to:

- automatic backups enabled;
- daily at 2:00 AM;
- timezone `UTC`;
- 14 completed database backups retained.

Daily and weekly presets are available. Advanced scheduling accepts a validated five-field cron expression and an IANA timezone such as `America/Chicago`. The effective timezone is shown on the page. Settings changes reschedule the in-process Go scheduler immediately; FastSell does not use systemd timers or host cron. It does not launch catch-up jobs after downtime.

Disable the automatic-backup toggle and save to stop scheduled backups. **Backup database now** remains available while scheduling is disabled.

Only one database backup, database restore, validation/deletion operation, or media archive can run at once. The API and recovery CLI share a nonblocking filesystem lock under `/app/backups/jobs`, so separate processes cannot run these operations concurrently. A conflicting request returns a conflict response instead of waiting.

## Database backup format

FastSell runs PostgreSQL 16 client tools against the configured PostgreSQL 16 server and creates `pg_dump -Fc` custom-format dumps with owner and ACL data omitted. A completed set is:

```text
fastsell-db-backup-YYYYMMDDTHHMMSS-vX.Y.Z-pg16.dump
fastsell-db-backup-YYYYMMDDTHHMMSS-vX.Y.Z-pg16.dump.sha256
fastsell-db-backup-YYYYMMDDTHHMMSS-vX.Y.Z-pg16.dump.json
```

If more than one backup starts in the same second, FastSell adds a numeric suffix. The portable JSON sidecar contains only format/version, timestamp, database type/name, PostgreSQL major, schema migration version, dump format, byte size, and origin (`manual`, `scheduled`, or `pre_restore`). It does not contain connection hosts, credentials, tokens, environment values, row data, or personal filesystem paths.

FastSell writes `.partial` files, flushes the dump, validates it with `pg_restore --list`, creates SHA-256 and metadata sidecars, and publishes the dump last. Interrupted or invalid artifacts never appear in inventory. Abandoned partial files are removed on startup and before the next backup.

Retention runs only after a new dump completes and validates. Manual and scheduled backups both count. Cleanup removes a dump and its sidecars as one set and never removes the set currently being created or restored. A cleanup warning does not change a valid backup job to failed.

## Restore from the Admin page

Choose a completed inventory entry and select **Validate** before relying on it. Restore accepts only an inventory-issued filename under `/app/backups/database`; uploads and arbitrary paths are not accepted.

Select **Restore**, read the destructive warning, and type:

```text
RESTORE FASTSELL
```

FastSell then:

1. enters maintenance mode and blocks new data-changing requests;
2. waits for active application writes;
3. verifies all three artifacts, SHA-256, metadata format, schema metadata, PostgreSQL major, and `pg_restore --list`;
4. creates and validates a pre-restore logical backup even if scheduling is disabled;
5. persists restore job state under the fixed backup root;
6. creates an empty staging database and restores into it with clean/if-exists/no-owner/no-ACL/exit-on-error behavior;
7. applies the installed migrations to staging with the same golang-migrate engine and files used by normal deployments;
8. validates the staged database's required tables, migration state, and connectivity;
9. atomically renames the current database aside and activates the staged database;
10. retains the original database while validating the active restore, reloading backup settings, and checking the equivalents of `/health` and `/health/db`;
11. swaps the original database back automatically if post-swap validation fails, or removes the retained rollback database after success;
12. exits maintenance mode only after validation succeeds.

Version 0.1.4 requires the backup PostgreSQL major version to match the running server. Automatic cross-major restore is intentionally unsupported. A backup with a schema newer than the installed FastSell version is also rejected.

Failures before the database swap leave the current database untouched. If post-swap validation fails, FastSell atomically swaps the retained original database back and validates it. The pre-restore logical backup is preserved throughout restore and rollback and cannot be removed by concurrent retention or deletion. If rollback or database health validation is uncertain, maintenance mode remains active and both backup and retained database recovery material are preserved. Never report or assume success until the job reaches `succeeded`.

## Disaster recovery without the web interface

Install or update FastSell to the desired version first. Existing completed sets can be copied back only under `/srv/fastsell/backups/database`. Stop the API and web containers, then invoke the recovery CLI from the setup bundle's installed Compose directory:

```bash
cd /srv/fastsell/compose
sudo docker compose \
  --env-file /srv/fastsell/config/.env \
  --project-name fastsell \
  -f docker-compose.yml \
  stop api web

sudo docker compose \
  --env-file /srv/fastsell/config/.env \
  --project-name fastsell \
  -f docker-compose.yml \
  run --rm --no-deps \
  --entrypoint /app/fastsell-backup \
  api restore \
  --backup-id 'fastsell-db-backup-YYYYMMDDTHHMMSS-vX.Y.Z-pg16.dump' \
  --confirm 'RESTORE FASTSELL'
```

The CLI accepts an inventory filename, not a path, and calls the same validation, pre-restore backup, restore, migration, rollback, and health code used by Admin. It reads the database password only from the existing container environment and never prints it. If recovery succeeds:

```bash
sudo docker compose \
  --env-file /srv/fastsell/config/.env \
  --project-name fastsell \
  -f docker-compose.yml \
  up -d api web
```

If recovery fails, leave the API stopped, preserve `/srv/fastsell/backups`, and use the sanitized job files in `/srv/fastsell/backups/jobs` to identify the failed phase.

This command is also the supported fresh-install recovery path for v0.1.4. A separate onboarding framework was not introduced.

## Media archives

**Create media archive now** manually creates a low-overhead `tar.zst` set for durable directories that exist under `/app/data`: images, exports, and videos. It never includes PostgreSQL storage, intake/processing files, backups, configuration, or `.env` files. Videos are reserved for future durable video storage; a missing or empty video directory is handled normally.

Media archives are not scheduled and media restore is not implemented in v0.1.4. They are full archives rather than incremental backups. Copy them off-host with the database sets and restore media manually only after reviewing archive contents and permissions.

## Operational checks

- Test backup validation and restore procedures before relying on them.
- Keep database and media artifacts from compatible points in time where practical.
- Protect dumps and media archives: logical dumps contain FastSell row data, and media archives may contain private images or videos.
- Check `/health` and `/health/db` after backup-related upgrades and restores.
