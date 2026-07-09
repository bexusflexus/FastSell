# Installation

FastSell v0.1 installs from the FastSell setup bundle. Normal users do not need to clone the full source repository.

## Prerequisites

- Docker Engine
- Docker Compose plugin
- Bash
- Permission to create and manage `/srv/fastsell`

## Install

`~/fastsell-install` is the setup workspace where you download and extract setup bundle files. Reuse this same folder for future updates.

`/srv/fastsell` is the runtime root created by the setup scripts. Runtime config lives at `/srv/fastsell/config/.env`, and runtime data lives under `/srv/fastsell/data`.

1) Create the setup workspace.

```bash
mkdir -p ~/fastsell-install
cd ~/fastsell-install
```

2) Download the setup bundle for the release you want from GitHub Releases. The tarball is preferred because it can be extracted directly into the stable setup workspace.

### Option 1: curl

Tarball:

```bash
curl -L -o fastsell-setup.tar.gz \
  https://github.com/bexusflexus/FastSell/releases/download/v0.1.1/fastsell-setup-v0.1.1.tar.gz
```

Zip:

```bash
curl -L -o fastsell-setup.zip \
  https://github.com/bexusflexus/FastSell/releases/download/v0.1.1/fastsell-setup-v0.1.1.zip
```

### Option 2: wget

Tarball:

```bash
wget -O fastsell-setup.tar.gz \
  https://github.com/bexusflexus/FastSell/releases/download/v0.1.1/fastsell-setup-v0.1.1.tar.gz
```

Zip:

```bash
wget -O fastsell-setup.zip \
  https://github.com/bexusflexus/FastSell/releases/download/v0.1.1/fastsell-setup-v0.1.1.zip
```

### Option 3: GitHub CLI

Tarball:

```bash
gh release download v0.1.1 --repo bexusflexus/FastSell \
  --pattern "fastsell-setup-v0.1.1.tar.gz" \
  --output fastsell-setup.tar.gz
```

Zip:

```bash
gh release download v0.1.1 --repo bexusflexus/FastSell \
  --pattern "fastsell-setup-v0.1.1.zip" \
  --output fastsell-setup.zip
```

### Option 4: Browser

Open the GitHub Releases page, download the release archive, and save it in `~/fastsell-install` as `fastsell-setup.tar.gz` or `fastsell-setup.zip`.

```text
https://github.com/bexusflexus/FastSell/releases
```

3) Extract into the setup workspace.

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

4) Run the installer from the setup workspace:

```bash
sudo bash setup/linux/install.sh
```

What the installer does:

- Creates `/srv/fastsell`
- Writes `/srv/fastsell/config/.env`
- Sets non-PostgreSQL runtime directories to `root:root` with host-browsable permissions
- Copies `docker-compose.yml`, nginx config, and database migrations
- Automatically adds SELinux relabel options to the installed Compose bind mounts when Docker reports SELinux support
- Opens the FastSell web and PostgreSQL ports permanently when firewalld is active
- Pulls prebuilt container images from GHCR
- Applies `db/migrations/000001_v0_1_baseline_schema.up.sql`
- Starts PostgreSQL, API, system-agent, and web services

FastSell does not require a host user or group named `fastsell`. App data under `/srv/fastsell/data` is root-owned for the local appliance model. PostgreSQL data under `/srv/fastsell/data/postgres` is managed by the PostgreSQL container and is not repaired by the setup scripts.

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

Back up FastSell before updating. Then reuse the same setup workspace and extract the newer setup bundle into it.

```bash
cd ~/fastsell-install
curl -L -o fastsell-setup.tar.gz \
  https://github.com/bexusflexus/FastSell/releases/download/v0.1.1/fastsell-setup-v0.1.1.tar.gz
tar -xzf fastsell-setup.tar.gz --strip-components=1
rm -- fastsell-setup.tar.gz
sudo bash setup/linux/update.sh
```

The updater runs from the setup workspace and refreshes runtime files under `/srv/fastsell`, repairs non-PostgreSQL app data ownership to `root:root`, pulls configured GHCR images, runs migrations, restarts services, and checks `/health` and `/health/db`. It preserves `/srv/fastsell/config/.env` and leaves `/srv/fastsell/data/postgres` ownership and permissions unchanged.

On SELinux-enabled Docker hosts, the updater reapplies the required bind mount labels to the freshly copied installed Compose file. It does not delete or recreate existing PostgreSQL data.

FastSell uses one GHCR package, `ghcr.io/bexusflexus/fastsell`, with component-specific tags. Production setup bundles use versioned image refs such as `api-v0.1.1`, `system-agent-v0.1.1`, and `web-v0.1.1`. Candidate setup bundles use full-SHA refs such as `api-sha-<full_git_sha>`. `latest` is a deprecated legacy compatibility default and is not moved by the release workflow.

## Uninstall

```bash
sudo bash setup/linux/uninstall.sh
```

Default uninstall removes FastSell containers, the Compose network, and installed app/runtime files, but preserves user data under `/srv/fastsell/data` and config under `/srv/fastsell/config`. Preserved data includes PostgreSQL data, uploaded images/files, generated exports, and other FastSell runtime data. Preserved config includes `.env`, database credentials, app paths, port/image settings, and nginx config.

Uninstall does not disable Docker or firewalld and does not remove firewall rules.

To permanently remove FastSell user data, back up first and run:

```bash
sudo bash setup/linux/uninstall.sh --killmydata
```

`--killmydata` removes `/srv/fastsell`, including `/srv/fastsell/data`, `/srv/fastsell/config`, PostgreSQL data, uploaded images/files, generated exports, and installed app/config/runtime files.

## Developer Install Path

Developers and contributors should clone the full repository, follow `docs/Development.md`, and use the contributor workflow in `CONTRIBUTING.md`. The setup bundle is the user-facing install artifact, not a source checkout.

## AI Configuration

Normal inventory setup can run without AI configured, but Whole Scene and AI features require Gemini configuration. For v0.1, Gemini is the only tested AI provider.

No API key is included with FastSell. Create your own Gemini API key, add it to FastSell's environment configuration, and configure Admin / AI Configuration to read `GEMINI_API_KEY`. See [AI setup](docs/AI_Setup.md).
