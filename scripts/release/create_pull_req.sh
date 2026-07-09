#!/usr/bin/env bash
set -euo pipefail

require_cmd() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        echo "[FAIL] Required command is missing: ${cmd}" >&2
        exit 1
    fi
}

repo_root() {
    git rev-parse --show-toplevel
}

ensure_clean_worktree() {
    if [ -n "$(git status --porcelain)" ]; then
        echo "[FAIL] Working tree has uncommitted changes. Commit or stash them before creating a pull request." >&2
        exit 1
    fi
}

main() {
    require_cmd git
    require_cmd gh

    cd "$(repo_root)"
    ensure_clean_worktree

    local branch
    branch="$(git branch --show-current)"
    if [ -z "${branch}" ]; then
        echo "[FAIL] This script must run from a named branch." >&2
        exit 1
    fi
    if [ "${branch}" = "main" ]; then
        echo "[FAIL] Refusing to create a pull request from main." >&2
        exit 1
    fi

    if [ "${FASTSELL_SKIP_PR_HELPER_PUSH:-0}" = "1" ]; then
        echo "[OK] Branch was already pushed by caller."
    else
        echo "[OK] Pushing current branch to origin/${branch}"
        git push -u origin "${branch}"
    fi

    if existing_url="$(gh pr view --json url --jq .url 2>/dev/null)"; then
        echo "[OK] Pull request already exists: ${existing_url}"
        echo "[OK] Next: ./scripts/release/squash_merge_pull_req.sh"
        exit 0
    fi

    echo "[OK] Creating pull request for ${branch}"
    gh pr create --fill

    local pr_url
    pr_url="$(gh pr view --json url --jq .url)"
    echo "[OK] Pull request: ${pr_url}"
    echo "[OK] Next: wait for checks, then run ./scripts/release/squash_merge_pull_req.sh"
}

main "$@"
