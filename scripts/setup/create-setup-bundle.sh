#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  bash scripts/setup/create-setup-bundle.sh v0.1.0
USAGE
}

if [ "$#" -ne 1 ]; then
    usage
    exit 1
fi

VERSION="$1"
case "${VERSION}" in
    v[0-9]*)
        ;;
    *)
        echo "[FAIL] Version must start with v and contain a release identifier, for example v0.1.0." >&2
        exit 1
        ;;
esac

case "${VERSION}" in
    *[!A-Za-z0-9._-]*)
        echo "[FAIL] Version may only contain letters, numbers, dots, underscores, and dashes." >&2
        exit 1
        ;;
esac

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

apply_versioned_image_tags() {
    if [[ ! "${VERSION}" =~ ^v[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
        echo "[OK] ${VERSION} is not a release semver tag; keeping latest image defaults."
        return
    fi

    echo "[OK] Using versioned FastSell image tags for ${VERSION}"
    sed -i \
        -e "s#ghcr.io/bexusflexus/fastsell:api-latest#ghcr.io/bexusflexus/fastsell:api-${VERSION}#g" \
        -e "s#ghcr.io/bexusflexus/fastsell:system-agent-latest#ghcr.io/bexusflexus/fastsell:system-agent-${VERSION}#g" \
        -e "s#ghcr.io/bexusflexus/fastsell:web-latest#ghcr.io/bexusflexus/fastsell:web-${VERSION}#g" \
        "${BUNDLE_DIR}/.env.example" \
        "${BUNDLE_DIR}/docker-compose.yml" \
        "${BUNDLE_DIR}/setup/linux/install.sh"
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
    require_file "docs/Deployment.md"
    require_file "docs/Backup_Restore.md"
    require_file "docs/System_Requirements.md"
    require_file "docs/Security.md"

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
    copy_file "docs/Deployment.md"
    copy_file "docs/Backup_Restore.md"
    copy_file "docs/System_Requirements.md"
    copy_file "docs/Security.md"

    apply_versioned_image_tags
    write_archives

    echo "[OK] Wrote ${DIST_DIR}/${BUNDLE_NAME}.zip"
    echo "[OK] Wrote ${DIST_DIR}/${BUNDLE_NAME}.tar.gz"
}

main "$@"
