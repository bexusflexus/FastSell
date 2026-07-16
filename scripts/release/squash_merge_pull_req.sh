#!/usr/bin/env bash
set -euo pipefail

YES=0
DELETE_BRANCH=0
PR=""
REPOSITORY=""
OWNER=""
PR_JSON=""
PR_STATE=""
PR_HEAD=""
PR_HEAD_OID=""
PR_MERGE_COMMIT=""
PR_FIELDS="number,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,mergeCommit,title,url,author"

usage() {
    cat <<'USAGE'
Usage:
  squash_merge_pull_req.sh [pr-number] [--yes] [--delete-branch]

Required checks are polled up to 180 times at ten-second intervals (30 minutes)
unless FASTSELL_RELEASE_CHECK_ATTEMPTS or FASTSELL_RELEASE_CHECK_SLEEP_SECONDS
is set to a stricter test or operator value.
USAGE
}

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# The private library is resolved from BASH_SOURCE so the entry point works
# from any directory; ShellCheck cannot resolve that dynamic absolute path.
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/pr_workflow_lib.sh"

require_cmd() {
    local cmd="$1"
    command -v "${cmd}" >/dev/null 2>&1 || fail "Required command is missing: ${cmd}"
}

confirm() {
    local prompt="$1"
    local answer
    if [ "${YES}" -eq 1 ]; then return 0; fi
    if ! read -r -p "${prompt} [y/N] " answer; then
        fail "Confirmation input was unavailable."
    fi
    case "${answer}" in
        y|Y|yes|YES) ;;
        *) fail "Cancelled." ;;
    esac
}

repo_root() {
    local root
    if ! root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
        fail "This script must run from inside a git repository."
    fi
    printf '%s\n' "${root}"
}

ensure_clean_worktree() {
    local status_output
    if ! status_output="$(git status --porcelain 2>&1)"; then
        fail "Could not inspect the working tree: ${status_output}"
    fi
    [ -z "${status_output}" ] || fail "Working tree has uncommitted changes. Commit or stash them before merging a pull request."
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --yes|-y) YES=1 ;;
            --delete-branch) DELETE_BRANCH=1 ;;
            -h|--help) usage; exit 0 ;;
            --*) fail "Unknown option: $1" ;;
            *)
                [ -z "${PR}" ] || fail "Unexpected argument: $1"
                PR="$1"
                ;;
        esac
        shift
    done
}

load_pr() {
    local selector="$1"
    if ! PR_JSON="$(gh pr view "${selector}" --repo "${REPOSITORY}" --json "${PR_FIELDS}" 2>&1)"; then
        fail "Could not load PR ${selector}: ${PR_JSON}"
    fi
    if ! PR="$(jq -er '.number' <<<"${PR_JSON}" 2>&1)"; then fail "PR JSON has no number: ${PR}"; fi
    if ! PR_STATE="$(jq -er '.state' <<<"${PR_JSON}" 2>&1)"; then fail "PR JSON has no state: ${PR_STATE}"; fi
    if ! PR_HEAD="$(jq -er '.headRefName' <<<"${PR_JSON}" 2>&1)"; then fail "PR JSON has no head branch: ${PR_HEAD}"; fi
    if ! PR_HEAD_OID="$(jq -er '.headRefOid' <<<"${PR_JSON}" 2>&1)"; then fail "PR JSON has no head OID: ${PR_HEAD_OID}"; fi
    if ! PR_MERGE_COMMIT="$(jq -r '.mergeCommit.oid // empty' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR merge commit: ${PR_MERGE_COMMIT}"; fi
}

validate_pr() {
    local expected_state="$1"
    local expected_head_oid="$2"
    local expected_base_oid="${3:-}"
    local is_draft
    local base
    local head_owner
    local cross_repository
    local actual_base_oid

    [[ "${PR}" =~ ^[0-9]+$ ]] || fail "Invalid PR number: ${PR}"
    [ "${PR_STATE}" = "${expected_state}" ] || fail "PR #${PR} is ${PR_STATE}; expected ${expected_state}."
    if ! is_draft="$(jq -r '.isDraft' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read draft state: ${is_draft}"; fi
    if ! base="$(jq -er '.baseRefName' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read base branch: ${base}"; fi
    if ! head_owner="$(jq -er '.headRepositoryOwner.login' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read head owner: ${head_owner}"; fi
    if ! cross_repository="$(jq -r '.isCrossRepository' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read repository relationship: ${cross_repository}"; fi
    [[ "${is_draft}" =~ ^(true|false)$ ]] || fail "PR #${PR} returned invalid draft state ${is_draft}."
    [[ "${cross_repository}" =~ ^(true|false)$ ]] || fail "PR #${PR} returned invalid cross-repository state ${cross_repository}."
    [ "${is_draft}" = "false" ] || fail "Refusing to merge draft PR #${PR}."
    [ "${base}" = "main" ] || fail "PR #${PR} targets ${base}, not main."
    [ "${cross_repository}" = "false" ] || fail "PR #${PR} is cross-repository."
    [ "${head_owner,,}" = "${OWNER,,}" ] || fail "PR #${PR} head owner ${head_owner} is not ${OWNER}."
    [ "${PR_HEAD_OID}" = "${expected_head_oid}" ] || fail "PR #${PR} head changed: expected ${expected_head_oid}, found ${PR_HEAD_OID}."
    if [ -n "${expected_base_oid}" ]; then
        if ! actual_base_oid="$(jq -er '.baseRefOid' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read base OID: ${actual_base_oid}"; fi
        [ "${actual_base_oid}" = "${expected_base_oid}" ] || fail "PR #${PR} base changed: expected ${expected_base_oid}, found ${actual_base_oid}."
    fi
}

main() {
    local root
    local resolved_repository
    local expected_head
    local expected_base
    local merge_args=(--squash)
    local merge_output
    local source_sha
    local origin_main
    local title
    local url

    parse_args "$@"
    require_cmd git
    require_cmd gh
    require_cmd jq

    if ! root="$(repo_root)"; then return 1; fi
    cd "${root}"
    ensure_clean_worktree
    if ! gh auth status >/dev/null 2>&1; then fail "GitHub CLI is not authenticated."; fi
    if ! REPOSITORY="$(release_origin_repository)"; then fail "Origin must be a recognized GitHub repository remote."; fi
    OWNER="${REPOSITORY%%/*}"
    if ! resolved_repository="$(gh repo view --repo "${REPOSITORY}" --json nameWithOwner --jq .nameWithOwner 2>&1)"; then
        fail "Could not resolve GitHub repository ${REPOSITORY}: ${resolved_repository}"
    fi
    [ "${resolved_repository,,}" = "${REPOSITORY,,}" ] || fail "GitHub repository ${resolved_repository} does not match origin ${REPOSITORY}."

    if [ -z "${PR}" ]; then
        if ! PR="$(gh pr view --repo "${REPOSITORY}" --json number --jq .number 2>&1)"; then
            fail "Could not identify the PR for the current branch: ${PR}"
        fi
    fi
    load_pr "${PR}"
    git check-ref-format --branch "${PR_HEAD}" >/dev/null 2>&1 || fail "PR #${PR} has invalid head branch ${PR_HEAD}."
    git fetch origin main
    git fetch origin "refs/heads/${PR_HEAD}:refs/remotes/origin/${PR_HEAD}"
    if ! expected_head="$(git rev-parse "origin/${PR_HEAD}" 2>&1)"; then fail "Could not resolve origin/${PR_HEAD}: ${expected_head}"; fi
    if ! expected_base="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve origin/main: ${expected_base}"; fi
    validate_pr OPEN "${expected_head}" "${expected_base}"

    if ! title="$(jq -er '.title' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR title: ${title}"; fi
    if ! url="$(jq -er '.url' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR URL: ${url}"; fi
    echo "[OK] Pull request #${PR}: ${title}"
    echo "[OK] branch: ${PR_HEAD}"
    echo "[OK] url: ${url}"

    release_wait_for_required_checks "${REPOSITORY}" main "${PR}"
    git fetch origin main
    if ! origin_main="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve pre-merge origin/main: ${origin_main}"; fi
    [ "${origin_main}" = "${expected_base}" ] || fail "origin/main changed while PR #${PR} checks ran; rerun after reviewing the new base."
    load_pr "${PR}"
    validate_pr OPEN "${expected_head}" "${expected_base}"
    confirm "Squash merge PR #${PR} into main at ${expected_head}?"

    if [ "${DELETE_BRANCH}" -eq 1 ]; then merge_args+=(--delete-branch); fi
    merge_args+=(--match-head-commit "${expected_head}")
    if ! merge_output="$(gh pr merge "${PR}" --repo "${REPOSITORY}" "${merge_args[@]}" 2>&1)"; then
        fail "Could not merge PR #${PR}: ${merge_output}"
    fi
    load_pr "${PR}"
    validate_pr MERGED "${expected_head}"
    [ -n "${PR_MERGE_COMMIT}" ] || fail "Merged PR #${PR} has no merge commit."

    echo "[OK] Updating local main"
    git fetch origin main
    git switch main
    git pull --ff-only origin main
    if ! source_sha="$(git rev-parse HEAD 2>&1)"; then fail "Could not resolve local main: ${source_sha}"; fi
    if ! origin_main="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve origin/main: ${origin_main}"; fi
    [ "${source_sha}" = "${origin_main}" ] || fail "Local main is not synchronized with origin/main."
    [ "${source_sha}" = "${PR_MERGE_COMMIT}" ] || fail "main advanced after PR #${PR} merged; inspect the repository before candidate QA."

    echo "[OK] Main commit: ${source_sha}"
    echo "[OK] Expected candidate image tags:"
    echo "     ghcr.io/${OWNER,,}/fastsell:api-sha-${source_sha}"
    echo "     ghcr.io/${OWNER,,}/fastsell:web-sha-${source_sha}"
    echo "     ghcr.io/${OWNER,,}/fastsell:system-agent-sha-${source_sha}"
    echo "[OK] Next: wait for Publish Images, then fetch the candidate bundle for ${source_sha}."
}

main "$@"
