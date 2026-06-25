# Security

FastSell v0.1 is designed for self-hosted use on a trusted network.

## Operator Responsibilities

- Use a strong PostgreSQL password.
- Keep `/srv/fastsell/config/.env` readable only by administrators.
- Do not commit `.env` files, API keys, provider credentials, or private host details.
- Keep Docker, the host OS, and FastSell images updated.
- Back up data and protect backups.

## Network Exposure

By default, FastSell serves HTTP on port `8888`. Put it behind a trusted reverse proxy with TLS if exposing it beyond localhost or a private LAN.

## AI Credentials

AI provider configuration requires user-provided credentials or environment variables. Store provider secrets outside Git and rotate them if exposed.

## Filesystem Storage

Uploaded images and generated exports live under `/srv/fastsell/data`. Treat this directory as private application data.

## Reporting Issues

For public security reports, open a GitHub security advisory or contact the project maintainer through the repository.
