#!/usr/bin/env bash
set -euo pipefail

DIRECT=0
YES=0
ALLOW_NON_HEAD=0

usage() {
    cat <<'USAGE'
Usage:
  fail_qa.sh <full-sha> [--direct] [--allow-non-head] [--yes]

Default behavior creates a revert branch and opens a revert PR.
Use --direct only for an explicit emergency revert pushed directly to main.
By default, the SHA must equal origin/main HEAD. Use --allow-non-head only when
you intentionally need to revert an older main commit.
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
    git rev-parse --show-toplevel
}

confirm() {
    local prompt="$1"
    local answer

    if [ "${YES}" -eq 1 ]; then
        return
    fi

    read -r -p "${prompt} [y/N] " answer
    case "${answer}" in
        y|Y|yes|YES) ;;
        *) echo "[FAIL] Cancelled."; exit 1 ;;
    esac
}

validate_sha() {
    local sha="$1"
    if [[ ! "${sha}" =~ ^[0-9a-f]{40}$ ]]; then
        echo "[FAIL] Expected a full 40-character lowercase git SHA." >&2
        exit 1
    fi
}

parse_args() {
    if [ "$#" -lt 1 ]; then
        usage >&2
        exit 1
    fi

    SHA="$1"
    shift

    while [ "$#" -gt 0 ]; do
        case "$1" in
            --direct) DIRECT=1; shift ;;
            --allow-non-head) ALLOW_NON_HEAD=1; shift ;;
            --yes|-y) YES=1; shift ;;
            -h|--help) usage; exit 0 ;;
            *) echo "[FAIL] Unknown argument: $1" >&2; usage >&2; exit 1 ;;
        esac
    done
}

ensure_clean_worktree() {
    if [ -n "$(git status --porcelain)" ]; then
        echo "[FAIL] Working tree has uncommitted changes. Commit or stash them before reverting." >&2
        exit 1
    fi
}

print_candidate_refs() {
    local sha="$1"
    local owner="$2"
    local manifest="dist/release-candidates/${sha}/artifact/fastsell-release-candidate-${sha}.json"

    echo "[OK] Failed candidate image refs:"
    echo "     ghcr.io/${owner}/fastsell:api-sha-${sha}"
    echo "     ghcr.io/${owner}/fastsell:web-sha-${sha}"
    echo "     ghcr.io/${owner}/fastsell:system-agent-sha-${sha}"
    if [ -f "${manifest}" ]; then
        echo "[OK] Local candidate manifest: ${manifest}"
    fi
}

ensure_revert_target_is_current_main_head() {
    local provided_sha="$1"
    local origin_main_head

    origin_main_head="$(git rev-parse origin/main)"
    if [ "${provided_sha}" = "${origin_main_head}" ]; then
        return
    fi

    echo "[FAIL] Failed QA SHA must equal current origin/main HEAD by default." >&2
    echo "       provided SHA:        ${provided_sha}" >&2
    echo "       origin/main HEAD:    ${origin_main_head}" >&2

    if [ "${ALLOW_NON_HEAD}" -eq 0 ]; then
        echo "       Use --allow-non-head only if you intentionally need to revert an older main commit." >&2
        exit 1
    fi

    confirm "Revert non-head main commit ${provided_sha:0:12}?"
}

ensure_revert_branch_absent() {
    local branch="$1"
    local ls_remote_status

    if git show-ref --verify --quiet "refs/heads/${branch}"; then
        echo "[FAIL] Revert branch already exists locally: ${branch}" >&2
        exit 1
    fi

    set +e
    git ls-remote --exit-code --heads origin "${branch}" >/dev/null 2>&1
    ls_remote_status="$?"
    set -e

    if [ "${ls_remote_status}" -eq 0 ]; then
        echo "[FAIL] Revert branch already exists on origin: ${branch}" >&2
        exit 1
    fi
    if [ "${ls_remote_status}" -ne 2 ]; then
        echo "[FAIL] Could not verify whether revert branch exists on origin: ${branch}" >&2
        exit 1
    fi
}

main() {
    parse_args "$@"
    require_cmd git
    require_cmd gh

    validate_sha "${SHA}"

    cd "$(repo_root)"
    ensure_clean_worktree

    git fetch origin main
    if ! git merge-base --is-ancestor "${SHA}" origin/main; then
        echo "[FAIL] ${SHA} is not reachable from origin/main." >&2
        exit 1
    fi
    ensure_revert_target_is_current_main_head "${SHA}"

    local short_sha
    local owner
    short_sha="${SHA:0:12}"
    owner="$(gh repo view --json owner --jq .owner.login | tr '[:upper:]' '[:lower:]')"
    print_candidate_refs "${SHA}" "${owner}"

    if [ "${DIRECT}" -eq 1 ]; then
        confirm "Directly revert ${short_sha} on main and push to origin/main?"
        git switch main
        git pull --ff-only origin main
        git revert --no-edit "${SHA}"
        git push origin main
        echo "[OK] Direct revert pushed to main."
        exit 0
    fi

    local branch
    branch="revert/failed-candidate-${short_sha}"
    ensure_revert_branch_absent "${branch}"

    echo "[OK] Creating revert branch ${branch}"
    git switch -c "${branch}" origin/main
    git revert --no-edit "${SHA}"
    git push -u origin "${branch}"

    local title
    local body
    title="Revert failed QA candidate ${short_sha}"
    body="Reverts failed staging QA candidate ${SHA}.

Candidate images were not deleted:
- ghcr.io/${owner}/fastsell:api-sha-${SHA}
- ghcr.io/${owner}/fastsell:web-sha-${SHA}
- ghcr.io/${owner}/fastsell:system-agent-sha-${SHA}"

    gh pr create --title "${title}" --body "${body}" --base main --head "${branch}"
    echo "[OK] Revert PR created for failed QA candidate ${short_sha}."
}

main "$@"
