#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  success_qa.sh <full-sha>

Prompts for a production version vX.Y.Z and triggers the Promote Release workflow.
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

validate_sha() {
    local sha="$1"
    if [[ ! "${sha}" =~ ^[0-9a-f]{40}$ ]]; then
        echo "[FAIL] Expected a full 40-character lowercase git SHA." >&2
        exit 1
    fi
}

validate_version() {
    local version="$1"
    if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "[FAIL] Version must match vX.Y.Z. vX.Y is not allowed." >&2
        exit 1
    fi
}

candidate_run_id() {
    local sha="$1"

    gh run list \
        --workflow publish-images.yml \
        --branch main \
        --limit 100 \
        --json databaseId,headSha,conclusion \
        --jq ".[] | select(.headSha == \"${sha}\" and .conclusion == \"success\") | .databaseId" \
        | awk 'NR == 1 { print }'
}

confirm_promotion() {
    local sha="$1"
    local version="$2"
    local answer

    echo "[OK] Promotion request:"
    echo "     source SHA: ${sha}"
    echo "     version:    ${version}"
    read -r -p "Trigger production promotion? [y/N] " answer
    case "${answer}" in
        y|Y|yes|YES) ;;
        *) echo "[FAIL] Cancelled."; exit 1 ;;
    esac
}

main() {
    if [ "$#" -ne 1 ]; then
        usage >&2
        exit 1
    fi

    require_cmd git
    require_cmd gh

    local sha="$1"
    validate_sha "${sha}"

    cd "$(repo_root)"

    local run_id
    run_id="$(candidate_run_id "${sha}")"
    if [ -z "${run_id}" ]; then
        echo "[FAIL] Could not find a successful Publish Images workflow run on main for ${sha}." >&2
        echo "       Wait for candidate image publishing and artifact generation to succeed, then retry." >&2
        exit 1
    fi
    echo "[OK] Found successful candidate Publish Images run: ${run_id}"

    local version
    read -r -p "Production version (vX.Y.Z): " version
    validate_version "${version}"
    confirm_promotion "${sha}" "${version}"

    echo "[OK] Triggering Promote Release for ${version} from ${sha}"
    gh workflow run promote-release.yml \
        -f "source_sha=${sha}" \
        -f "version=${version}"

    cat <<NEXT
[OK] Promotion workflow triggered.
[OK] Monitor it with:
     gh run list --workflow promote-release.yml --limit 5
     gh run watch

[OK] The workflow retags candidate digests as versioned production tags and does not update latest.
NEXT
}

main "$@"
