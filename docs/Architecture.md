# Architecture

FastSell is a self-hosted web application for inventory intake, review, and sales preparation.

## Components

- React/TypeScript frontend served by nginx
- Go API service
- Go system-agent service for narrowly scoped host/container health checks
- PostgreSQL database
- Local filesystem storage for uploaded and generated images
- In-process logical database backup scheduler and fixed-root backup/restore service
- Optional AI provider integrations configured by the operator

## Data Flow

1. Users upload item or whole-scene photos through the web UI.
2. The API stores upload metadata in PostgreSQL and writes files under the configured data root.
3. Background workers process images and update upload status.
4. Review screens turn upload groups or whole-scene candidates into inventory items.
5. Sales-prep screens create listing drafts and photo exports.

## Storage

PostgreSQL stores metadata, inventory records, upload state, provider configuration, listing drafts, and review state. Images and exports are files under `/srv/fastsell/data`.

Logical database dumps, media archives, and filesystem-persisted backup/restore job state live under `/srv/fastsell/backups`. The API receives only the data submounts it needs and does not mount the raw PostgreSQL directory. External backup software is responsible for copying the fixed backup root off-host.

The production Compose stack mounts the Docker socket read-only only into the narrowly scoped system-agent. The main API does not receive Docker socket access; Admin System obtains container status over the internal Compose network.

## AI Features

AI review is optional. FastSell stores provider configuration, but provider credentials are supplied by the operator. Do not commit API keys or local secrets.
