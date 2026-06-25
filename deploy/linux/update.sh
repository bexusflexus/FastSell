#!/usr/bin/env bash
set -euo pipefail

ROOT="/srv/fastsell"
COMPOSE_DIR="${ROOT}/compose"
CONFIG_DIR="${ROOT}/config"
ENV_FILE="${CONFIG_DIR}/.env"
MIGRATIONS_DIR="${ROOT}/db/migrations"
NGINX_DIR="${CONFIG_DIR}/nginx"
COMPOSE_FILE="${COMPOSE_DIR}/docker-compose.yml"
PROJECT_NAME="fastsell"
DEFAULT_HTTP_PORT="8888"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

DOCKER_CMD=(docker)

as_root() {
    if [ "${EUID}" -eq 0 ]; then
        "$@"
    else
        if ! command -v sudo >/dev/null 2>&1; then
            echo "[FAIL] This script needs permission to write ${ROOT}, but sudo is not installed."
            exit 1
        fi
        sudo "$@"
    fi
}

print_docker_guidance() {
    cat <<'GUIDANCE'
[FAIL] Docker Engine and the Docker Compose plugin are required.

Arch Linux:
  sudo pacman -S docker docker-compose
  sudo systemctl enable --now docker

Debian/Ubuntu:
  Install Docker Engine and the Compose plugin from Docker's official apt repository:
  https://docs.docker.com/engine/install/debian/
  https://docs.docker.com/engine/install/ubuntu/

After installation, rerun:
  bash deploy/linux/update.sh
GUIDANCE
}

check_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        print_docker_guidance
        exit 1
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
        echo "       Start the Docker service, then rerun this updater."
        exit 1
    fi

    if ! "${DOCKER_CMD[@]}" compose version >/dev/null 2>&1; then
        print_docker_guidance
        exit 1
    fi
}

compose() {
    "${DOCKER_CMD[@]}" compose \
        --env-file "${ENV_FILE}" \
        --project-name "${PROJECT_NAME}" \
        -f "${COMPOSE_FILE}" \
        "$@"
}

require_existing_install() {
    if [ ! -d "${ROOT}" ]; then
        echo "[FAIL] ${ROOT} does not exist. Install FastSell first:"
        echo "       bash deploy/linux/install.sh"
        exit 1
    fi

    if [ ! -f "${ENV_FILE}" ]; then
        echo "[FAIL] Runtime config ${ENV_FILE} is missing. This updater will not create or overwrite it."
        exit 1
    fi
}

update_repo_checkout() {
    if [ ! -d "${REPO_ROOT}/.git" ]; then
        echo "[OK] Repository is not a git checkout; using files from ${REPO_ROOT}"
        return
    fi

    if ! command -v git >/dev/null 2>&1; then
        echo "[FAIL] ${REPO_ROOT} is a git checkout, but git is not installed."
        echo "       Install git or manually update this checkout before rerunning:"
        echo "       git -C ${REPO_ROOT} pull --ff-only"
        exit 1
    fi

    echo "[OK] Updating repository checkout"
    if ! git -C "${REPO_ROOT}" pull --ff-only; then
        echo "[FAIL] Could not fast-forward ${REPO_ROOT}."
        echo "       Resolve the checkout manually, then rerun:"
        echo "       git -C ${REPO_ROOT} pull --ff-only"
        exit 1
    fi
}

copy_release_files() {
    echo "[OK] Copying updated runtime files"
    as_root install -d -m 0755 "${ROOT}" "${COMPOSE_DIR}" "${CONFIG_DIR}" "${NGINX_DIR}" "${MIGRATIONS_DIR}"
    as_root install -m 0644 "${REPO_ROOT}/docker-compose.yml" "${COMPOSE_FILE}"
    as_root install -m 0644 "${REPO_ROOT}/docker/nginx/fastsell.conf" "${NGINX_DIR}/fastsell.conf"

    as_root find "${MIGRATIONS_DIR}" -type f -name '*.sql' -delete
    as_root cp "${REPO_ROOT}/db/migrations/"*.sql "${MIGRATIONS_DIR}/"
    as_root chmod 0644 "${MIGRATIONS_DIR}/"*.sql
}

pull_images() {
    echo "[OK] Pulling updated FastSell images"
    compose --profile tools pull migrate system-agent api web
}

apply_migrations() {
    echo "[OK] Starting PostgreSQL"
    compose up -d postgres

    echo "[OK] Applying database migrations"
    compose run --rm migrate
}

restart_services() {
    echo "[OK] Starting updated FastSell services"
    compose up -d
}

env_value() {
    local key="$1"
    local value

    case "${key}" in
        ""|*[!A-Za-z0-9_]*)
            echo "[FAIL] Invalid env key requested: ${key}" >&2
            exit 1
            ;;
    esac

    value="$(as_root awk -v key="${key}" '
        BEGIN { prefix = key "=" }
        index($0, prefix) == 1 { value = substr($0, length(prefix) + 1) }
        END { print value }
    ' "${ENV_FILE}")"
    value="${value%\"}"
    value="${value#\"}"
    value="${value%\'}"
    value="${value#\'}"
    printf '%s' "${value}"
}

check_health() {
    local port
    local app_url

    port="$(env_value FASTSELL_HTTP_PORT)"
    if [ -z "${port}" ]; then
        port="${DEFAULT_HTTP_PORT}"
    fi
    app_url="http://localhost:${port}"

    echo "[OK] Checking FastSell health"
    if ! curl -fsS "${app_url}/health" >/dev/null; then
        echo "[FAIL] ${app_url}/health did not return success."
        compose ps
        exit 1
    fi

    if ! curl -fsS "${app_url}/health/db" >/dev/null; then
        echo "[FAIL] ${app_url}/health/db did not return success."
        compose ps
        exit 1
    fi

    echo "[OK] FastSell update complete"
    echo "[OK] Open ${app_url}"
    compose ps
}

main() {
    check_docker
    require_existing_install
    update_repo_checkout
    copy_release_files
    pull_images
    apply_migrations
    restart_services
    check_health
}

main "$@"
