# Installation

FastSell v0.1 installs through Docker Compose and the Linux helper scripts in `deploy/linux`.

## Prerequisites

- Docker Engine
- Docker Compose plugin
- Git
- Bash
- Permission to create and manage `/srv/fastsell`

## Install

```bash
git clone https://github.com/bexusflexus/FastSell.git
cd FastSell
bash deploy/linux/install.sh
```

The installer:

- Creates `/srv/fastsell`
- Writes `/srv/fastsell/config/.env`
- Copies `docker-compose.yml`, nginx config, and database migrations
- Pulls container images
- Applies `db/migrations/000001_v0_1_baseline_schema.up.sql`
- Starts PostgreSQL, API, system-agent, and web services

The default web port is `8888`.

```text
http://localhost:8888
```

## Update

```bash
cd FastSell
bash deploy/linux/update.sh
```

The updater refreshes release files, pulls configured images, runs migrations, restarts services, and checks `/health` and `/health/db`.

## Uninstall

```bash
bash deploy/linux/uninstall.sh
```

Uninstall removes FastSell containers, the Compose network, and `/srv/fastsell`, including database and image data. Back up first.

## AI Configuration

AI features are disabled until you configure a provider in the admin UI. Provider API keys or environment-variable names must be supplied by the operator. Do not put provider secrets into the repository.
