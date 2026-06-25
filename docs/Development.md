# Development

FastSell has a Go backend and a React/TypeScript frontend.

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

## License

FastSell is licensed under GPL-3.0-or-later.
