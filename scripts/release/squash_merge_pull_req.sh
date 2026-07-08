#!/usr/bin/env bash
set -euo pipefail

YES=0
DELETE_BRANCH=0
PR=""

usage() {
    cat <<'USAGE'
Usage:
  squash_merge_pull_req.sh [pr-number] [--yes] [--delete-branch]
USAGE
}

require_cmd() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        echo "[FAIL] Required command is missing: ${cmd}" >&2
        exit 1
    fi
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

repo_root() {
    git rev-parse --show-toplevel
}

run_pr_check_gate() {
    local pr="$1"
    local required_output
    local required_status
    local full_output
    local full_status

    echo "[OK] Required check status:"
    set +e
    required_output="$(gh pr checks "${pr}" --required 2>&1)"
    required_status="$?"
    set -e

    if [ "${required_status}" -eq 0 ]; then
        printf '%s\n' "${required_output}"
        return
    fi

    if ! printf '%s\n' "${required_output}" | rg -q "no required checks reported"; then
        printf '%s\n' "${required_output}" >&2
        echo "[FAIL] Required checks are not passing." >&2
        echo "[OK] Full check status:"
        gh pr checks "${pr}" || true
        exit 1
    fi

    printf '%s\n' "${required_output}"
    echo "[OK] No required checks are configured; checking all PR checks instead."

    set +e
    full_output="$(gh pr checks "${pr}" 2>&1)"
    full_status="$?"
    set -e
    printf '%s\n' "${full_output}"

    if [ "${full_status}" -ne 0 ]; then
        echo "[FAIL] One or more PR checks are failing, pending, or unavailable." >&2
        exit 1
    fi
}

ensure_clean_worktree() {
    if [ -n "$(git status --porcelain)" ]; then
        echo "[FAIL] Working tree has uncommitted changes. Commit or stash them before merging a pull request." >&2
        exit 1
    fi
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --yes|-y) YES=1; shift ;;
            --delete-branch) DELETE_BRANCH=1; shift ;;
            -h|--help) usage; exit 0 ;;
            *)
                if [ -n "${PR}" ]; then
                    echo "[FAIL] Unexpected argument: $1" >&2
                    usage >&2
                    exit 1
                fi
                PR="$1"
                shift
                ;;
        esac
    done
}

main() {
    parse_args "$@"
    require_cmd git
    require_cmd gh
    require_cmd rg

    cd "$(repo_root)"
    ensure_clean_worktree

    if [ -z "${PR}" ]; then
        PR="$(gh pr view --json number --jq .number)"
    fi

    local is_draft
    is_draft="$(gh pr view "${PR}" --json isDraft --jq .isDraft)"

    echo "[OK] Pull request details:"
    gh pr view "${PR}" --json number,title,headRefName,author,url,isDraft \
        --template '  #{{.number}} {{.title}}{{"\n"}}  branch: {{.headRefName}}{{"\n"}}  author: {{.author.login}}{{"\n"}}  draft: {{.isDraft}}{{"\n"}}  url: {{.url}}{{"\n"}}'

    if [ "${is_draft}" = "true" ]; then
        echo "[FAIL] Refusing to merge a draft pull request." >&2
        exit 1
    fi

    run_pr_check_gate "${PR}"

    confirm "Squash merge PR ${PR}?"

    local merge_args
    merge_args=(--squash)
    if [ "${DELETE_BRANCH}" -eq 1 ]; then
        merge_args+=(--delete-branch)
    fi

    gh pr merge "${PR}" "${merge_args[@]}"

    echo "[OK] Updating local main"
    git fetch origin main
    git switch main
    git pull --ff-only origin main

    local source_sha
    local owner
    source_sha="$(git rev-parse HEAD)"
    owner="$(gh repo view --json owner --jq .owner.login | tr '[:upper:]' '[:lower:]')"

    echo "[OK] Main commit: ${source_sha}"
    echo "[OK] Expected candidate image tags:"
    echo "     ghcr.io/${owner}/fastsell:api-sha-${source_sha}"
    echo "     ghcr.io/${owner}/fastsell:web-sha-${source_sha}"
    echo "     ghcr.io/${owner}/fastsell:system-agent-sha-${source_sha}"
    echo "[OK] Next: wait for Publish Images, then run <install-root>/dev_only/fetch_candidate_bundle.sh ${source_sha}"
}

main "$@"
