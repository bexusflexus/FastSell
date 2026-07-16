# Installation Details

This is aimed at System Administrators and presents a more technical version of the install process.

FastSell v0.1 uses Docker Compose for the public self-hosted deployment model.

Normal users deploy FastSell from the setup bundle attached to GitHub Releases. The setup bundle contains runtime Compose files, migrations, setup scripts, and user documentation; it does not require a full repository clone.

`~/fastsell-install` is the extracted setup workspace for initial installation and production updates. It remains separate from the FastSell runtime root.

## Compose Stack

`docker-compose.yml` defines:

- `postgres`
- `migrate`
- `api`
- `system-agent`
- `web`

Runtime files are installed under `/srv/fastsell`:

```text
/srv/fastsell/compose/docker-compose.yml
/srv/fastsell/config/.env
/srv/fastsell/config/nginx/fastsell.conf
/srv/fastsell/db/migrations
/srv/fastsell/data
/srv/fastsell/backups
```

Ownership model:

- `/srv/fastsell`, compose files, config, migrations, app data, images, intake folders, and exports are root-owned host paths.
- Normal non-PostgreSQL directories are `0755`.
- Normal non-PostgreSQL files are `0644`.
- `/srv/fastsell/config/.env` is `0600`.
- `/srv/fastsell/backups` and its database, media, jobs, and restore-staging directories are `root:root` mode `0700`; artifacts are mode `0600`.
- `/srv/fastsell/data/postgres` is owned and managed by the PostgreSQL container. The setup scripts leave its ownership and permissions unchanged.

No host user or group named `fastsell` is required. Updates repair older non-PostgreSQL app data ownership to `root:root` so exports remain browsable from the host.

## SELinux and firewalld

The Linux install and update scripts keep a distro-neutral Compose source file and patch only the installed copy at `/srv/fastsell/compose/docker-compose.yml` when Docker reports SELinux in `docker info` security options. On SELinux-enabled Docker hosts, the installed Compose file uses `:Z` labels for FastSell bind mounts and leaves the system-agent's `/var/run/docker.sock` mount read-only without relabeling. The main API does not mount the Docker socket.

Users do not need manual SELinux setup commands for the standard FastSell install. Fresh installs explicitly create PostgreSQL storage for `postgres:16-alpine` at `/srv/fastsell/data/postgres` with owner/group `70:70` and mode `0700`. Updates do not delete, recreate, or repair existing PostgreSQL data.

When firewalld is installed and active, install and update permanently open these ports and reload firewalld:

- `${FASTSELL_HTTP_PORT:-8888}/tcp`
- `5432/tcp`

If firewalld is absent or inactive, the scripts print a notice and continue.

PostgreSQL remote database access is intentionally enabled. The Compose stack publishes `5432:5432` so administrators can connect for backup, restore, inspection, and integrations. Protect the host with normal network controls when FastSell is installed on a reachable machine.

## Images

The GitHub Actions workflow publishes one GHCR package:

- `ghcr.io/bexusflexus/fastsell`

Components are separated by tags:

- Candidate images use full-SHA tags such as `api-sha-<full_git_sha>`, `system-agent-sha-<full_git_sha>`, and `web-sha-<full_git_sha>`.
- Production images use versioned tags such as `api-v0.1.1`, `system-agent-v0.1.1`, and `web-v0.1.1`.

The release workflow does not publish or move `latest` tags. Any remaining `latest` references are legacy compatibility defaults for older local/source-checkout flows and should not be used as production release refs.

If GHCR push fails with `permission_denied: write_package`, confirm repository Actions workflow permissions are set to Read and write permissions, and confirm the workflow uses `GITHUB_TOKEN` with `packages: write`.

## Configuration

The setup installer writes `/srv/fastsell/config/.env`. Important settings include:

- `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `DATABASE_URL`
- `FASTSELL_HTTP_PORT`
- `FASTSELL_API_IMAGE`, `FASTSELL_SYSTEM_AGENT_IMAGE`, `FASTSELL_WEB_IMAGE`
- `DATA_ROOT`, `IMAGE_ROOT`, and intake directories
- `GEMINI_API_KEY`, when Whole Scene or AI features are used

Normal inventory setup can run without AI configured, but Whole Scene and AI features require Gemini configuration. For v0.1, Gemini is the only tested AI provider. See `docs/AI_Setup.md`.

## Operations

Initial install from the setup workspace:

```bash
cd ~/fastsell-install
sudo bash setup/linux/install.sh
```

The installer performs a read-only preflight before any password prompt or mutation. It permits only a missing or completely empty `/srv/fastsell` with no FastSell containers, Compose project containers, network, or labeled volumes. Any existing configuration, PostgreSQL files, images, intake data, exports, backups, Compose files, or other partial runtime state is refused with update and destructive-uninstall guidance.

Normal production update after creating and validating an Admin logical database backup:

```bash
cd ~/fastsell-install
sudo ./setup/linux/fastsell-update
```

The command discovers or validates a stable `vX.Y.Z` GitHub Release, verifies the versioned setup tarball against `fastsell-release-vX.Y.Z.sha256`, rejects unsafe archive structure, extracts into a secure temporary directory, and runs that bundle's `update.sh`. `update.sh` preserves `/srv/fastsell/data`, `/srv/fastsell/backups`, and `/srv/fastsell/config/.env`, applies migrations and versioned production images, and verifies health. After successful release application, the bundled updater atomically refreshes its original setup-workspace copy from the verified extracted bundle. The manual method remains `sudo bash setup/linux/update.sh` from a verified extracted production bundle.

Uninstall has two paths:

1) Default uninstall preserves user data under `/srv/fastsell/data`, logical backups under `/srv/fastsell/backups`, and config under `/srv/fastsell/config`. Preserved config includes `.env`, database credentials, app paths, port/image settings, and nginx config.

```bash
sudo bash setup/linux/uninstall.sh
```


2) Permanently delete FastSell data, config, and installed app/runtime files by running:

```bash
sudo bash setup/linux/uninstall.sh --killmydata
```

Uninstall stops and removes FastSell Compose resources but does not disable Docker or firewalld.

Check status:

```bash
sudo docker compose --env-file /srv/fastsell/config/.env \
  --project-name fastsell \
  -f /srv/fastsell/compose/docker-compose.yml ps
```

View logs:

```bash
sudo docker logs fastsell_api
sudo docker logs fastsell_web
sudo docker logs fastsell_postgres
```

Apply migrations manually:

```bash
sudo docker compose --env-file /srv/fastsell/config/.env \
  --project-name fastsell \
  -f /srv/fastsell/compose/docker-compose.yml \
  --profile tools run --rm migrate
```
