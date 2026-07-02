# Installation

FastSell v0.1 installs from the FastSell setup bundle. Normal users do not need to clone the full source repository.

## Prerequisites

- Docker Engine
- Docker Compose plugin
- Bash
- Permission to create and manage `/srv/fastsell`

## Install

1) Create a landing spot for the install program.  
  ```bash
  mkdir -p ~/fastsell-install
  cd ~/fastsell-install
  ```

2) Download the setup bundle for the release you want from GitHub Releases.  Choose from either the .zip or .tar files.  There are 4 options for downloading (browser, curl, wget, gh).  Pick your favorite and replace version number, if necessary.

  #### Option 1: curl

  Tarball  
  ```bash
  curl -L -O https://github.com/bexusflexus/FastSell/releases/download/v0.1.0/fastsell-setup-v0.1.0.tar.gz
  ```
  Zip
  ```bash
  curl -L -O https://github.com/bexusflexus/FastSell/releases/download/v0.1.0/fastsell-setup-v0.1.0.zip
  ```

  #### Option 2: wget

  Tarball
  ```bash
  wget https://github.com/bexusflexus/FastSell/releases/download/v0.1.0/fastsell-setup-v0.1.0.tar.gz
  ```
  Zip
  ```bash
  wget https://github.com/bexusflexus/FastSell/releases/download/v0.1.0/fastsell-setup-v0.1.0.zip
  ```

  #### Option 3: GitHub CLI

  Tarball
  ```bash
  gh release download v0.1.0 --repo bexusflexus/FastSell --pattern "fastsell-setup-v0.1.0.tar.gz"
  ```
  Zip
  ```bash
  gh release download v0.1.0 --repo bexusflexus/FastSell --pattern "fastsell-setup-v0.1.0.zip"
  ```

  #### Option 4: Browser

  Paste into browser address bar:
  ```text
  https://github.com/bexusflexus/FastSell/releases
  ```

3) Extract either archive format:

Tarball
```bash
tar -xzf fastsell-setup-v0.1.0.tar.gz
```
OR
*Use this to delete tarball after extraction
```bash
tar -xzf fastsell-setup-v0.1.0.tar.gz && rm -- fastsell-setup-v0.1.0.tar.gz
```

Zip file
```bash
unzip fastsell-setup-v0.1.0.zip
```
OR
*Use this to delete tarball after extraction
```bash
unzip fastsell-setup-v0.1.0.zip && rm -- fastsell-setup-v0.1.0.zip
```

4) Run the installer from the extracted setup directory:

  ```bash
  cd fastsell-setup-v0.1.0
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

Back up FastSell before updating. Then download and extract the newer setup bundle from GitHub Releases.

```bash
cd fastsell-setup-v0.1.1
sudo bash setup/linux/update.sh
```

The updater refreshes runtime files from the extracted setup bundle, repairs non-PostgreSQL app data ownership to `root:root`, pulls configured GHCR images, runs migrations, restarts services, and checks `/health` and `/health/db`. It leaves `/srv/fastsell/data/postgres` ownership and permissions unchanged.

On SELinux-enabled Docker hosts, the updater reapplies the required bind mount labels to the freshly copied installed Compose file. It does not delete or recreate existing PostgreSQL data.

Default development and mainline examples use one GHCR package, `ghcr.io/bexusflexus/fastsell`, with component tags: `api-latest`, `system-agent-latest`, and `web-latest`. Release setup bundles prefer matching version tags when available, such as `api-v0.1.0`, `system-agent-v0.1.0`, and `web-v0.1.0`.

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
