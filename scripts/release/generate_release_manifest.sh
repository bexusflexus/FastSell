#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  generate_release_manifest.sh --kind candidate --repo owner/repo --source-sha <sha> \
    --bundle-name <name> --api-ref <ref> --api-digest <sha256:digest> \
    --web-ref <ref> --web-digest <sha256:digest> \
    --system-agent-ref <ref> --system-agent-digest <sha256:digest> \
    --output <path>

  generate_release_manifest.sh --kind production --repo owner/repo --source-sha <sha> \
    --version vX.Y.Z --promoted-from-sha <sha> --bundle-name <name> \
    --api-ref <prod-ref> --api-candidate-ref <candidate-ref> --api-digest <sha256:digest> \
    --web-ref <prod-ref> --web-candidate-ref <candidate-ref> --web-digest <sha256:digest> \
    --system-agent-ref <prod-ref> --system-agent-candidate-ref <candidate-ref> \
    --system-agent-digest <sha256:digest> --output <path>
USAGE
}

KIND=""
REPO=""
SOURCE_SHA=""
VERSION=""
PROMOTED_FROM_SHA=""
BUNDLE_NAME=""
API_REF=""
API_CANDIDATE_REF=""
API_DIGEST=""
WEB_REF=""
WEB_CANDIDATE_REF=""
WEB_DIGEST=""
SYSTEM_AGENT_REF=""
SYSTEM_AGENT_CANDIDATE_REF=""
SYSTEM_AGENT_DIGEST=""
OUTPUT=""

while [ "$#" -gt 0 ]; do
    case "$1" in
        --kind) KIND="$2"; shift 2 ;;
        --repo) REPO="$2"; shift 2 ;;
        --source-sha) SOURCE_SHA="$2"; shift 2 ;;
        --version) VERSION="$2"; shift 2 ;;
        --promoted-from-sha) PROMOTED_FROM_SHA="$2"; shift 2 ;;
        --bundle-name) BUNDLE_NAME="$2"; shift 2 ;;
        --api-ref) API_REF="$2"; shift 2 ;;
        --api-candidate-ref) API_CANDIDATE_REF="$2"; shift 2 ;;
        --api-digest) API_DIGEST="$2"; shift 2 ;;
        --web-ref) WEB_REF="$2"; shift 2 ;;
        --web-candidate-ref) WEB_CANDIDATE_REF="$2"; shift 2 ;;
        --web-digest) WEB_DIGEST="$2"; shift 2 ;;
        --system-agent-ref) SYSTEM_AGENT_REF="$2"; shift 2 ;;
        --system-agent-candidate-ref) SYSTEM_AGENT_CANDIDATE_REF="$2"; shift 2 ;;
        --system-agent-digest) SYSTEM_AGENT_DIGEST="$2"; shift 2 ;;
        --output) OUTPUT="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "[FAIL] Unknown argument: $1" >&2; usage >&2; exit 1 ;;
    esac
done

require_value() {
    local name="$1"
    local value="$2"

    if [ -z "${value}" ]; then
        echo "[FAIL] ${name} is required." >&2
        exit 1
    fi
}

validate_digest() {
    local name="$1"
    local value="$2"

    if [[ ! "${value}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
        echo "[FAIL] ${name} must be a sha256 digest." >&2
        exit 1
    fi
}

validate_git_sha() {
    local name="$1"
    local value="$2"

    if [[ ! "${value}" =~ ^[0-9a-f]{40}$ ]]; then
        echo "[FAIL] ${name} must be a full 40-character lowercase git SHA." >&2
        exit 1
    fi
}

migration_max() {
    local path
    local max=""

    shopt -s nullglob
    for path in db/migrations/*.up.sql; do
        path="$(basename -- "${path}")"
        if [ -z "${max}" ] || [[ "${path}" > "${max}" ]]; then
            max="${path}"
        fi
    done
    shopt -u nullglob

    printf '%s' "${max}"
}

json_string() {
    local value="$1"

    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    printf '"%s"' "${value}"
}

write_candidate_manifest() {
    local generated_at
    local short_sha
    local migration

    generated_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    short_sha="${SOURCE_SHA:0:12}"
    migration="$(migration_max)"

    {
        printf '{\n'
        printf '  "schema_version": 1,\n'
        printf '  "kind": "candidate",\n'
        printf '  "repo": %s,\n' "$(json_string "${REPO}")"
        printf '  "source_sha": %s,\n' "$(json_string "${SOURCE_SHA}")"
        printf '  "source_short_sha": %s,\n' "$(json_string "${short_sha}")"
        printf '  "generated_at": %s,\n' "$(json_string "${generated_at}")"
        printf '  "setup_bundle_artifact": %s,\n' "$(json_string "${BUNDLE_NAME}")"
        printf '  "migration_max": %s,\n' "$(json_string "${migration}")"
        printf '  "images": {\n'
        printf '    "api": { "ref": %s, "digest": %s },\n' "$(json_string "${API_REF}")" "$(json_string "${API_DIGEST}")"
        printf '    "web": { "ref": %s, "digest": %s },\n' "$(json_string "${WEB_REF}")" "$(json_string "${WEB_DIGEST}")"
        printf '    "system_agent": { "ref": %s, "digest": %s }\n' "$(json_string "${SYSTEM_AGENT_REF}")" "$(json_string "${SYSTEM_AGENT_DIGEST}")"
        printf '  }\n'
        printf '}\n'
    } > "${OUTPUT}"
}

write_production_manifest() {
    local generated_at
    local short_sha
    local migration

    generated_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    short_sha="${SOURCE_SHA:0:12}"
    migration="$(migration_max)"

    {
        printf '{\n'
        printf '  "schema_version": 1,\n'
        printf '  "kind": "production",\n'
        printf '  "repo": %s,\n' "$(json_string "${REPO}")"
        printf '  "version": %s,\n' "$(json_string "${VERSION}")"
        printf '  "source_sha": %s,\n' "$(json_string "${SOURCE_SHA}")"
        printf '  "source_short_sha": %s,\n' "$(json_string "${short_sha}")"
        printf '  "promoted_from_candidate_sha": %s,\n' "$(json_string "${PROMOTED_FROM_SHA}")"
        printf '  "generated_at": %s,\n' "$(json_string "${generated_at}")"
        printf '  "setup_bundle_artifact": %s,\n' "$(json_string "${BUNDLE_NAME}")"
        printf '  "migration_max": %s,\n' "$(json_string "${migration}")"
        printf '  "images": {\n'
        printf '    "api": { "production_ref": %s, "candidate_ref": %s, "digest": %s },\n' "$(json_string "${API_REF}")" "$(json_string "${API_CANDIDATE_REF}")" "$(json_string "${API_DIGEST}")"
        printf '    "web": { "production_ref": %s, "candidate_ref": %s, "digest": %s },\n' "$(json_string "${WEB_REF}")" "$(json_string "${WEB_CANDIDATE_REF}")" "$(json_string "${WEB_DIGEST}")"
        printf '    "system_agent": { "production_ref": %s, "candidate_ref": %s, "digest": %s }\n' "$(json_string "${SYSTEM_AGENT_REF}")" "$(json_string "${SYSTEM_AGENT_CANDIDATE_REF}")" "$(json_string "${SYSTEM_AGENT_DIGEST}")"
        printf '  }\n'
        printf '}\n'
    } > "${OUTPUT}"
}

main() {
    require_value "--kind" "${KIND}"
    require_value "--repo" "${REPO}"
    require_value "--source-sha" "${SOURCE_SHA}"
    require_value "--bundle-name" "${BUNDLE_NAME}"
    require_value "--api-ref" "${API_REF}"
    require_value "--api-digest" "${API_DIGEST}"
    require_value "--web-ref" "${WEB_REF}"
    require_value "--web-digest" "${WEB_DIGEST}"
    require_value "--system-agent-ref" "${SYSTEM_AGENT_REF}"
    require_value "--system-agent-digest" "${SYSTEM_AGENT_DIGEST}"
    require_value "--output" "${OUTPUT}"

    validate_digest "--api-digest" "${API_DIGEST}"
    validate_digest "--web-digest" "${WEB_DIGEST}"
    validate_digest "--system-agent-digest" "${SYSTEM_AGENT_DIGEST}"
    validate_git_sha "--source-sha" "${SOURCE_SHA}"

    mkdir -p "$(dirname -- "${OUTPUT}")"

    case "${KIND}" in
        candidate)
            write_candidate_manifest
            ;;
        production)
            require_value "--version" "${VERSION}"
            require_value "--promoted-from-sha" "${PROMOTED_FROM_SHA}"
            require_value "--api-candidate-ref" "${API_CANDIDATE_REF}"
            require_value "--web-candidate-ref" "${WEB_CANDIDATE_REF}"
            require_value "--system-agent-candidate-ref" "${SYSTEM_AGENT_CANDIDATE_REF}"
            validate_git_sha "--promoted-from-sha" "${PROMOTED_FROM_SHA}"
            if [[ ! "${VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
                echo "[FAIL] --version must match vX.Y.Z." >&2
                exit 1
            fi
            write_production_manifest
            ;;
        *)
            echo "[FAIL] --kind must be candidate or production." >&2
            exit 1
            ;;
    esac

    echo "[OK] Wrote ${OUTPUT}"
}

main "$@"
