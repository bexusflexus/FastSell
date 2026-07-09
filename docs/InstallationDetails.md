# Installation Details

This is aimed at System Administers and presents a more technical version of the install process.

FastSell v0.1 uses Docker Compose for the public self-hosted deployment model.

Normal users deploy FastSell from the setup bundle attached to GitHub Releases. The setup bundle contains runtime Compose files, migrations, setup scripts, and user documentation; it does not require a full repository clone.

Use one stable setup workspace, such as `~/fastsell-install`, for extracting setup bundle files and running setup scripts. Reuse that same workspace for updates. The setup workspace is separate from the FastSell runtime root.

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
```

Ownership model:

- `/srv/fastsell`, compose files, config, migrations, app data, images, intake folders, and exports are root-owned host paths.
- Normal non-PostgreSQL directories are `0755`.
- Normal non-PostgreSQL files are `0644`.
- `/srv/fastsell/config/.env` is `0600`.
- `/srv/fastsell/data/postgres` is owned and managed by the PostgreSQL container. The setup scripts leave its ownership and permissions unchanged.

No host user or group named `fastsell` is required. Updates repair older non-PostgreSQL app data ownership to `root:root` so exports remain browsable from the host.

## SELinux and firewalld

The Linux install and update scripts keep a distro-neutral Compose source file and patch only the installed copy at `/srv/fastsell/compose/docker-compose.yml` when Docker reports SELinux in `docker info` security options. On SELinux-enabled Docker hosts, the installed Compose file uses `:Z` labels for FastSell bind mounts and leaves `/var/run/docker.sock` as a read-only mount without relabeling.

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
- Production images use versioned tags such as `api-v0.1.0`, `system-agent-v0.1.0`, and `web-v0.1.0`.

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

-Initial install from the setup workspace:

```bash
cd ~/fastsell-install
sudo bash setup/linux/install.sh
```

-Update: back up before updating, extract the newer setup bundle into the same setup workspace, then run the updater. The updater requires an existing install, preserves `/srv/fastsell/data` and `/srv/fastsell/config`, copies updated runtime files, applies migrations, pulls updated FastSell images, restarts services, and checks health.

```bash
cd ~/fastsell-install
sudo bash setup/linux/update.sh
```

-Uninstall (2 paths):

1) Default uninstall preserves user data under `/srv/fastsell/data` and config under `/srv/fastsell/config`. Preserved config includes `.env`, database credentials, app paths, port/image settings, and nginx config.

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
