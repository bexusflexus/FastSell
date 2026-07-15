# Security

FastSell v0.1 is designed for self-hosted use on a trusted network.

## Operator Responsibilities

- Use a strong PostgreSQL password.
- Keep `/srv/fastsell/config/.env` readable only by administrators.
- Do not commit `.env` files, API keys, provider credentials, or private host details.
- Keep Docker, the host OS, and FastSell images updated.
- Use Admin → Backup & Restore for logical database dumps, copy `/srv/fastsell/backups` off-host, and protect those copies.

## Network Exposure

By default, FastSell serves HTTP on port `8888`. Put it behind a trusted reverse proxy with TLS if exposing it beyond localhost or a private LAN.

## AI Credentials

Normal inventory setup can run without AI configured, but Whole Scene and AI features require Gemini configuration. For v0.1, Gemini is the only tested AI provider. See `docs/AI_Setup.md`.

No API key is included with FastSell. Store `GEMINI_API_KEY` only in FastSell's environment configuration or another private secret store. Do not commit real keys into Git, and do not paste keys into screenshots, support tickets, logs, chat messages, or public issues. Rotate keys if exposed.

## Filesystem Storage

Uploaded images and generated exports live under `/srv/fastsell/data`. Treat this directory as private application data.

Logical dumps and media archives live under `/srv/fastsell/backups` with root-only directory and file modes. They contain private application data even though their metadata sidecars contain no credentials. Never use `/srv/fastsell/data/postgres` or `/srv/fastsell/config/.env` as an application backup, and never expose either through the backup API.

FastSell uses a root-owned appliance-style host runtime tree. The setup scripts repair non-PostgreSQL app data to `root:root` with host-browsable directory and file modes, while backups remain root-only. PostgreSQL data under `/srv/fastsell/data/postgres` is left to the PostgreSQL container, and install/update leave its ownership and permissions unchanged.

## Reporting Issues

For public security reports, open a GitHub security advisory or contact the project maintainer through the repository.
