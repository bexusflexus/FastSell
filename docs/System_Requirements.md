# System Requirements

FastSell is intended for a self-hosted Linux machine on a trusted network.

## Minimum

- Linux host with systemd-friendly shell environment
- Docker Engine and Docker Compose plugin
- 2 CPU cores
- 2 GB RAM
- 10 GB free disk, plus space for uploaded images and backups
- Bash for the provided setup scripts
- Git only for development or contribution work from a full repository clone

## Recommended

- 4 CPU cores
- 4 GB RAM or more
- SSD storage for PostgreSQL and image processing
- Regular off-host backups

## Runtime Services

- PostgreSQL 16
- FastSell Go API
- FastSell system-agent
- nginx-served React frontend
- Local filesystem image storage under `/srv/fastsell/data`

AI-assisted features are optional and require provider credentials supplied by the operator through FastSell configuration or environment variables.  Currently, only Google Gemini is supported.
