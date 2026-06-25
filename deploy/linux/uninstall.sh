#!/usr/bin/env bash
set -euo pipefail

ROOT="/srv/fastsell"
ENV_FILE="${ROOT}/config/.env"
COMPOSE_FILE="${ROOT}/compose/docker-compose.yml"
PROJECT_NAME="fastsell"
DOCKER_CMD=(docker)

usage() {
    cat <<'USAGE'
Usage:
  bash deploy/linux/uninstall.sh
USAGE
}

as_root() {
    if [ "${EUID}" -eq 0 ]; then
        "$@"
    else
        if ! command -v sudo >/dev/null 2>&1; then
            echo "[FAIL] sudo is required to remove ${ROOT}."
            exit 1
        fi
        sudo "$@"
    fi
}

check_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        echo "[WARN] Docker is not installed; no Docker containers or networks will be removed."
        return 1
    fi

    if [ "${EUID}" -eq 0 ]; then
        DOCKER_CMD=(docker)
    else
        if ! command -v sudo >/dev/null 2>&1; then
            echo "[FAIL] sudo is required to manage Docker as a non-root user."
            exit 1
        fi
        DOCKER_CMD=(sudo docker)
    fi

    if ! "${DOCKER_CMD[@]}" ps >/dev/null 2>&1; then
        echo "[FAIL] Docker is installed, but the daemon is not usable with ${DOCKER_CMD[*]}."
        echo "       Start the Docker service, then rerun this uninstaller."
        exit 1
    fi

    if ! "${DOCKER_CMD[@]}" compose version >/dev/null 2>&1; then
        echo "[FAIL] Docker Compose plugin is required to remove FastSell compose services."
        exit 1
    fi

    return 0
}

compose_down() {
    if [ -f "${COMPOSE_FILE}" ] && [ -f "${ENV_FILE}" ] && command -v docker >/dev/null 2>&1; then
        "${DOCKER_CMD[@]}" compose \
            --env-file "${ENV_FILE}" \
            --project-name "${PROJECT_NAME}" \
            -f "${COMPOSE_FILE}" \
            down --remove-orphans
    else
        echo "[WARN] FastSell compose/env files not found; skipping compose down."
    fi
}

remove_known_docker_artifacts() {
    "${DOCKER_CMD[@]}" rm -f \
        fastsell-web \
        fastsell-api \
        fastsell-system-agent \
        fastsell-postgres \
        >/dev/null 2>&1 || true

    "${DOCKER_CMD[@]}" network rm fastsell-net >/dev/null 2>&1 || true
}

remove_runtime_root() {
    if [ "${ROOT}" != "/srv/fastsell" ]; then
        echo "[FAIL] Refusing to remove unexpected root: ${ROOT}"
        exit 1
    fi

    if [ -e "${ROOT}" ]; then
        echo "[WARN] Removing all FastSell runtime data under ${ROOT}"
        as_root rm -rf -- "${ROOT}"
    else
        echo "[OK] Runtime directory ${ROOT} is already absent."
    fi
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        -h|--help)
            usage
            exit 0
            ;;
        *)
            usage
            exit 1
            ;;
    esac
    shift
done

if check_docker; then
    compose_down
    remove_known_docker_artifacts
fi
remove_runtime_root

echo "[OK] FastSell uninstall complete"
