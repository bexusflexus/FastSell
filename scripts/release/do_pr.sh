#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  do_pr.sh [commit message]
  do_pr.sh --help

Runs the normal single-maintainer FastSell PR flow from the current feature branch:
  git add . -> commit -> push/create or reuse PR -> watch checks -> squash merge -> update local main

If no commit message is provided, the current branch name is used.
USAGE
}

require_cmd() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        echo "[FAIL] Required command is missing: ${cmd}" >&2
        exit 1
    fi
}

repo_root() {
    local root

    if ! root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
        echo "[FAIL] This script must run from inside a git repository." >&2
        exit 1
    fi

    printf '%s' "${root}"
}

current_branch() {
    local branch

    branch="$(git branch --show-current)"
    if [ -z "${branch}" ]; then
        echo "[FAIL] This script must run from a named branch." >&2
        exit 1
    fi
    if [ "${branch}" = "main" ]; then
        echo "[FAIL] Refusing to run the PR flow from main." >&2
        exit 1
    fi

    printf '%s' "${branch}"
}

check_auth() {
    if ! gh auth status >/dev/null 2>&1; then
        echo "[FAIL] GitHub CLI is not authenticated. Run gh auth login, then retry." >&2
        exit 1
    fi
}

find_pr_number() {
    local pr_number

    if ! pr_number="$(gh pr view --json number --jq '.number' 2>/dev/null)"; then
        echo "[FAIL] Could not find a pull request for the current branch after creation attempt." >&2
        exit 1
    fi
    if [ -z "${pr_number}" ]; then
        echo "[FAIL] Pull request number is empty after creation attempt." >&2
        exit 1
    fi

    printf '%s' "${pr_number}"
}

print_candidate_next_steps() {
    local main_sha="$1"

    cat <<NEXT
[8/8] Candidate QA next step
Main commit: ${main_sha}

Use ~/fastsell-install as an example setup workspace:

  cd ~/fastsell-install/dev_only
  ./fetch_candidate_bundle.sh ${main_sha}

  cd ~/fastsell-install
  sudo bash setup/linux/update.sh
NEXT
}

main() {
    if [ "${1:-}" = "--help" ]; then
        usage
        exit 0
    fi
    if [[ "${1:-}" == --* ]]; then
        echo "[FAIL] Unsupported option: $1" >&2
        usage >&2
        exit 1
    fi
    require_cmd git
    require_cmd gh

    local root
    root="$(repo_root)"
    cd "${root}"

    echo "[1/8] Checking repo, branch, and GitHub auth"
    check_auth

    local branch
    branch="$(current_branch)"
    echo "[OK] Current branch: ${branch}"

    echo "[OK] Fetching origin/main"
    git fetch origin main

    local commit_message
    commit_message="$*"
    if [ -z "${commit_message}" ]; then
        commit_message="${branch}"
    fi

    echo "[2/8] Inspecting local changes"
    echo "[OK] git status --short --untracked-files=all:"
    local status_output
    status_output="$(git status --short --untracked-files=all)"
    if [ -n "${status_output}" ]; then
        printf '%s\n' "${status_output}"
    else
        echo "  clean"
    fi

    local behind_count
    local ahead_count
    read -r behind_count ahead_count < <(git rev-list --left-right --count origin/main...HEAD)
    echo "[OK] Branch state relative to origin/main: ahead ${ahead_count}, behind ${behind_count}"

    echo "[3/8] Staging and committing changes"
    if [ -n "${status_output}" ]; then
        git add .
        git diff --cached --check

        if git diff --cached --quiet; then
            echo "[OK] No staged changes after git add ."
        else
            echo "[OK] Committing with message: ${commit_message}"
            git commit -m "${commit_message}"
        fi
    else
        echo "[OK] No local changes to commit."
    fi

    read -r behind_count ahead_count < <(git rev-list --left-right --count origin/main...HEAD)
    if [ "${ahead_count}" -eq 0 ]; then
        echo "[FAIL] No local changes and no commits ahead of origin/main. Nothing to submit." >&2
        exit 1
    fi
    echo "[OK] Branch is ready: ahead ${ahead_count}, behind ${behind_count}"

    echo "[4/8] Creating or reusing pull request"
    bash scripts/release/create_pull_req.sh

    local pr_number
    pr_number="$(find_pr_number)"
    echo "[OK] Using PR #${pr_number}"

    echo "[5/8] Watching PR checks"
    if ! gh pr checks "${pr_number}" --watch; then
        echo "[FAIL] Pull request checks did not pass. Not merging." >&2
        exit 1
    fi

    echo "[6/8] Squash merging pull request"
    bash scripts/release/squash_merge_pull_req.sh "${pr_number}" --yes

    echo "[7/8] Updating local main"
    git switch main
    git pull --ff-only origin main

    local main_sha
    main_sha="$(git rev-parse HEAD)"
    echo "[OK] Local main is at ${main_sha}"

    print_candidate_next_steps "${main_sha}"
}

main "$@"
