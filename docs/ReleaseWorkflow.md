# Release Workflow

FastSell releases use GitHub Actions, GHCR, Docker Compose, and setup bundles. The release path intentionally separates merge-to-main candidate publishing from production promotion so staging QA happens once and production tags move only after approval.

## Normal Flow

1. Create a feature branch locally.
2. Make your changes.
3. Run the normal single-maintainer PR workflow:

   ```bash
   ./scripts/release/do_pr.sh "Commit message"
   ```

   If no commit message is provided, the current branch name is used:

   ```bash
   ./scripts/release/do_pr.sh
   ```

   This runs `git add .`, commits, pushes the current branch, creates or reuses a pull request, waits for GitHub checks to register, watches checks, squash merges after checks pass, updates local `main`, and prints candidate QA instructions. No manual `git push origin` or `gh pr checks --watch` is needed when using `do_pr.sh`. If the branch already has a pull request, local commits are still pushed before checks and merge.

   Advanced/manual primitives remain available:

   ```bash
   ./scripts/release/create_pull_req.sh
   ./scripts/release/squash_merge_pull_req.sh <pr-number>
   ```

   Use the lower-level scripts when you want a more review-oriented or enterprise-friendly flow where PR creation, GitHub review, and merge are separate steps.

4. The `Publish Images` workflow runs on the resulting `main` commit and publishes candidate images:

   ```text
   ghcr.io/<owner>/fastsell:api-sha-<full_git_sha>
   ghcr.io/<owner>/fastsell:web-sha-<full_git_sha>
   ghcr.io/<owner>/fastsell:system-agent-sha-<full_git_sha>
   ```

5. The same workflow uploads a candidate setup bundle and candidate release manifest as GitHub Actions artifacts.
6. Install the candidate helper into the staging setup workspace once if it is not already present. The real helper is source-controlled at `dev_only/fetch_candidate_bundle.sh`, is not included in normal production setup bundles, and self-refreshes from that same path at the requested candidate SHA before doing candidate QA work.

   ```bash
   mkdir -p ~/fastsell-install/dev_only
   cp -p dev_only/fetch_candidate_bundle.sh ~/fastsell-install/dev_only/fetch_candidate_bundle.sh
   chmod +x ~/fastsell-install/dev_only/fetch_candidate_bundle.sh
   ```

   If your repo checkout and staging host are different machines, copy `dev_only/fetch_candidate_bundle.sh` to the staging setup workspace with `scp` or `rsync`. Then fetch and apply the candidate setup-bundle files from inside the staging setup workspace:

   ```bash
   cd ~/fastsell-install/dev_only
   ./fetch_candidate_bundle.sh <full_git_sha>

   cd ~/fastsell-install
   sudo bash setup/linux/update.sh
   ```

7. Test the candidate once on staging.
8. If QA fails, create a revert PR:

    ```bash
    ./scripts/release/fail_qa.sh <full_git_sha>
    ```

9. If QA succeeds, promote the tested candidate:

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

## Staging Candidate Setup Workspace

Candidate testing uses the same stable setup workspace model as normal installs. A FastSell setup workspace is the extracted setup-bundle tree, usually `~/fastsell-install`, that contains files such as:

```text
docker-compose.yml
.env.example
setup/linux/update.sh
db/
docker/
docs/
```

Developer-only candidate tooling lives under `~/fastsell-install/dev_only/`. Normal production setup bundles do not include `dev_only`, and candidate bundle application preserves the existing `dev_only` directory. The real helper is source-controlled at `dev_only/fetch_candidate_bundle.sh`.

To install the helper into an existing staging setup workspace from a repo checkout:

```bash
mkdir -p ~/fastsell-install/dev_only
cp -p dev_only/fetch_candidate_bundle.sh ~/fastsell-install/dev_only/fetch_candidate_bundle.sh
chmod +x ~/fastsell-install/dev_only/fetch_candidate_bundle.sh
```

If your repo checkout and staging host are different machines, copy `dev_only/fetch_candidate_bundle.sh` to the staging setup workspace with `scp` or `rsync`.

This manual copy is only needed to bootstrap a staging setup workspace. On each candidate run, the helper downloads `dev_only/fetch_candidate_bundle.sh` from the exact requested candidate SHA, refreshes itself if needed, and restarts itself before continuing.

On the staging host, run:

```bash
cd ~/fastsell-install/dev_only
./fetch_candidate_bundle.sh <full_git_sha>
```

The helper:

- locates the `publish-images.yml` workflow run for the full SHA
- waits briefly if the workflow run is not visible yet
- offers to watch the run if it is queued or in progress
- downloads the `fastsell-candidate-<full_git_sha>` artifact after success
- stores the artifact under `~/fastsell-install/dev_only/candidates/<full_git_sha>/`
- prints the candidate manifest and image refs
- applies candidate setup-bundle files into the setup workspace
- preserves `~/fastsell-install/.env` and `~/fastsell-install/dev_only/`
- does not touch runtime data directly
- does not run `update.sh`

By default the helper reads artifacts from `bexusflexus/FastSell`. Set `FASTSELL_GITHUB_REPO=owner/repo` before running it if testing a fork.

After applying the candidate files, run the normal update command. This is the step that updates the actual `/srv/fastsell` runtime:

```bash
cd ~/fastsell-install
sudo bash setup/linux/update.sh
```

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
