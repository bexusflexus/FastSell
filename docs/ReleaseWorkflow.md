# Release Workflow

FastSell releases use GitHub Actions, GHCR, Docker Compose, and setup bundles. The release path intentionally separates merge-to-main candidate publishing from production promotion so staging QA happens once and production tags move only after approval.

## Normal Flow

1. Create a feature branch locally.
2. Commit and push the branch.
3. Open a pull request:

   ```bash
   ./scripts/release/create_pull_req.sh
   ```

4. Wait for automated PR checks.
5. Squash merge the PR:

   ```bash
   ./scripts/release/squash_merge_pull_req.sh <pr-number>
   ```

   If you are on the PR branch, the PR number can be omitted.

6. The `Publish Images` workflow runs on the resulting `main` commit and publishes candidate images:

   ```text
   ghcr.io/<owner>/fastsell:api-sha-<full_git_sha>
   ghcr.io/<owner>/fastsell:web-sha-<full_git_sha>
   ghcr.io/<owner>/fastsell:system-agent-sha-<full_git_sha>
   ```

7. The same workflow uploads a candidate setup bundle and candidate release manifest as GitHub Actions artifacts.
8. Install the candidate on a staging host:

   ```bash
   ./scripts/release/install_candidate.sh <full_git_sha>
   ```

9. Test the candidate once on staging.
10. If QA fails, create a revert PR:

    ```bash
    ./scripts/release/fail_qa.sh <full_git_sha>
    ```

11. If QA succeeds, promote the tested candidate:

    ```bash
    ./scripts/release/success_qa.sh <full_git_sha>
    ```

    The script prompts for a production version in `vX.Y.Z` format and triggers the `Promote Release` workflow.

## Branch Protection

Light branch protection on `main` is optional but recommended. It makes the expected PR validation environment explicit without changing the release workflow.

Configure it with:

```bash
./scripts/admin/configure_main_branch_protection.sh --dry-run
./scripts/admin/configure_main_branch_protection.sh
```

The script uses classic branch protection through `gh api`. It requires the current PR checks, leaves required reviews disabled, does not enforce admins, disables force pushes and branch deletion, and does not restrict push actors.

Before applying it, verify the exact check names from a recent pull request:

```bash
gh pr checks <pr-number> --json name,state,workflow --jq '.[] | [.workflow, .name, .state] | @tsv'
```

If GitHub reports different check names, edit `REQUIRED_CHECK_CONTEXTS` near the top of `scripts/admin/configure_main_branch_protection.sh` before running it for real. This is repository administration and should not be run as part of every release.

## Tags

Candidate tags are full-SHA tags. They identify the exact `main` commit that was built and tested:

```text
api-sha-<full_git_sha>
web-sha-<full_git_sha>
system-agent-sha-<full_git_sha>
```

Production tags are Semantic Versioning tags:

```text
api-vX.Y.Z
web-vX.Y.Z
system-agent-vX.Y.Z
```

Production promotion retags the exact tested candidate image digests to those production tags. It does not rebuild images.

`latest` is not part of the release path. Existing `latest` references are legacy compatibility defaults for older local/source-checkout flows and should not be treated as production release refs. The release workflows do not publish or move `latest`.

## Release Manifests

Candidate and production manifests are JSON files written under `dist/` during GitHub Actions runs.

Candidate manifests record:

- repository
- source full SHA and short SHA
- generated timestamp
- candidate image refs
- exact image digests
- setup bundle artifact name
- highest migration file detected

Production manifests record:

- release version
- source full SHA
- candidate refs tested
- production refs created
- exact image digests
- setup bundle artifact name
- highest migration file detected

Use the manifest to prove which image digests were tested and promoted.

## Staging Install

`install_candidate.sh` always downloads and extracts the candidate artifact locally first. If these environment variables are not set, it stops there and prints manual next steps:

```bash
FASTSELL_STAGING_HOST
FASTSELL_STAGING_USER
FASTSELL_STAGING_PATH
FASTSELL_STAGING_INSTALL_MODE
```

When `FASTSELL_STAGING_HOST` is set, the script asks before copying the candidate bundle to the staging host and running either `setup/linux/update.sh` or `setup/linux/install.sh`. The default mode is `update`.

Do not commit local staging configuration. Keep staging hostnames, private paths, private network details, and credentials out of the repo.

## QA Failure

Default failure handling creates a revert branch from current `origin/main`, reverts the failed squash-merge commit, pushes the branch, and opens a revert PR:

```bash
./scripts/release/fail_qa.sh <full_git_sha>
```

For an emergency direct revert to `main`, use:

```bash
./scripts/release/fail_qa.sh <full_git_sha> --direct
```

The direct path requires confirmation unless `--yes` is also supplied. Candidate images are not deleted by default.

## Production Promotion

Successful QA is promoted with:

```bash
./scripts/release/success_qa.sh <full_git_sha>
```

The `Promote Release` workflow validates that:

- the version matches `vX.Y.Z`
- candidate images exist
- candidate digests are valid
- production version tags do not already exist

It then retags the candidate digests as production tags, creates a production setup bundle using versioned image refs, writes a production release manifest, writes checksums, and creates or updates the GitHub Release.

## Rollback

App-only rollback is acceptable when the database schema and filesystem data remain compatible. Install or update with a previous production setup bundle, or set the three FastSell image refs back to previous versioned image tags and restart the stack.

Rollback involving database migrations is riskier. Updates run migrations forward. If a release changes schema in a way older code cannot use, app-only rollback may not work. Take a database backup before updates and restore the database backup when schema rollback is required.

Rollback involving filesystem or image data may require restoring files from the same point in time as the database. This matters when a release changes how uploaded images, generated files, exports, or metadata are written. Keep database and file backups aligned when possible.

Always back up before production updates that include migrations or file-data changes.
