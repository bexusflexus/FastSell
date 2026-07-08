#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  bash scripts/setup/create-setup-bundle.sh v0.1.0
  bash scripts/setup/create-setup-bundle.sh candidate-<sha> \
    --api-image ghcr.io/bexusflexus/fastsell:api-sha-<sha> \
    --system-agent-image ghcr.io/bexusflexus/fastsell:system-agent-sha-<sha> \
    --web-image ghcr.io/bexusflexus/fastsell:web-sha-<sha>
USAGE
}

if [ "$#" -lt 1 ]; then
    usage
    exit 1
fi

VERSION="$1"
shift
case "${VERSION}" in
    ""|-[!-]*|*/*|*\\*)
        echo "[FAIL] Version must be a bundle identifier, for example v0.1.0 or candidate-<sha>." >&2
        exit 1
        ;;
    *[!A-Za-z0-9._-]*)
        echo "[FAIL] Version may only contain letters, numbers, dots, underscores, and dashes." >&2
        exit 1
        ;;
esac

API_IMAGE=""
SYSTEM_AGENT_IMAGE=""
WEB_IMAGE=""

while [ "$#" -gt 0 ]; do
    case "$1" in
        --api-image)
            [ "$#" -ge 2 ] || { echo "[FAIL] --api-image requires a value." >&2; exit 1; }
            API_IMAGE="$2"
            shift 2
            ;;
        --system-agent-image)
            [ "$#" -ge 2 ] || { echo "[FAIL] --system-agent-image requires a value." >&2; exit 1; }
            SYSTEM_AGENT_IMAGE="$2"
            shift 2
            ;;
        --web-image)
            [ "$#" -ge 2 ] || { echo "[FAIL] --web-image requires a value." >&2; exit 1; }
            WEB_IMAGE="$2"
            shift 2
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

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
DIST_DIR="${REPO_ROOT}/dist"
BUNDLE_NAME="fastsell-setup-${VERSION}"
TMP_ROOT="$(mktemp -d)"
BUNDLE_DIR="${TMP_ROOT}/${BUNDLE_NAME}"

cleanup() {
    rm -rf -- "${TMP_ROOT}"
}
trap cleanup EXIT

require_file() {
    local path="$1"
    if [ ! -f "${REPO_ROOT}/${path}" ]; then
        echo "[FAIL] Required file is missing: ${path}" >&2
        exit 1
    fi
}

require_dir() {
    local path="$1"
    if [ ! -d "${REPO_ROOT}/${path}" ]; then
        echo "[FAIL] Required directory is missing: ${path}" >&2
        exit 1
    fi
}

require_command() {
    local name="$1"
    if ! command -v "${name}" >/dev/null 2>&1; then
        echo "[FAIL] Required command is missing: ${name}" >&2
        exit 1
    fi
}

copy_file() {
    local path="$1"
    mkdir -p "${BUNDLE_DIR}/$(dirname -- "${path}")"
    cp -p "${REPO_ROOT}/${path}" "${BUNDLE_DIR}/${path}"
}

copy_dir() {
    local path="$1"
    mkdir -p "${BUNDLE_DIR}/$(dirname -- "${path}")"
    cp -a "${REPO_ROOT}/${path}" "${BUNDLE_DIR}/${path}"
}

validate_image_ref() {
    local name="$1"
    local value="$2"

    if [ -z "${value}" ]; then
        echo "[FAIL] ${name} is required." >&2
        exit 1
    fi

    case "${value}" in
        *[[:space:]]*)
            echo "[FAIL] ${name} must not contain whitespace: ${value}" >&2
            exit 1
            ;;
        ghcr.io/*)
            ;;
        *)
            echo "[FAIL] ${name} must be a GHCR image ref: ${value}" >&2
            exit 1
            ;;
    esac
}

prepare_image_refs() {
    if [[ "${VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        API_IMAGE="${API_IMAGE:-ghcr.io/bexusflexus/fastsell:api-${VERSION}}"
        SYSTEM_AGENT_IMAGE="${SYSTEM_AGENT_IMAGE:-ghcr.io/bexusflexus/fastsell:system-agent-${VERSION}}"
        WEB_IMAGE="${WEB_IMAGE:-ghcr.io/bexusflexus/fastsell:web-${VERSION}}"
    elif [ -z "${API_IMAGE}" ] || [ -z "${SYSTEM_AGENT_IMAGE}" ] || [ -z "${WEB_IMAGE}" ]; then
        echo "[FAIL] Non-production bundles must pass explicit --api-image, --system-agent-image, and --web-image refs." >&2
        exit 1
    fi

    validate_image_ref "FASTSELL_API_IMAGE" "${API_IMAGE}"
    validate_image_ref "FASTSELL_SYSTEM_AGENT_IMAGE" "${SYSTEM_AGENT_IMAGE}"
    validate_image_ref "FASTSELL_WEB_IMAGE" "${WEB_IMAGE}"
}

apply_image_refs() {
    echo "[OK] Using FastSell image refs:"
    echo "     FASTSELL_API_IMAGE=${API_IMAGE}"
    echo "     FASTSELL_SYSTEM_AGENT_IMAGE=${SYSTEM_AGENT_IMAGE}"
    echo "     FASTSELL_WEB_IMAGE=${WEB_IMAGE}"

    sed -i \
        -e "s#ghcr.io/bexusflexus/fastsell:api-latest#${API_IMAGE}#g" \
        -e "s#ghcr.io/bexusflexus/fastsell:system-agent-latest#${SYSTEM_AGENT_IMAGE}#g" \
        -e "s#ghcr.io/bexusflexus/fastsell:web-latest#${WEB_IMAGE}#g" \
        "${BUNDLE_DIR}/.env.example" \
        "${BUNDLE_DIR}/docker-compose.yml" \
        "${BUNDLE_DIR}/setup/linux/install.sh"
}

file_contains_literal() {
    local file="$1"
    local needle="$2"

    awk -v needle="${needle}" 'index($0, needle) { found = 1 } END { exit found ? 0 : 1 }' "${file}"
}

verify_image_ref_substitution() {
    local file
    local ref

    for file in \
        "${BUNDLE_DIR}/.env.example" \
        "${BUNDLE_DIR}/docker-compose.yml" \
        "${BUNDLE_DIR}/setup/linux/install.sh"; do
        for ref in "${API_IMAGE}" "${SYSTEM_AGENT_IMAGE}" "${WEB_IMAGE}"; do
            if ! file_contains_literal "${file}" "${ref}"; then
                echo "[FAIL] Expected image ref was not written to ${file}: ${ref}" >&2
                exit 1
            fi
        done
    done

    echo "[OK] Verified image refs in generated bundle files."
}

verify_dev_only_not_bundled() {
    if [ -e "${BUNDLE_DIR}/dev_only" ]; then
        echo "[FAIL] dev_only must not be included in setup bundles." >&2
        exit 1
    fi
}

write_archives() {
    mkdir -p "${DIST_DIR}"
    rm -f \
        "${DIST_DIR}/${BUNDLE_NAME}.tar.gz" \
        "${DIST_DIR}/${BUNDLE_NAME}.zip"

    tar -C "${TMP_ROOT}" -czf "${DIST_DIR}/${BUNDLE_NAME}.tar.gz" "${BUNDLE_NAME}"
    (
        cd "${TMP_ROOT}"
        zip -qr "${DIST_DIR}/${BUNDLE_NAME}.zip" "${BUNDLE_NAME}"
    )
}

main() {
    require_command "zip"
    require_file "README.md"
    require_file "LICENSE"
    require_file ".env.example"
    require_file "docker-compose.yml"
    require_file "docker/nginx/fastsell.conf"
    require_dir "db/migrations"
    require_dir "setup/linux"
    require_file "setup/linux/install.sh"
    require_file "setup/linux/update.sh"
    require_file "setup/linux/uninstall.sh"
    require_file "docs/Installation.md"
    require_file "docs/AI_Setup.md"
    require_file "docs/InstallationDetails.md"
    require_file "docs/Backup_Restore.md"
    require_file "docs/System_Requirements.md"
    require_file "docs/Security.md"
    require_file "docs/TheBasics.md"
    require_file "docs/images/ai_setup/gemini_admin_setup.png"
    require_file "docs/images/thebasics/container_types.png"
    require_file "docs/images/thebasics/containers.png"
    require_file "docs/images/thebasics/inv_container_contents.png"
    require_file "docs/images/thebasics/inv_grps.png"
    require_file "docs/images/thebasics/inv_item.png"
    require_file "docs/images/thebasics/inventory.png"
    require_file "docs/images/thebasics/locations.png"
    require_file "docs/images/thebasics/review.png"
    require_file "docs/images/thebasics/sell.png"

    mkdir -p "${BUNDLE_DIR}"

    copy_file "README.md"
    copy_file "LICENSE"
    copy_file ".env.example"
    copy_file "docker-compose.yml"
    copy_dir "db/migrations"
    copy_dir "setup/linux"
    copy_file "docker/nginx/fastsell.conf"
    copy_file "docs/Installation.md"
    copy_file "docs/AI_Setup.md"
    copy_file "docs/InstallationDetails.md"
    copy_file "docs/Backup_Restore.md"
    copy_file "docs/System_Requirements.md"
    copy_file "docs/Security.md"
    copy_file "docs/TheBasics.md"
    copy_file "docs/images/ai_setup/gemini_admin_setup.png"
    copy_file "docs/images/thebasics/container_types.png"

    copy_file "docs/images/thebasics/containers.png"
    copy_file "docs/images/thebasics/inv_container_contents.png"
    copy_file "docs/images/thebasics/inv_grps.png"
    copy_file "docs/images/thebasics/inv_item.png"
    copy_file "docs/images/thebasics/inventory.png"
    copy_file "docs/images/thebasics/locations.png"
    copy_file "docs/images/thebasics/review.png"
    copy_file "docs/images/thebasics/sell.png"

    prepare_image_refs
    apply_image_refs
    verify_image_ref_substitution
    verify_dev_only_not_bundled
    write_archives

    echo "[OK] Wrote ${DIST_DIR}/${BUNDLE_NAME}.zip"
    echo "[OK] Wrote ${DIST_DIR}/${BUNDLE_NAME}.tar.gz"
}

main "$@"
