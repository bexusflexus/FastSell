#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  fetch_candidate_bundle.sh --install-root <path>

Copies dev_only/fetch_candidate_bundle.sh into an existing FastSell install root.
Run the copied helper from <install-root>/dev_only on the staging host.
USAGE
}

INSTALL_ROOT=""

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
            --install-root)
                [ "$#" -ge 2 ] || { echo "[FAIL] --install-root requires a path." >&2; exit 1; }
                INSTALL_ROOT="$2"
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
}

validate_install_root() {
    local root="$1"

    if [ -z "${root}" ]; then
        echo "[FAIL] --install-root is required." >&2
        exit 1
    fi

    if [ ! -d "${root}" ]; then
        echo "[FAIL] Install root does not exist: ${root}" >&2
        exit 1
    fi

    if [ ! -f "${root}/docker-compose.yml" ] ||
        [ ! -f "${root}/.env.example" ] ||
        [ ! -f "${root}/setup/linux/update.sh" ]; then
        echo "[FAIL] Install root does not look like a FastSell install tree: ${root}" >&2
        echo "       Expected docker-compose.yml, .env.example, and setup/linux/update.sh." >&2
        exit 1
    fi
}

main() {
    parse_args "$@"
    require_cmd git

    cd "$(repo_root)"
    validate_install_root "${INSTALL_ROOT}"

    mkdir -p "${INSTALL_ROOT}/dev_only"
    cp -p "dev_only/fetch_candidate_bundle.sh" "${INSTALL_ROOT}/dev_only/fetch_candidate_bundle.sh"

    cat <<NEXT
[OK] Installed candidate helper:
     ${INSTALL_ROOT}/dev_only/fetch_candidate_bundle.sh

[OK] On the staging host, run:
     cd ${INSTALL_ROOT}/dev_only
     ./fetch_candidate_bundle.sh <full-sha>
NEXT
}

main "$@"
