# Architecture

FastSell is a self-hosted web application for inventory intake, review, and sales preparation.

## Components

- React/TypeScript frontend served by nginx
- Go API service
- Go system-agent service for host/container health checks
- PostgreSQL database
- Local filesystem storage for uploaded and generated images
- Optional AI provider integrations configured by the operator

## Data Flow

1. Users upload item or whole-scene photos through the web UI.
2. The API stores upload metadata in PostgreSQL and writes files under the configured data root.
3. Background workers process images and update upload status.
4. Review screens turn upload groups or whole-scene candidates into inventory items.
5. Sales-prep screens create listing drafts and photo exports.

## Storage

PostgreSQL stores metadata, inventory records, upload state, provider configuration, listing drafts, and review state. Images and exports are files under `/srv/fastsell/data`.

## AI Features

AI review is optional. FastSell stores provider configuration, but provider credentials are supplied by the operator. Do not commit API keys or local secrets.
