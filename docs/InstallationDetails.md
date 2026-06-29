# Installation Details

This is aimed at System Administers and presents a more technical version of the install process.

FastSell v0.1 uses Docker Compose for the public self-hosted deployment model.

Normal users deploy FastSell from the setup bundle attached to GitHub Releases. The setup bundle contains runtime Compose files, migrations, setup scripts, and user documentation; it does not require a full repository clone.

## Compose Stack

`docker-compose.yml` defines:

- `postgres`
- `migrate`
- `api`
- `system-agent`
- `web`

Runtime files live under `/srv/fastsell`:

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

## Images

The GitHub Actions workflow publishes one GHCR package:

- `ghcr.io/bexusflexus/fastsell`

Components are separated by tags:

- `api-latest`
- `system-agent-latest`
- `web-latest`

The publish workflow also emits component-prefixed branch, SHA, and version tags such as `api-main`, `api-sha-<sha>`, `api-v0.1.0`, `system-agent-main`, and `web-v0.1.0`. Release setup bundles prefer matching versioned image tags when the bundle version is a semver tag.

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

-Initial Install from an extracted setup bundle:

```bash
sudo bash setup/linux/install.sh
```

-Update: back up before updating, then run the updater from the newly extracted setup bundle. The updater requires an existing install, preserves /srv/fastsell/data and /srv/fastsell/config, copies updated runtime files, applies migrations, pulls updated FastSell images, restarts services, and checks health.

```bash
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
