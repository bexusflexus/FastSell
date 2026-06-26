# Installation

FastSell v0.1 installs from the FastSell setup bundle. Normal users do not need to clone the full source repository.

## Prerequisites

- Docker Engine
- Docker Compose plugin
- Bash
- Permission to create and manage `/srv/fastsell`

## Install

Download the setup bundle for the release you want from GitHub Releases:

```text
https://github.com/bexusflexus/FastSell/releases
```

Extract either archive format:

```bash
unzip fastsell-setup-v0.1.0.zip
```

```bash
tar -xzf fastsell-setup-v0.1.0.tar.gz
```

Run the installer from the extracted setup directory:

```bash
cd fastsell-setup-v0.1.0
sudo bash setup/linux/install.sh
```

The installer:

- Creates `/srv/fastsell`
- Writes `/srv/fastsell/config/.env`
- Copies `docker-compose.yml`, nginx config, and database migrations
- Pulls prebuilt container images from GHCR
- Applies `db/migrations/000001_v0_1_baseline_schema.up.sql`
- Starts PostgreSQL, API, system-agent, and web services

The default web port is `8888`.

```text
http://localhost:8888
```

## Update

Back up FastSell before updating. Then download and extract the newer setup bundle from GitHub Releases.

```bash
cd fastsell-setup-v0.1.1
sudo bash setup/linux/update.sh
```

The updater refreshes runtime files from the extracted setup bundle, pulls configured GHCR images, runs migrations, restarts services, and checks `/health` and `/health/db`.

Default development and mainline examples use one GHCR package, `ghcr.io/bexusflexus/fastsell`, with component tags: `api-latest`, `system-agent-latest`, and `web-latest`. Release setup bundles prefer matching version tags when available, such as `api-v0.1.0`, `system-agent-v0.1.0`, and `web-v0.1.0`.

## Uninstall

```bash
sudo bash setup/linux/uninstall.sh
```

Uninstall removes FastSell containers, the Compose network, and `/srv/fastsell`, including database and image data. Back up first.

## Developer Install Path

Developers and contributors should clone the full repository, follow `docs/Development.md`, and use the contributor workflow in `CONTRIBUTING.md`. The setup bundle is the user-facing install artifact, not a source checkout.

## AI Configuration

Normal inventory setup can run without AI configured, but Whole Scene and AI features require Gemini configuration. For v0.1, Gemini is the only tested AI provider.

No API key is included with FastSell. Create your own Gemini API key, add it to FastSell's environment configuration, and configure Admin / AI Configuration to read `GEMINI_API_KEY`. See `docs/AI_Setup.md`.
