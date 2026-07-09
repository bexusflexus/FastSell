# Development

FastSell has a Go backend and a React/TypeScript frontend.

Use a full repository clone for development and contribution work. The setup bundle is for normal users who only need to install, update, uninstall, and run FastSell from prebuilt GHCR images.

Do not clone the full repo into `~/fastsell-install`; that folder is the stable setup workspace for extracted setup bundles. Use a separate development checkout, for example:

```bash
mkdir -p ~/Code/bexusflexus
cd ~/Code/bexusflexus
git clone git@github.com:bexusflexus/FastSell.git
cd FastSell
```

Normal users should use setup bundles, not a repo clone. Setup bundles are extracted into `~/fastsell-install`; runtime files, config, and data are installed under `/srv/fastsell`.

See `CONTRIBUTING.md` for branch creation, pull request, validation, and maintainer approval expectations.

## Backend

```bash
cd api
go test ./...
```

The API requires `DATABASE_URL` when run locally. Use PostgreSQL through Docker Compose or another local PostgreSQL instance and apply migrations from `db/migrations`.

Normal inventory development can run without AI configured. Whole Scene and AI features require Gemini configuration; for v0.1, Gemini is the only tested provider. See `docs/AI_Setup.md` and use `GEMINI_API_KEY` as the local environment variable name when testing those features.

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
bash scripts/setup/create-setup-bundle.sh <version>
```

Replace `<version>` with the version you are building, such as `v0.1.1`. This writes `dist/fastsell-setup-<version>.zip` and `dist/fastsell-setup-<version>.tar.gz`. The generated `dist/` directory is ignored by Git.

## License

FastSell is licensed under GPL-3.0-or-later.
