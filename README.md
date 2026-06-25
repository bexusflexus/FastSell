# FastSell

FastSell is a self-hosted inventory intake, review, and sales-prep system. It helps you upload item photos, group intake batches, review and enrich inventory records, manage storage containers and locations, and prepare items for sale.

License: GPL-3.0-or-later.

## What It Runs

- Go backend API and background workers
- React/TypeScript frontend served by nginx
- PostgreSQL database
- Docker Compose deployment
- Local filesystem image storage under `/srv/fastsell/data`
- Optional AI-assisted review using user-provided provider credentials

## Quick Start

Prerequisites: Linux host, Docker Engine, Docker Compose plugin, Git, and enough disk for PostgreSQL plus uploaded images.

```bash
git clone https://github.com/bexusflexus/FastSell.git
cd FastSell
bash deploy/linux/install.sh
```

The installer creates `/srv/fastsell`, asks for a PostgreSQL password, copies Compose and migration files, applies the database baseline, and starts the stack.

Default web URL:

```text
http://localhost:8888
```

## Documentation

- [System requirements](docs/System_Requirements.md)
- [Installation](docs/Installation.md)
- [Deployment](docs/Deployment.md)
- [Development](docs/Development.md)
- [Architecture](docs/Architecture.md)
- [Backup and restore](docs/Backup_Restore.md)
- [Security](docs/Security.md)
- [Roadmap](docs/Roadmap.md)

## Development

API:

```bash
cd api
go test ./...
```

Frontend:

```bash
cd frontend
npm install
npm run dev
npm run build
```

## Conduct

Be respectful. Be useful. Be direct, but not abusive.
