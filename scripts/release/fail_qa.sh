#!/usr/bin/env bash
set -euo pipefail

DIRECT=0
ROLLBACK_ONLY=0
YES=0
ALLOW_NON_HEAD=0
SHA=""
RETRY_BRANCH=""
REVERT_BRANCH=""
REVERT_BASE_SHA=""
EXPECTED_REVERT_HEAD=""
PR_NUMBER=""
PR_STATE=""
PR_JSON=""
PR_MERGE_COMMIT=""
REPOSITORY=""
OWNER=""
PR_FIELDS="number,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,mergeCommit"

usage() {
    cat <<'USAGE'
Usage:
  fail_qa.sh <full-sha> <retry-branch-name> [--yes]
  fail_qa.sh <full-sha> --rollback-only [--yes]
  fail_qa.sh <full-sha> --direct [--yes]

The default workflow creates or reuses a checked revert PR, merges it, updates
local main, creates the retry branch, and reapplies the failed candidate.

Use --rollback-only to stop on synchronized main after the checked revert.
Use --direct only for an explicitly confirmed emergency revert pushed to main.
By default, a new rollback must target origin/main HEAD. --allow-non-head keeps
the guarded escape hatch for intentionally reverting an older commit.
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

repo_root() {
    local root
    if ! root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
        fail "This script must run from inside a git repository."
    fi
    printf '%s\n' "${root}"
}

confirm() {
    local prompt="$1"
    local answer

    if [ "${YES}" -eq 1 ]; then
        return 0
    fi
    if ! read -r -p "${prompt} [y/N] " answer; then
        fail "Confirmation input was unavailable; rerun interactively or use --yes only after reviewing the operation."
    fi
    case "${answer}" in
        y|Y|yes|YES) ;;
        *) fail "Cancelled." ;;
    esac
}

validate_sha() {
    [[ "$1" =~ ^[0-9a-f]{40}$ ]] || fail "Expected a full 40-character lowercase git SHA."
}

validate_retry_branch() {
    local branch="$1"
    [ "${branch}" != "main" ] || fail "Retry branch cannot be main."
    git check-ref-format --branch "${branch}" >/dev/null 2>&1 || fail "Invalid retry branch name: ${branch}"
}

parse_args() {
    [ "$#" -ge 1 ] || { usage >&2; exit 1; }
    if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
        usage
        exit 0
    fi
    SHA="$1"
    shift

    while [ "$#" -gt 0 ]; do
        case "$1" in
            --direct) DIRECT=1 ;;
            --rollback-only) ROLLBACK_ONLY=1 ;;
            --allow-non-head) ALLOW_NON_HEAD=1 ;;
            --yes|-y) YES=1 ;;
            -h|--help) usage; exit 0 ;;
            --*) fail "Unknown option: $1" ;;
            *)
                [ -z "${RETRY_BRANCH}" ] || fail "Only one retry branch name may be provided."
                RETRY_BRANCH="$1"
                ;;
        esac
        shift
    done

    [ "${DIRECT}" -eq 0 ] || [ "${ROLLBACK_ONLY}" -eq 0 ] || fail "--direct and --rollback-only cannot be combined."
    if [ "${DIRECT}" -eq 1 ] || [ "${ROLLBACK_ONLY}" -eq 1 ]; then
        [ -z "${RETRY_BRANCH}" ] || fail "A retry branch cannot be combined with --direct or --rollback-only."
        return 0
    fi
    if [ -z "${RETRY_BRANCH}" ]; then
        if [ -t 0 ]; then
            if ! read -r -p "Retry branch name: " RETRY_BRANCH; then
                fail "Retry branch input was unavailable."
            fi
        else
            fail "Retry branch name is required when input is not interactive."
        fi
    fi
}

report_in_progress_revert() {
    local revert_head_path
    if ! revert_head_path="$(git rev-parse --git-path REVERT_HEAD 2>/dev/null)"; then
        fail "Could not inspect git revert state."
    fi
    if [ -f "${revert_head_path}" ]; then
        cat >&2 <<'RECOVERY'
[FAIL] A git revert is already in progress. The repository was not changed further.

git status

# Resolve the files, then:
git add <resolved-files>
git revert --continue

# Or abandon:
git revert --abort
RECOVERY
        exit 1
    fi
}

ensure_clean_worktree() {
    local status_output
    if ! status_output="$(git status --porcelain 2>&1)"; then
        fail "Could not inspect the working tree: ${status_output}"
    fi
    [ -z "${status_output}" ] || fail "Working tree has uncommitted changes. Commit or stash them before handling failed QA."
}

check_auth_and_repository() {
    local resolved
    if ! gh auth status >/dev/null 2>&1; then
        fail "GitHub CLI is not authenticated. Run gh auth login, then retry."
    fi
    if ! REPOSITORY="$(release_origin_repository)"; then
        fail "Origin must be a recognized GitHub repository remote."
    fi
    OWNER="${REPOSITORY%%/*}"
    if ! resolved="$(gh repo view "${REPOSITORY}" --json nameWithOwner --jq .nameWithOwner 2>&1)"; then
        fail "Could not resolve GitHub repository ${REPOSITORY}: ${resolved}"
    fi
    [ "${resolved,,}" = "${REPOSITORY,,}" ] || fail "GitHub repository ${resolved} does not match origin ${REPOSITORY}."
    echo "[OK] Repository: ${REPOSITORY}"
}

fetch_origin() {
    echo "[OK] Fetching origin"
    git fetch --prune origin
    git show-ref --verify --quiet refs/remotes/origin/main || fail "origin/main is unavailable after fetch."
}

resolve_candidate() {
    local resolved
    validate_sha "${SHA}"
    git cat-file -e "${SHA}^{commit}" 2>/dev/null || fail "Candidate commit does not exist: ${SHA}"
    if ! resolved="$(git rev-parse "${SHA}^{commit}" 2>&1)"; then
        fail "Could not resolve candidate ${SHA}: ${resolved}"
    fi
    SHA="${resolved}"
    git merge-base --is-ancestor "${SHA}" origin/main || fail "${SHA} is not reachable from origin/main."
    echo "[OK] Failed candidate: ${SHA}"
}

local_branch_exists() {
    git show-ref --verify --quiet "refs/heads/$1"
}

remote_branch_exists() {
    git show-ref --verify --quiet "refs/remotes/origin/$1"
}

patch_id_for_commit() {
    local commit="$1"
    local patch_id
    if ! patch_id="$(git show --pretty=format: "${commit}" | git patch-id --stable | awk 'NR == 1 { print $1 }')"; then
        fail "Could not compute patch ID for commit ${commit}."
    fi
    [ -n "${patch_id}" ] || fail "Commit ${commit} has no patch ID."
    printf '%s\n' "${patch_id}"
}

patch_id_for_diff() {
    local from="$1"
    local to="$2"
    local patch_id
    if ! patch_id="$(git diff "${from}" "${to}" | git patch-id --stable | awk 'NR == 1 { print $1 }')"; then
        fail "Could not compute aggregate patch ID for ${from}..${to}."
    fi
    [ -n "${patch_id}" ] || fail "${from}..${to} has no aggregate patch ID."
    printf '%s\n' "${patch_id}"
}

candidate_patch_id() {
    patch_id_for_commit "${SHA}"
}

expected_revert_patch_id() {
    local patch_id
    if ! patch_id="$(git diff "${SHA}" "${SHA}^" | git patch-id --stable | awk 'NR == 1 { print $1 }')"; then
        fail "Could not compute the expected rollback patch ID for ${SHA}."
    fi
    [ -n "${patch_id}" ] || fail "Candidate ${SHA} has no reversible patch."
    printf '%s\n' "${patch_id}"
}

verify_single_commit_patch() {
    local ref="$1"
    local base="$2"
    local expected_patch="$3"
    local description="$4"
    local tip
    local resolved_base
    local count
    local parent_line
    local -a parents
    local commit_patch
    local aggregate_patch

    if ! tip="$(git rev-parse "${ref}^{commit}" 2>&1)"; then
        fail "Could not resolve ${description} ${ref}: ${tip}"
    fi
    if ! resolved_base="$(git rev-parse "${base}^{commit}" 2>&1)"; then
        fail "Could not resolve expected base ${base}: ${resolved_base}"
    fi
    if ! count="$(git rev-list --count "${resolved_base}..${tip}" 2>&1)"; then
        fail "Could not count commits for ${description}: ${count}"
    fi
    [ "${count}" -eq 1 ] || fail "${description} must contain exactly one commit after ${resolved_base}; found ${count}."
    if ! parent_line="$(git rev-list --parents -n 1 "${tip}" 2>&1)"; then
        fail "Could not inspect parent topology for ${description}: ${parent_line}"
    fi
    read -r -a parents <<<"${parent_line}"
    [ "${#parents[@]}" -eq 2 ] || fail "${description} must be a non-merge commit with exactly one parent."
    [ "${parents[1]}" = "${resolved_base}" ] || fail "${description} parent ${parents[1]} does not equal expected base ${resolved_base}."
    if ! commit_patch="$(patch_id_for_commit "${tip}")"; then
        return 1
    fi
    [ "${commit_patch}" = "${expected_patch}" ] || fail "${description} commit patch is not the expected patch."
    if ! aggregate_patch="$(patch_id_for_diff "${resolved_base}" "${tip}")"; then
        return 1
    fi
    [ "${aggregate_patch}" = "${expected_patch}" ] || fail "${description} aggregate diff is not the expected patch."
}

verify_revert_ref() {
    local ref="$1"
    local base="$2"
    local message
    local expected

    if ! expected="$(expected_revert_patch_id)"; then
        return 1
    fi
    verify_single_commit_patch "${ref}" "${base}" "${expected}" "Revert branch ${REVERT_BRANCH}"
    if ! message="$(git show -s --format=%B "${ref}" 2>&1)"; then
        fail "Could not inspect revert commit message: ${message}"
    fi
    if ! rg -Fq "This reverts commit ${SHA}." <<<"${message}"; then
        fail "Revert branch ${REVERT_BRANCH} does not identify candidate ${SHA}."
    fi
}

run_revert_with_recovery() {
    if git revert --no-edit "${SHA}"; then
        return 0
    fi
    cat >&2 <<'RECOVERY'
[FAIL] Candidate revert has conflicts. The repository is left in the normal revert conflict state.

git status

# Resolve the files, then:
git add <resolved-files>
git revert --continue

# Or abandon:
git revert --abort
RECOVERY
    exit 1
}

load_pr_fields() {
    if ! PR_NUMBER="$(jq -er '.number' <<<"${PR_JSON}" 2>&1)"; then fail "PR JSON has no number: ${PR_NUMBER}"; fi
    if ! PR_STATE="$(jq -er '.state' <<<"${PR_JSON}" 2>&1)"; then fail "PR JSON has no state: ${PR_STATE}"; fi
    if ! PR_MERGE_COMMIT="$(jq -r '.mergeCommit.oid // empty' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR merge commit: ${PR_MERGE_COMMIT}"; fi
}

find_revert_pr() {
    local list_json
    local matches_json
    local count

    PR_NUMBER=""
    PR_STATE=""
    PR_JSON=""
    PR_MERGE_COMMIT=""
    if ! list_json="$(gh pr list \
        --repo "${REPOSITORY}" \
        --head "${REVERT_BRANCH}" \
        --base main \
        --state all \
        --limit 100 \
        --json "${PR_FIELDS}" 2>&1)"; then
        fail "Could not list revert PRs: ${list_json}"
    fi
    if ! matches_json="$(jq -ce '[.[] | select(.state == "OPEN" or .state == "MERGED")]' <<<"${list_json}" 2>&1)"; then
        fail "Could not inspect revert PR list JSON: ${matches_json}"
    fi
    if ! count="$(jq -er 'length' <<<"${matches_json}" 2>&1)"; then
        fail "Could not count matching revert PRs: ${count}"
    fi
    [ "${count}" -le 1 ] || fail "Multiple open or merged PRs use revert branch ${REVERT_BRANCH}; manual review is required."
    if [ "${count}" -eq 0 ]; then
        return 0
    fi
    if ! PR_JSON="$(jq -ce '.[0]' <<<"${matches_json}" 2>&1)"; then
        fail "Could not read matching revert PR: ${PR_JSON}"
    fi
    load_pr_fields
    validate_pr_identity "${PR_STATE}"
}

load_pr_by_number() {
    local number="$1"
    if ! PR_JSON="$(gh pr view "${number}" \
        --repo "${REPOSITORY}" \
        --json "${PR_FIELDS}" 2>&1)"; then
        fail "Could not load revert PR #${number}: ${PR_JSON}"
    fi
    load_pr_fields
    [ "${PR_NUMBER}" = "${number}" ] || fail "GitHub returned PR #${PR_NUMBER} when PR #${number} was requested."
}

validate_pr_identity() {
    local expected_state="$1"
    local is_draft
    local base
    local head
    local head_owner
    local cross_repository

    [[ "${PR_NUMBER}" =~ ^[0-9]+$ ]] || fail "Revert PR number is invalid: ${PR_NUMBER}"
    [ "${PR_STATE}" = "${expected_state}" ] || fail "Revert PR #${PR_NUMBER} is ${PR_STATE}; expected ${expected_state}."
    if ! is_draft="$(jq -r '.isDraft' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR draft state: ${is_draft}"; fi
    if ! base="$(jq -er '.baseRefName' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR base: ${base}"; fi
    if ! head="$(jq -er '.headRefName' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR head: ${head}"; fi
    if ! head_owner="$(jq -er '.headRepositoryOwner.login' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR head owner: ${head_owner}"; fi
    if ! cross_repository="$(jq -r '.isCrossRepository' <<<"${PR_JSON}" 2>&1)"; then fail "Could not read PR repository relationship: ${cross_repository}"; fi
    [[ "${is_draft}" =~ ^(true|false)$ ]] || fail "Revert PR #${PR_NUMBER} returned invalid draft state ${is_draft}."
    [[ "${cross_repository}" =~ ^(true|false)$ ]] || fail "Revert PR #${PR_NUMBER} returned invalid cross-repository state ${cross_repository}."
    [ "${is_draft}" = "false" ] || fail "Revert PR #${PR_NUMBER} is a draft."
    [ "${base}" = "main" ] || fail "Revert PR #${PR_NUMBER} targets ${base}, not main."
    [ "${head}" = "${REVERT_BRANCH}" ] || fail "Revert PR #${PR_NUMBER} head ${head} is not ${REVERT_BRANCH}."
    [ "${cross_repository}" = "false" ] || fail "Revert PR #${PR_NUMBER} is cross-repository."
    [ "${head_owner,,}" = "${OWNER,,}" ] || fail "Revert PR #${PR_NUMBER} head owner ${head_owner} is not ${OWNER}."
}

validate_pr_head() {
    local expected_state="$1"
    local expected_head="$2"
    local expected_base="${3:-}"
    local actual_head
    local actual_base

    validate_pr_identity "${expected_state}"
    if ! actual_head="$(jq -er '.headRefOid' <<<"${PR_JSON}" 2>&1)"; then
        fail "Could not read PR head OID: ${actual_head}"
    fi
    [ "${actual_head}" = "${expected_head}" ] || fail "Revert PR #${PR_NUMBER} head changed: expected ${expected_head}, found ${actual_head}."
    if [ -n "${expected_base}" ]; then
        if ! actual_base="$(jq -er '.baseRefOid' <<<"${PR_JSON}" 2>&1)"; then
            fail "Could not read PR base OID: ${actual_base}"
        fi
        [ "${actual_base}" = "${expected_base}" ] || fail "Revert PR #${PR_NUMBER} base changed: expected ${expected_base}, found ${actual_base}."
    fi
}

ensure_new_revert_target_is_head() {
    local origin_head
    if ! origin_head="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve origin/main: ${origin_head}"; fi
    if [ "${SHA}" = "${origin_head}" ]; then return 0; fi
    echo "[FAIL] Failed QA SHA must equal current origin/main HEAD for a new rollback." >&2
    echo "       provided SHA:     ${SHA}" >&2
    echo "       origin/main HEAD: ${origin_head}" >&2
    [ "${ALLOW_NON_HEAD}" -eq 1 ] || fail "Use --allow-non-head only when intentionally reverting an older main commit."
    confirm "Revert non-head main commit ${SHA:0:12}?"
}

prepare_revert_branch() {
    local local_exists=0
    local remote_exists=0
    local local_sha=""
    local remote_sha=""
    local head

    if local_branch_exists "${REVERT_BRANCH}"; then local_exists=1; fi
    if remote_branch_exists "${REVERT_BRANCH}"; then remote_exists=1; fi
    if [ "${local_exists}" -eq 1 ]; then
        if ! local_sha="$(git rev-parse "${REVERT_BRANCH}" 2>&1)"; then fail "Could not resolve local revert branch: ${local_sha}"; fi
    fi
    if [ "${remote_exists}" -eq 1 ]; then
        if ! remote_sha="$(git rev-parse "origin/${REVERT_BRANCH}" 2>&1)"; then fail "Could not resolve origin revert branch: ${remote_sha}"; fi
    fi

    if [ "${local_exists}" -eq 1 ] && [ "${remote_exists}" -eq 1 ] && [ "${local_sha}" != "${remote_sha}" ]; then
        if [ "${local_sha}" = "${REVERT_BASE_SHA}" ]; then
            verify_revert_ref "origin/${REVERT_BRANCH}" "${REVERT_BASE_SHA}"
            git switch "${REVERT_BRANCH}"
            git merge --ff-only "origin/${REVERT_BRANCH}"
            local_sha="${remote_sha}"
        elif [ "${remote_sha}" = "${REVERT_BASE_SHA}" ]; then
            verify_revert_ref "${REVERT_BRANCH}" "${REVERT_BASE_SHA}"
        else
            fail "Local and origin revert branches differ and neither is the unchanged expected base."
        fi
    fi

    if [ "${local_exists}" -eq 1 ]; then
        echo "[OK] Reusing local revert branch ${REVERT_BRANCH}."
        git switch "${REVERT_BRANCH}"
    elif [ "${remote_exists}" -eq 1 ]; then
        echo "[OK] Reusing origin revert branch ${REVERT_BRANCH}."
        git switch -c "${REVERT_BRANCH}" --track "origin/${REVERT_BRANCH}"
    else
        echo "[OK] Creating revert branch ${REVERT_BRANCH} at ${REVERT_BASE_SHA}."
        git switch -c "${REVERT_BRANCH}" "${REVERT_BASE_SHA}"
    fi

    if ! head="$(git rev-parse HEAD 2>&1)"; then fail "Could not resolve revert branch HEAD: ${head}"; fi
    if [ "${head}" = "${REVERT_BASE_SHA}" ]; then
        echo "[OK] Revert branch is unchanged at its expected base; creating the revert commit."
        run_revert_with_recovery
        if ! head="$(git rev-parse HEAD 2>&1)"; then fail "Could not resolve new revert commit: ${head}"; fi
    fi
    verify_revert_ref HEAD "${REVERT_BASE_SHA}"
    EXPECTED_REVERT_HEAD="${head}"

    if remote_branch_exists "${REVERT_BRANCH}"; then
        if ! remote_sha="$(git rev-parse "origin/${REVERT_BRANCH}" 2>&1)"; then fail "Could not resolve origin revert branch: ${remote_sha}"; fi
        if [ "${remote_sha}" = "${EXPECTED_REVERT_HEAD}" ]; then
            echo "[OK] Origin revert branch is already current."
        elif [ "${remote_sha}" = "${REVERT_BASE_SHA}" ]; then
            echo "[OK] Fast-forwarding origin revert branch ${REVERT_BRANCH}."
            git push origin "${REVERT_BRANCH}"
        else
            fail "Origin revert branch changed unexpectedly; refusing to overwrite it."
        fi
    else
        echo "[OK] Pushing revert branch ${REVERT_BRANCH}."
        git push -u origin "${REVERT_BRANCH}"
    fi
}

print_candidate_refs() {
    local owner_lower="${OWNER,,}"
    echo "[OK] Candidate images are preserved:"
    echo "     ghcr.io/${owner_lower}/fastsell:api-sha-${SHA}"
    echo "     ghcr.io/${owner_lower}/fastsell:web-sha-${SHA}"
    echo "     ghcr.io/${owner_lower}/fastsell:system-agent-sha-${SHA}"
}

create_revert_pr() {
    local output
    local title="Revert failed QA candidate ${SHA:0:12}"
    local body="Reverts failed staging QA candidate ${SHA}.

Candidate images are preserved."

    echo "[OK] Creating revert PR."
    if ! output="$(gh pr create \
        --repo "${REPOSITORY}" \
        --title "${title}" \
        --body "${body}" \
        --base main \
        --head "${OWNER}:${REVERT_BRANCH}" 2>&1)"; then
        fail "Could not create revert PR: ${output}"
    fi
    find_revert_pr
    [ -n "${PR_NUMBER}" ] || fail "Revert PR was created but could not be found."
    validate_pr_head OPEN "${EXPECTED_REVERT_HEAD}" "${REVERT_BASE_SHA}"
    echo "[OK] Created revert PR #${PR_NUMBER}."
}

merge_revert_pr() {
    local output
    local number="${PR_NUMBER}"
    local current_base

    load_pr_by_number "${number}"
    validate_pr_head OPEN "${EXPECTED_REVERT_HEAD}" "${REVERT_BASE_SHA}"
    release_wait_for_required_checks "${REPOSITORY}" main "${number}"

    # Close the check-to-merge race by reloading every identity field and then
    # binding GitHub's merge operation to the same verified head commit. Also
    # refuse a changed base because gh has no equivalent base-OID merge guard.
    git fetch origin main
    if ! current_base="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve pre-merge origin/main: ${current_base}"; fi
    [ "${current_base}" = "${REVERT_BASE_SHA}" ] || fail "origin/main changed while revert PR #${number} checks ran; rerun requires manual review."
    load_pr_by_number "${number}"
    validate_pr_head OPEN "${EXPECTED_REVERT_HEAD}" "${REVERT_BASE_SHA}"
    echo "[OK] Squash-merging revert PR #${number} at head ${EXPECTED_REVERT_HEAD}."
    if ! output="$(gh pr merge "${number}" \
        --repo "${REPOSITORY}" \
        --squash \
        --match-head-commit "${EXPECTED_REVERT_HEAD}" 2>&1)"; then
        fail "Could not merge revert PR #${number}: ${output}"
    fi
    load_pr_by_number "${number}"
    validate_pr_head MERGED "${EXPECTED_REVERT_HEAD}"
    [ -n "${PR_MERGE_COMMIT}" ] || fail "Merged revert PR #${number} has no merge commit."
}

synchronize_main() {
    local local_main
    local remote_main
    echo "[OK] Synchronizing local main."
    git fetch origin main
    git switch main
    git pull --ff-only origin main
    if ! local_main="$(git rev-parse main 2>&1)"; then fail "Could not resolve local main: ${local_main}"; fi
    if ! remote_main="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve origin/main: ${remote_main}"; fi
    [ "${local_main}" = "${remote_main}" ] || fail "Local main does not equal origin/main after fast-forward pull."
}

verify_effective_revert_on_main() {
    local number="${PR_NUMBER}"
    local current_main
    local expected_patch

    load_pr_by_number "${number}"
    validate_pr_head MERGED "${EXPECTED_REVERT_HEAD}"
    [ -n "${PR_MERGE_COMMIT}" ] || fail "Revert PR #${number} has no merge commit."
    git cat-file -e "${PR_MERGE_COMMIT}^{commit}" 2>/dev/null || fail "Merged revert commit is unavailable locally: ${PR_MERGE_COMMIT}"
    if ! current_main="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve synchronized origin/main: ${current_main}"; fi
    [ "${current_main}" = "${PR_MERGE_COMMIT}" ] || fail "origin/main advanced after revert PR #${number}; effective rollback state is ambiguous and requires manual review."
    if ! expected_patch="$(expected_revert_patch_id)"; then return 1; fi
    verify_single_commit_patch "${PR_MERGE_COMMIT}" "${REVERT_BASE_SHA}" "${expected_patch}" "Merged revert PR #${number}"
    echo "[OK] Synchronized main is exactly the verified rollback merge with no later candidate reapplication."
}

verify_retry_ref() {
    local ref="$1"
    local expected
    if ! expected="$(candidate_patch_id)"; then return 1; fi
    verify_single_commit_patch "${ref}" origin/main "${expected}" "Retry branch ${RETRY_BRANCH}"
}

prepare_retry_branch() {
    local local_exists=0
    local remote_exists=0
    local local_sha=""
    local remote_sha=""
    local main_sha
    local head

    validate_retry_branch "${RETRY_BRANCH}"
    if ! main_sha="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve retry base origin/main: ${main_sha}"; fi
    if local_branch_exists "${RETRY_BRANCH}"; then local_exists=1; fi
    if remote_branch_exists "${RETRY_BRANCH}"; then remote_exists=1; fi
    if [ "${local_exists}" -eq 1 ]; then
        if ! local_sha="$(git rev-parse "${RETRY_BRANCH}" 2>&1)"; then fail "Could not resolve local retry branch: ${local_sha}"; fi
    fi
    if [ "${remote_exists}" -eq 1 ]; then
        if ! remote_sha="$(git rev-parse "origin/${RETRY_BRANCH}" 2>&1)"; then fail "Could not resolve origin retry branch: ${remote_sha}"; fi
    fi

    if [ "${local_exists}" -eq 1 ] && [ "${remote_exists}" -eq 1 ] && [ "${local_sha}" != "${remote_sha}" ]; then
        if [ "${local_sha}" = "${main_sha}" ]; then
            verify_retry_ref "origin/${RETRY_BRANCH}"
            git switch "${RETRY_BRANCH}"
            git merge --ff-only "origin/${RETRY_BRANCH}"
            local_sha="${remote_sha}"
        elif [ "${remote_sha}" = "${main_sha}" ]; then
            verify_retry_ref "${RETRY_BRANCH}"
        else
            fail "Local and origin retry branches differ and neither is the unchanged synchronized main base."
        fi
    fi

    if [ "${local_exists}" -eq 1 ]; then
        echo "[OK] Reusing local retry branch ${RETRY_BRANCH}."
        git switch "${RETRY_BRANCH}"
    elif [ "${remote_exists}" -eq 1 ]; then
        echo "[OK] Reusing origin retry branch ${RETRY_BRANCH}."
        git switch -c "${RETRY_BRANCH}" --track "origin/${RETRY_BRANCH}"
    else
        echo "[OK] Creating retry branch ${RETRY_BRANCH} from synchronized main."
        git switch -c "${RETRY_BRANCH}" "${main_sha}"
    fi

    if ! head="$(git rev-parse HEAD 2>&1)"; then fail "Could not resolve retry branch HEAD: ${head}"; fi
    if [ "${head}" = "${main_sha}" ]; then
        echo "[OK] Reapplying failed candidate ${SHA}."
        if ! git cherry-pick -x "${SHA}"; then
            cat >&2 <<'RECOVERY'
[FAIL] Candidate cherry-pick has conflicts. The repository is left in the normal conflict state.

git status

# Resolve the files, then:
git add <resolved-files>
git cherry-pick --continue

# Or abandon:
git cherry-pick --abort
RECOVERY
            exit 1
        fi
    else
        verify_retry_ref HEAD
        echo "[OK] Failed candidate is already reapplied as the only retry commit on ${RETRY_BRANCH}."
    fi
    verify_retry_ref HEAD
}

direct_tip_is_completed_revert() {
    local tip="$1"
    local parent_line
    local -a parents
    local message
    local expected
    local actual

    if ! parent_line="$(git rev-list --parents -n 1 "${tip}" 2>&1)"; then fail "Could not inspect direct revert tip: ${parent_line}"; fi
    read -r -a parents <<<"${parent_line}"
    [ "${#parents[@]}" -eq 2 ] || return 1
    if ! message="$(git show -s --format=%B "${tip}" 2>&1)"; then fail "Could not inspect direct revert message: ${message}"; fi
    if ! rg -Fq "This reverts commit ${SHA}." <<<"${message}"; then return 1; fi
    if ! expected="$(expected_revert_patch_id)"; then return 1; fi
    if ! actual="$(patch_id_for_commit "${tip}")"; then return 1; fi
    [ "${actual}" = "${expected}" ] || return 1
    if ! actual="$(patch_id_for_diff "${parents[1]}" "${tip}")"; then return 1; fi
    [ "${actual}" = "${expected}" ] || return 1
    REVERT_BASE_SHA="${parents[1]}"
    return 0
}

run_direct_mode() {
    local head
    local origin_head

    git switch main
    git pull --ff-only origin main
    if ! head="$(git rev-parse HEAD 2>&1)"; then fail "Could not resolve local main: ${head}"; fi
    if ! origin_head="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve origin/main: ${origin_head}"; fi

    if [ "${head}" != "${SHA}" ] && direct_tip_is_completed_revert "${head}"; then
        if [ "${origin_head}" = "${head}" ]; then
            echo "[OK] Emergency direct revert was already completed and pushed."
        elif [ "${origin_head}" = "${REVERT_BASE_SHA}" ]; then
            echo "[OK] Resuming interrupted direct mode by pushing the already-created verified revert."
            git push origin main
        else
            fail "Direct revert tip is valid, but origin/main is neither its parent nor the revert commit; manual review is required."
        fi
        echo "[OK] Current branch: main"
        echo "[OK] No retry branch was created."
        return 0
    fi

    if [ "${head}" != "${SHA}" ]; then
        [ "${ALLOW_NON_HEAD}" -eq 1 ] || fail "Direct mode requires the candidate to equal current main HEAD unless --allow-non-head is explicit."
    fi
    REVERT_BASE_SHA="${head}"
    confirm "Directly revert ${SHA:0:12} on main and push to origin/main?"
    run_revert_with_recovery
    if ! head="$(git rev-parse HEAD 2>&1)"; then fail "Could not resolve direct revert commit: ${head}"; fi
    direct_tip_is_completed_revert "${head}" || fail "Direct revert commit did not match the expected rollback."
    git push origin main
    git fetch origin main
    if ! origin_head="$(git rev-parse origin/main 2>&1)"; then fail "Could not verify pushed origin/main: ${origin_head}"; fi
    [ "${origin_head}" = "${head}" ] || fail "origin/main does not equal the verified direct revert after push."
    echo "[OK] Emergency direct revert pushed to origin/main."
    echo "[OK] Current branch: main"
    echo "[OK] No retry branch was created."
}

print_final_status() {
    local current_branch
    if ! current_branch="$(git branch --show-current 2>&1)"; then fail "Could not read current branch: ${current_branch}"; fi
    echo
    echo "[OK] Failed-QA workflow complete."
    echo "     Current branch: ${current_branch}"
    echo "     Current main: synchronized with origin/main"
    echo "     Failed candidate: reverted on main"
    if [ "${ROLLBACK_ONLY}" -eq 1 ]; then
        echo "     Retry preparation: skipped (--rollback-only)"
        echo "     Next: inspect synchronized main before starting new work"
    else
        echo "     Failed candidate changes: reapplied on retry branch"
        echo "     Next: implement the targeted correction, then run do_pr.sh"
    fi
}

main() {
    local root
    local current_main

    parse_args "$@"
    require_cmd git
    require_cmd gh
    require_cmd jq
    require_cmd rg
    require_cmd awk

    validate_sha "${SHA}"
    if ! root="$(repo_root)"; then return 1; fi
    cd "${root}"
    report_in_progress_revert
    ensure_clean_worktree
    check_auth_and_repository
    fetch_origin
    resolve_candidate
    print_candidate_refs

    if [ "${DIRECT}" -eq 1 ]; then
        run_direct_mode
        exit 0
    fi

    REVERT_BRANCH="revert/failed-candidate-${SHA:0:12}"
    find_revert_pr
    if [ "${PR_STATE}" = "MERGED" ]; then
        remote_branch_exists "${REVERT_BRANCH}" || fail "Merged revert PR #${PR_NUMBER} has no origin/${REVERT_BRANCH} ref to verify."
        if ! EXPECTED_REVERT_HEAD="$(git rev-parse "origin/${REVERT_BRANCH}" 2>&1)"; then fail "Could not resolve merged revert branch: ${EXPECTED_REVERT_HEAD}"; fi
        validate_pr_head MERGED "${EXPECTED_REVERT_HEAD}"
        [ -n "${PR_MERGE_COMMIT}" ] || fail "Merged revert PR #${PR_NUMBER} has no merge commit."
        if ! current_main="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve origin/main: ${current_main}"; fi
        [ "${current_main}" = "${PR_MERGE_COMMIT}" ] || fail "origin/main advanced after the rollback merge; delayed resume requires manual review."
        if ! REVERT_BASE_SHA="$(git rev-parse "${PR_MERGE_COMMIT}^" 2>&1)"; then fail "Could not resolve rollback merge base: ${REVERT_BASE_SHA}"; fi
        prepare_revert_branch
        echo "[OK] Reusing already merged and exactly verified revert PR #${PR_NUMBER}."
    else
        ensure_new_revert_target_is_head
        if ! REVERT_BASE_SHA="$(git rev-parse origin/main 2>&1)"; then fail "Could not resolve rollback base: ${REVERT_BASE_SHA}"; fi
        prepare_revert_branch
        if [ -n "${PR_NUMBER}" ]; then
            load_pr_by_number "${PR_NUMBER}"
            validate_pr_head OPEN "${EXPECTED_REVERT_HEAD}" "${REVERT_BASE_SHA}"
            echo "[OK] Reusing exact revert PR #${PR_NUMBER}."
        else
            create_revert_pr
        fi
        merge_revert_pr
    fi

    synchronize_main
    verify_effective_revert_on_main
    if [ "${ROLLBACK_ONLY}" -eq 1 ]; then
        ensure_clean_worktree
        print_final_status
        exit 0
    fi

    prepare_retry_branch
    ensure_clean_worktree
    print_final_status
}

main "$@"
