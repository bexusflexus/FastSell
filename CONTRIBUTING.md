# Contributing

FastSell development uses short-lived branches and pull requests into `main`.

## Branch Workflow

Start from an up-to-date `main` branch:

```bash
git checkout main
git pull --ff-only
```

Create a feature branch:

```bash
git checkout -b feature/setup-bundle
```

Make changes, run validation, commit, and push:

```bash
git status --short
git diff --check
docker compose config
cd api && go test ./...
cd ../frontend && npm run build
git add .
git commit -m "Add setup bundle workflow"
git push -u origin feature/setup-bundle
```

Open a pull request to `main`, wait for CI, and require maintainer approval before merge.

## Branch Prefixes

- `feature/` for user-visible functionality or larger additions
- `fix/` for bug fixes
- `docs/` for documentation-only changes
- `chore/` for maintenance work

## Local Validation

Recommended validation before opening a pull request:

```bash
git diff --check
docker compose config
bash -n setup/linux/install.sh setup/linux/update.sh setup/linux/uninstall.sh
bash -n scripts/setup/create-setup-bundle.sh
bash scripts/setup/create-setup-bundle.sh v0.1.0-test
cd api && go test ./...
cd ../frontend && npm run build
```

If a change affects the setup bundle, also inspect the generated archives under `dist/` and verify the extracted bundle can run `docker compose config`.
