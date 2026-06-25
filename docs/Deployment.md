# Deployment

FastSell v0.1 uses Docker Compose for the public self-hosted deployment model.

## Compose Stack

`docker-compose.yml` defines:

- `postgres`
- `migrate`
- `fastsell-api`
- `fastsell-system-agent`
- `fastsell-web`

Runtime files live under `/srv/fastsell`:

```text
/srv/fastsell/compose/docker-compose.yml
/srv/fastsell/config/.env
/srv/fastsell/config/nginx/fastsell.conf
/srv/fastsell/db/migrations
/srv/fastsell/data
```

## Images

The GitHub Actions workflow publishes these GHCR images:

- `ghcr.io/bexusflexus/fastsell-api`
- `ghcr.io/bexusflexus/fastsell-system-agent`
- `ghcr.io/bexusflexus/fastsell-web`

Tags are published for `main`, semantic version tags, and commit SHA tags.

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
sudo docker logs fastsell-api
sudo docker logs fastsell-web
sudo docker logs fastsell-postgres
```

Apply migrations manually:

```bash
sudo docker compose --env-file /srv/fastsell/config/.env \
  --project-name fastsell \
  -f /srv/fastsell/compose/docker-compose.yml \
  --profile tools run --rm migrate
```
