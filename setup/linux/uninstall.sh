#!/usr/bin/env bash
set -euo pipefail

ROOT="/srv/fastsell"
DATA_DIR="${ROOT}/data"
CONFIG_DIR="${ROOT}/config"
ENV_FILE="${CONFIG_DIR}/.env"
COMPOSE_FILE="${ROOT}/compose/docker-compose.yml"
PROJECT_NAME="fastsell"
DOCKER_CMD=(docker)
KILL_MY_DATA=false

usage() {
    cat <<'USAGE'
Usage:
  bash setup/linux/uninstall.sh [--killmydata]

Default uninstall preserves FastSell user data under /srv/fastsell/data
and installed config under /srv/fastsell/config.

Options:
  --killmydata  Permanently remove all FastSell data, config, and installed
                files under /srv/fastsell.
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
        fastsell_web \
        fastsell_api \
        fastsell_system_agent \
        fastsell_postgres \
        >/dev/null 2>&1 || true

    "${DOCKER_CMD[@]}" network rm fastsell-net >/dev/null 2>&1 || true
}

assert_expected_root() {
    if [ "${ROOT}" != "/srv/fastsell" ]; then
        echo "[FAIL] Refusing to remove unexpected root: ${ROOT}"
        exit 1
    fi
}

remove_app_runtime_files() {
    assert_expected_root

    echo "[OK] Removing FastSell app/runtime files while preserving user data and config"
    as_root rm -rf -- \
        "${ROOT}/compose" \
        "${ROOT}/db"

    if [ -d "${DATA_DIR}" ]; then
        echo "[OK] User data preserved at ${DATA_DIR}"
        echo "     Preserved data includes PostgreSQL data, uploaded images/files, generated exports, and other FastSell runtime data."
    else
        echo "[OK] No FastSell user data directory found at ${DATA_DIR}."
    fi

    if [ -d "${CONFIG_DIR}" ]; then
        echo "[OK] Config preserved at ${CONFIG_DIR}"
        echo "     Preserved config includes .env, database credentials, app paths, port/image settings, and nginx config."
    else
        echo "[OK] No FastSell config directory found at ${CONFIG_DIR}."
    fi
}

remove_runtime_root() {
    assert_expected_root

    echo "======================================================================"
    echo "[WARN] --killmydata was provided."
    echo "[WARN] Permanently deleting all FastSell data, config, and installed files under ${ROOT}."
    echo "[WARN] This includes ${DATA_DIR}, ${CONFIG_DIR}, PostgreSQL data, uploaded images/files, generated exports, and installed app/config/runtime files."
    echo "======================================================================"
    if [ -e "${ROOT}" ]; then
        echo "[WARN] Removing all FastSell files under ${ROOT}"
        as_root rm -rf -- "${ROOT}"
        echo "[OK] Deleted all FastSell data, config, and install files under ${ROOT}"
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
        --killmydata)
            KILL_MY_DATA=true
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
if [ "${KILL_MY_DATA}" = true ]; then
    remove_runtime_root
else
    remove_app_runtime_files
fi

echo "[OK] Docker service and firewalld state were left unchanged."
echo "[OK] FastSell uninstall complete"
