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

Prerequisites: Linux host, Docker Engine, Docker Compose plugin, Bash, and enough disk for PostgreSQL plus uploaded images.

Normal users install from the FastSell setup bundle published with each GitHub Release. The setup bundle contains only the files needed to install, update, uninstall, and run FastSell from prebuilt GHCR images.

```bash
mkdir -p ~/fastsell-install
cd ~/fastsell-install
curl -L -o fastsell-setup.tar.gz \
  https://github.com/bexusflexus/FastSell/releases/download/v0.1.0/fastsell-setup-v0.1.0.tar.gz
tar -xzf fastsell-setup.tar.gz --strip-components=1
rm -- fastsell-setup.tar.gz
sudo bash setup/linux/install.sh
```

`~/fastsell-install` is the reusable setup workspace. The installer creates the runtime root at `/srv/fastsell`, asks for a PostgreSQL password, copies Compose and migration files, pulls prebuilt GHCR images, applies the database baseline, and starts the stack.

Update from a newer setup bundle after taking a backup:

```bash
cd ~/fastsell-install
# download and extract the newer setup bundle here, then:
sudo bash setup/linux/update.sh
```

Uninstall:

```bash
sudo bash setup/linux/uninstall.sh
```

Default uninstall removes FastSell containers and installed app/runtime files, but preserves user data under `/srv/fastsell/data` and config under `/srv/fastsell/config`. To permanently remove PostgreSQL data, uploaded images/files, generated exports, config, and installed app/runtime files, back up first and run:

```bash
sudo bash setup/linux/uninstall.sh --killmydata
```

Default web URL:

```text
http://localhost:8888
```

## Documentation

- [System requirements](docs/System_Requirements.md)
- [Installation](docs/Installation.md)
- [Installation Details (Technical)](docs/InstallationDetails.md)
- [Development](docs/Development.md)
- [Release workflow](docs/ReleaseWorkflow.md)
- [AI setup](docs/AI_Setup.md)
- [Architecture](docs/Architecture.md)
- [Backup and restore](docs/Backup_Restore.md)
- [Security](docs/Security.md)
- [Roadmap](docs/Roadmap.md)
- [The Basics Of FastSell](docs/TheBasics.md)


Normal inventory setup can run without AI configured, but Whole Scene and AI features require Gemini configuration. For v0.1, Gemini is the only tested AI provider; see [AI setup](docs/AI_Setup.md).

## Development

Developers and contributors should use a full repository clone instead of the setup bundle.

```bash
git clone https://github.com/bexusflexus/FastSell.git
cd FastSell
```

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
