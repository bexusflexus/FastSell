#!/usr/bin/env bash
set -euo pipefail

# Verify exact check names before applying protection:
#   gh pr checks <pr-number> --json name,state,workflow --jq '.[] | [.workflow, .name, .state] | @tsv'
#
# Branch protection required status check contexts use the check run name values.
# Edit this list if GitHub reports different names for a current pull request.
REQUIRED_CHECK_CONTEXTS=(
    "Build setup bundle"
    "Test"
    "Docker build (api, ./api, ./api/Dockerfile)"
    "Docker build (system-agent, ./api, ./api/Dockerfile.system-agent)"
    "Docker build (web, ., ./docker/web.Dockerfile)"
)

BRANCH="main"
YES=0
DRY_RUN=0

usage() {
    cat <<'USAGE'
Usage:
  configure_main_branch_protection.sh [branch] [--yes] [--dry-run]

Configures light classic branch protection for main by default.
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

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --yes|-y)
                YES=1
                shift
                ;;
            --dry-run)
                DRY_RUN=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            --*)
                echo "[FAIL] Unknown option: $1" >&2
                usage >&2
                exit 1
                ;;
            *)
                if [ "${BRANCH}" != "main" ]; then
                    echo "[FAIL] Branch was already set to ${BRANCH}; unexpected argument: $1" >&2
                    usage >&2
                    exit 1
                fi
                BRANCH="$1"
                shift
                ;;
        esac
    done
}

validate_branch() {
    case "${BRANCH}" in
        ""|*/*|*\\*|*[!A-Za-z0-9._-]*)
            echo "[FAIL] Branch must be a simple branch name without slashes, for example main." >&2
            exit 1
            ;;
    esac
}

json_string() {
    local value="$1"

    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    printf '"%s"' "${value}"
}

json_context_array() {
    local first=1
    local context

    printf '['
    for context in "${REQUIRED_CHECK_CONTEXTS[@]}"; do
        if [ "${first}" -eq 0 ]; then
            printf ','
        fi
        json_string "${context}"
        first=0
    done
    printf ']'
}

payload_json() {
    cat <<JSON
{
  "required_status_checks": {
    "strict": false,
    "contexts": $(json_context_array)
  },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "required_linear_history": false,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": false,
  "lock_branch": false
}
JSON
}

confirm() {
    local answer

    if [ "${YES}" -eq 1 ]; then
        return
    fi

    read -r -p "Apply branch protection with these settings? [y/N] " answer
    case "${answer}" in
        y|Y|yes|YES) ;;
        *) echo "[FAIL] Cancelled."; exit 1 ;;
    esac
}

print_plan() {
    local owner="$1"
    local repo="$2"
    local context

    echo "[OK] Repository: ${owner}/${repo}"
    echo "[OK] Target branch: ${BRANCH}"
    echo "[OK] Required status check contexts:"
    for context in "${REQUIRED_CHECK_CONTEXTS[@]}"; do
        echo "     - ${context}"
    done
    echo "[OK] Reviews required: no"
    echo "[OK] Enforce admins: no"
    echo "[OK] Force pushes allowed: no"
    echo "[OK] Branch deletion allowed: no"
    echo "[OK] Push actor restrictions: none"
}

apply_protection() {
    local owner="$1"
    local repo="$2"
    local endpoint="/repos/${owner}/${repo}/branches/${BRANCH}/protection"

    payload_json | gh api \
        -X PUT \
        -H "Accept: application/vnd.github+json" \
        "${endpoint}" \
        --input -
}

read_back_protection() {
    local owner="$1"
    local repo="$2"
    local endpoint="/repos/${owner}/${repo}/branches/${BRANCH}/protection"

    gh api \
        -H "Accept: application/vnd.github+json" \
        "${endpoint}" \
        --jq '{required_status_checks, enforce_admins, required_pull_request_reviews, restrictions, allow_force_pushes, allow_deletions}'
}

main() {
    parse_args "$@"
    require_cmd git
    require_cmd gh
    validate_branch

    cd "$(repo_root)"

    local owner
    local repo
    owner="$(gh repo view --json owner --jq .owner.login)"
    repo="$(gh repo view --json name --jq .name)"

    print_plan "${owner}" "${repo}"

    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[OK] Dry run only. Branch protection payload:"
        payload_json
        exit 0
    fi

    confirm
    apply_protection "${owner}" "${repo}" >/dev/null

    echo "[OK] Branch protection applied. Current protection summary:"
    read_back_protection "${owner}" "${repo}"
}

main "$@"
