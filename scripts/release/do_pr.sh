#!/usr/bin/env bash
set -euo pipefail

NO_WATCH=0

usage() {
    cat <<'USAGE'
Usage:
  do_pr.sh [--no-watch]

Runs the normal FastSell PR flow from the current feature branch:
  verify branch/worktree -> create or reuse PR -> watch checks -> ask before squash merge -> update local main

Options:
  --no-watch   Create or reuse the PR, then print the next command without watching or merging.
  --help       Show this help.
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

ensure_clean_worktree() {
    if [ -n "$(git status --porcelain)" ]; then
        echo "[FAIL] Working tree has uncommitted changes. Commit or stash them before running the PR flow." >&2
        exit 1
    fi
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

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --no-watch)
                NO_WATCH=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                echo "[FAIL] Unknown argument: $1" >&2
                usage >&2
                exit 1
                ;;
        esac
    done
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

watch_checks() {
    local pr_number="$1"
    local watch_status

    set +e
    gh pr checks "${pr_number}" --watch
    watch_status="$?"
    set -e

    if [ "${watch_status}" -ne 0 ]; then
        echo "[WARN] Check watch exited non-zero. Final merge gate will verify current PR check status."
    fi
}

print_candidate_next_steps() {
    local main_sha="$1"

    cat <<NEXT
[6/6] Candidate QA next step
Use ~/fastsell-install as an example setup workspace:

  cd ~/fastsell-install/dev_only
  ./fetch_candidate_bundle.sh ${main_sha}

  cd ~/fastsell-install
  sudo bash setup/linux/update.sh
NEXT
}

main() {
    parse_args "$@"
    require_cmd git
    require_cmd gh

    local root
    root="$(repo_root)"
    cd "${root}"

    echo "[1/6] Checking branch and worktree"
    check_auth
    local branch
    branch="$(current_branch)"
    ensure_clean_worktree
    echo "[OK] Current branch: ${branch}"

    echo "[2/6] Creating or finding pull request"
    bash scripts/release/create_pull_req.sh

    local pr_number
    pr_number="$(find_pr_number)"
    echo "[OK] Using PR #${pr_number}"

    if [ "${NO_WATCH}" -eq 1 ]; then
        cat <<NEXT
[OK] --no-watch requested. PR is ready for manual checks.
[OK] Next command:
     ./scripts/release/squash_merge_pull_req.sh ${pr_number}
NEXT
        exit 0
    fi

    echo "[3/6] Watching PR checks"
    watch_checks "${pr_number}"

    echo "[4/6] Confirming squash merge"
    echo "[OK] Final check status and merge confirmation are handled by squash_merge_pull_req.sh."
    bash scripts/release/squash_merge_pull_req.sh "${pr_number}"

    echo "[5/6] Updating local main"
    local main_sha
    main_sha="$(git rev-parse main)"
    echo "[OK] Local main is at ${main_sha}"

    print_candidate_next_steps "${main_sha}"
}

main "$@"
