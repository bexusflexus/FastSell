# Installation

FastSell installs from the FastSell setup bundle. Normal users do not need to clone the full source repository.

## Prerequisites

- Docker Engine
- Docker Compose plugin
- Bash
- Permission to create and manage `/srv/fastsell`

## Install

`~/fastsell-install` is only a setup workspace where installation or manual-fallback bundle files are extracted. It is not runtime state and is not required for normal `fastsell-update` use.

`/srv/fastsell` is the runtime root created by the setup scripts. Runtime config lives at `/srv/fastsell/config/.env`, runtime data lives under `/srv/fastsell/data`, and logical backups live under the fixed `/srv/fastsell/backups` root.

### 1) Create the setup workspace.

```bash
mkdir -p ~/fastsell-install
cd ~/fastsell-install
```

### 2) Download the latest stable setup bundle from GitHub Releases. The tarball is preferred because it can be extracted directly into the stable setup workspace.

#### Option 1: curl

Tarball:

```bash
curl -fL -o fastsell-setup.tar.gz \
  https://github.com/bexusflexus/FastSell/releases/latest/download/fastsell-setup.tar.gz
```

#### Option 2: wget

Tarball:

```bash
wget -O fastsell-setup.tar.gz \
  https://github.com/bexusflexus/FastSell/releases/latest/download/fastsell-setup.tar.gz
```

#### Option 3: GitHub CLI

Tarball:

```bash
gh release download --repo bexusflexus/FastSell \
  --pattern "fastsell-setup.tar.gz" \
  --output fastsell-setup.tar.gz
```

For an exact version or rollback, set the desired release tag explicitly and use its version-specific asset:

```bash
VERSION=vX.Y.Z
curl -fL -o fastsell-setup.tar.gz \
  "https://github.com/bexusflexus/FastSell/releases/download/${VERSION}/fastsell-setup-${VERSION}.tar.gz"
```

#### Option 4: Browser

Open the GitHub Releases page, download `fastsell-setup.tar.gz` from the latest release, and save it in `~/fastsell-install`.

```text
https://github.com/bexusflexus/FastSell/releases
```

### 3) Extract into the setup workspace.

Preferred tarball path:

```bash
tar -xzf fastsell-setup.tar.gz --strip-components=1
rm -- fastsell-setup.tar.gz
```

Zip archives contain a top-level folder. If using the zip, extract to a temporary folder and copy that folder's contents into the setup workspace:

```bash
rm -rf .fastsell-setup-unzip
mkdir .fastsell-setup-unzip
unzip fastsell-setup.zip -d .fastsell-setup-unzip
cp -a .fastsell-setup-unzip/fastsell-setup-*/. .
rm -rf .fastsell-setup-unzip fastsell-setup.zip
```

### 4) Run the installer from the setup workspace:

```bash
sudo bash setup/linux/install.sh
```

`install.sh` is only for a genuinely new installation. Before prompting for a database password or changing the host, it refuses any nonempty `/srv/fastsell` tree, preserved data or backups, installed Compose files, FastSell containers, or FastSell Compose resources. Use `fastsell-update` or the manual `update.sh` fallback for an existing installation. There is no force bypass.

What the installer does:

- Creates `/srv/fastsell`
- Creates root-only backup directories under `/srv/fastsell/backups`
- Writes `/srv/fastsell/config/.env`
- Sets non-PostgreSQL runtime directories to `root:root` with host-browsable permissions
- Copies `docker-compose.yml`, nginx config, and database migrations
- Automatically adds SELinux relabel options to the installed Compose bind mounts when Docker reports SELinux support
- Opens the FastSell web and PostgreSQL ports permanently when firewalld is active
- Pulls prebuilt container images from GHCR
- Applies `db/migrations/000001_v0_1_baseline_schema.up.sql`
- Starts PostgreSQL, API, system-agent, and web services
- Installs `/usr/local/bin/fastsell-update`

FastSell does not require a host user or group named `fastsell`. App data under `/srv/fastsell/data` is root-owned for the local appliance model. Backup directories and artifacts are root-only (`0700` directories and `0600` files). PostgreSQL data under `/srv/fastsell/data/postgres` is managed by the PostgreSQL container and is not repaired by the setup scripts.

The default web port is `8888`.

```text
http://localhost:8888
```

## SELinux, Firewall, and Database Access

The Linux install and update scripts detect Docker SELinux support from `docker info` security options. When Docker reports SELinux enabled, the scripts patch the installed Compose file under `/srv/fastsell/compose/docker-compose.yml` with the required bind mount labels. Users do not need to run manual SELinux commands for the standard FastSell install.

When firewalld is installed and active, the scripts permanently open the FastSell web port and PostgreSQL port, then reload firewalld. By default these are:

- `8888/tcp`
- `5432/tcp`

If firewalld is absent or inactive, the scripts print a notice and continue without changing firewall rules.

PostgreSQL remote database access is intentionally enabled by the bundled Compose configuration. This exposes port `5432` for administration, backup, restore, and integrations. Use a strong PostgreSQL password and limit network access at the host, router, or firewall layer when the machine is reachable from untrusted networks.

## Update

Create and validate a logical database backup in **Admin → Backup & Restore** before updating. The normal end-user command updates to the latest stable production release:

```bash
sudo fastsell-update
```

Select an exact stable release with:

```bash
sudo fastsell-update --version vX.Y.Z
```

Use `--yes` to skip the ordinary update confirmation. Selecting a version older than the installed version is blocked unless `--allow-rollback` is also supplied. Rollback is explicitly warned because forward-only database migrations may make older application versions unsafe.

`fastsell-update` reads the installed `FASTSELL_VERSION` from `/srv/fastsell/config/.env`. Without `--version`, it queries GitHub's latest stable Release endpoint and rejects draft or prerelease metadata. It reports installed and selected versions and exits successfully without downloading release assets when already current.

For an update, it creates a root-only `mktemp -d` workspace and downloads exactly these versioned production Release assets:

```text
fastsell-setup-vX.Y.Z.tar.gz
fastsell-release-vX.Y.Z.sha256
```

It uses `curl --fail --location`, extracts the setup archive's exact checksum from the release-published checksum file, verifies it with `sha256sum -c`, and then inspects the archive. Absolute paths, traversal components, unexpected top-level paths, duplicate entries, links, devices, and archives missing `setup/linux/update.sh` or `setup/linux/fastsell-update` are rejected. Extraction occurs only in the secure temporary workspace after validation.

After confirmation, the downloaded bundle's `setup/linux/update.sh` refreshes runtime Compose files and migrations, updates only managed versioned image references inherited from the production bundle, runs migrations, restarts services, validates `/health` and `/health/db`, and finally refreshes `/usr/local/bin/fastsell-update`. Existing `/srv/fastsell/data`, `/srv/fastsell/backups`, `/srv/fastsell/config/.env`, PostgreSQL storage, images, exports, and intake state are preserved.

Failed metadata or asset downloads, missing assets, malformed checksums, checksum mismatches, and unsafe archives stop before `update.sh` runs. Temporary files are removed on success, failure, interruption, or signal. If `update.sh` itself fails, its nonzero status is reported and preserved; because migrations and container updates are operational steps, inspect its output and system health before retrying.

### Manual setup-bundle fallback

If the installed command is unavailable, download and extract the desired production setup bundle into `~/fastsell-install`, verify the matching release checksum, and then run:

```bash
cd ~/fastsell-install
sudo bash setup/linux/update.sh
```

`install.sh` creates new runtime state and refuses existing or partial installations. `update.sh` requires an existing `.env` and preserves it while applying the exact files and image references already embedded in the extracted bundle. Do not delete `/srv/fastsell`; it is the runtime state, while `~/fastsell-install` is only a disposable setup workspace.

On SELinux-enabled Docker hosts, the updater reapplies the required bind mount labels to the freshly copied installed Compose file. It does not delete or recreate existing PostgreSQL data.

FastSell uses one GHCR package, `ghcr.io/bexusflexus/fastsell`, with component-specific tags. Production setup bundles use versioned image refs such as `api-vX.Y.Z`, `system-agent-vX.Y.Z`, and `web-vX.Y.Z`. Candidate setup bundles and candidate helpers are maintainer-only and use immutable full-SHA refs such as `api-sha-<full_git_sha>`. The production updater never downloads candidate artifacts or constructs image tags. `latest` container tags are deprecated compatibility defaults and are not moved by the release workflow.

## Uninstall

```bash
sudo bash setup/linux/uninstall.sh
```

Default uninstall removes FastSell containers, the Compose network, installed app/runtime files, and `/usr/local/bin/fastsell-update`, but preserves user data under `/srv/fastsell/data`, logical backups under `/srv/fastsell/backups`, and config under `/srv/fastsell/config`. Preserved data includes PostgreSQL data, uploaded images/files, generated exports, and other FastSell runtime data. Preserved config includes `.env`, database credentials, app paths, port/image settings, and nginx config.

Uninstall does not disable Docker or firewalld and does not remove firewall rules.

To permanently remove FastSell user data, back up first and run:

```bash
sudo bash setup/linux/uninstall.sh --killmydata
```

`--killmydata` removes `/srv/fastsell`, including `/srv/fastsell/data`, `/srv/fastsell/backups`, `/srv/fastsell/config`, PostgreSQL data, uploaded images/files, generated exports, logical backups, and installed app/config/runtime files.

## Developer Install Path

Developers and contributors should clone the full repository, follow `docs/Development.md`, and use the contributor workflow in `CONTRIBUTING.md`. The setup bundle is the user-facing install artifact, not a source checkout.

## AI Configuration

Normal inventory setup can run without AI configured, but Whole Scene and AI features require Gemini configuration. For v0.1, Gemini is the only tested AI provider.

No API key is included with FastSell. Create your own Gemini API key, add it to FastSell's environment configuration, and configure Admin / AI Configuration to read `GEMINI_API_KEY`. See [AI setup](docs/AI_Setup.md).
