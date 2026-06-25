# Deployment

FastSell v0.1 uses Docker Compose for the public self-hosted deployment model.

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

## Images

The GitHub Actions workflow publishes one GHCR package:

- `ghcr.io/bexusflexus/fastsell`

Components are separated by tags:

- `api-latest`
- `system-agent-latest`
- `web-latest`

The publish workflow also emits component-prefixed branch, SHA, and version tags such as `api-main`, `api-sha-<sha>`, `api-v0.1.0`, `system-agent-main`, and `web-v0.1.0`.

If GHCR push fails with `permission_denied: write_package`, confirm repository Actions workflow permissions are set to Read and write permissions, and confirm the workflow uses `GITHUB_TOKEN` with `packages: write`.

## Configuration

The install script writes `/srv/fastsell/config/.env`. Important settings include:

- `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `DATABASE_URL`
- `FASTSELL_HTTP_PORT`
- `FASTSELL_API_IMAGE`, `FASTSELL_SYSTEM_AGENT_IMAGE`, `FASTSELL_WEB_IMAGE`
- `DATA_ROOT`, `IMAGE_ROOT`, and intake directories
- AI provider credentials or environment variable names, when configured by the operator

## Operations

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
