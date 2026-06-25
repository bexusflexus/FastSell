# Development

FastSell has a Go backend and a React/TypeScript frontend.

Use a full repository clone for development and contribution work. The setup bundle is for normal users who only need to install, update, uninstall, and run FastSell from prebuilt GHCR images.

```bash
git clone https://github.com/bexusflexus/FastSell.git
cd FastSell
```

See `CONTRIBUTING.md` for branch creation, pull request, validation, and maintainer approval expectations.

## Backend

```bash
cd api
go test ./...
```

The API requires `DATABASE_URL` when run locally. Use PostgreSQL through Docker Compose or another local PostgreSQL instance and apply migrations from `db/migrations`.

## Frontend

```bash
cd frontend
npm install
npm run dev
npm run build
```

The Vite dev server proxies API calls according to `frontend/vite.config.ts`.

## Database

The v0.1 baseline migration is:

```text
db/migrations/000001_v0_1_baseline_schema.up.sql
db/migrations/000001_v0_1_baseline_schema.down.sql
```

Use new numbered migrations for future schema changes. Do not edit the baseline after public release unless the release is being rebuilt before publication.

## Local Files

Image files are stored on disk, not in PostgreSQL. Local development data, `.env` files, build output, and dependency directories should stay untracked.

## Setup Bundle

The user-facing setup bundle is generated from the full repository:

```bash
bash scripts/setup/create-setup-bundle.sh v0.1.0
```

This writes `dist/fastsell-setup-v0.1.0.zip` and `dist/fastsell-setup-v0.1.0.tar.gz`. The generated `dist/` directory is ignored by Git.

## License

FastSell is licensed under GPL-3.0-or-later.
