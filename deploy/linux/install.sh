#!/usr/bin/env bash
set -euo pipefail

ROOT="/srv/fastsell"
COMPOSE_DIR="${ROOT}/compose"
CONFIG_DIR="${ROOT}/config"
ENV_FILE="${CONFIG_DIR}/.env"
DATA_DIR="${ROOT}/data"
MIGRATIONS_DIR="${ROOT}/db/migrations"
NGINX_DIR="${CONFIG_DIR}/nginx"
PROJECT_NAME="fastsell"
DEFAULT_HTTP_PORT="8888"
DEFAULT_API_IMAGE="ghcr.io/bexusflexus/fastsell-api:latest"

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
  bash deploy/linux/install.sh
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
        echo "       Start the Docker service, then rerun this installer."
        exit 1
    fi

    if ! "${DOCKER_CMD[@]}" compose version >/dev/null 2>&1; then
        print_docker_guidance
        exit 1
    fi
}

urlencode() {
    local input="$1"
    local output=""
    local i char encoded

    for ((i = 0; i < ${#input}; i++)); do
        char="${input:i:1}"
        case "${char}" in
            [a-zA-Z0-9.~_-])
                output+="${char}"
                ;;
            *)
                printf -v encoded '%%%02X' "'${char}"
                output+="${encoded}"
                ;;
        esac
    done

    printf '%s' "${output}"
}

prompt_password() {
    local first second

    read -r -s -p "PostgreSQL password: " first
    printf '\n'
    read -r -s -p "Confirm PostgreSQL password: " second
    printf '\n'

    if [ -z "${first}" ]; then
        echo "[FAIL] PostgreSQL password cannot be empty."
        exit 1
    fi

    if [ "${first}" != "${second}" ]; then
        echo "[FAIL] PostgreSQL passwords did not match."
        exit 1
    fi

    POSTGRES_PASSWORD_VALUE="${first}"
}

write_env_file() {
    local encoded_password
    local tmp_env
    encoded_password="$(urlencode "${POSTGRES_PASSWORD_VALUE}")"
    tmp_env="$(mktemp)"
    chmod 600 "${tmp_env}"

    cat > "${tmp_env}" <<ENV
POSTGRES_DB=fastsell
POSTGRES_USER=fastsell
POSTGRES_PASSWORD=${POSTGRES_PASSWORD_VALUE}
DATABASE_URL=postgres://fastsell:${encoded_password}@postgres:5432/fastsell?sslmode=disable

DATA_ROOT=/app/data
INTAKE_DIR=/app/data/intake/incoming
INTAKE_PROCESSING_DIR=/app/data/intake/processing
INTAKE_FAILED_DIR=/app/data/intake/failed
IMAGE_ROOT=/app/data/images
MAX_UPLOAD_MB=25
ITEM_IMAGE_MAX_UPLOAD_MB=10
ITEM_IMAGE_MAX_COUNT=50

FRONTEND_HOSTING_MODE=nginx
FRONTEND_PUBLIC_URL=${FASTSELL_PUBLIC_URL:-http://localhost:${FASTSELL_HTTP_PORT:-${DEFAULT_HTTP_PORT}}}
SYSTEM_AGENT_URL=http://fastsell-system-agent:8081

LISTING_PHOTO_EXPORT_ROOT=/app/data/exports/listing-photos
LISTING_PHOTO_EXPORT_HOST_ROOT=/srv/fastsell/data/exports/listing-photos
LISTING_PHOTO_EXPORT_TTL_HOURS=24

FASTSELL_ENV_FILE=/srv/fastsell/config/.env
FASTSELL_HTTP_PORT=${FASTSELL_HTTP_PORT:-${DEFAULT_HTTP_PORT}}
FASTSELL_API_IMAGE=${FASTSELL_API_IMAGE:-ghcr.io/bexusflexus/fastsell-api:latest}
FASTSELL_SYSTEM_AGENT_IMAGE=${FASTSELL_SYSTEM_AGENT_IMAGE:-ghcr.io/bexusflexus/fastsell-system-agent:latest}
FASTSELL_WEB_IMAGE=${FASTSELL_WEB_IMAGE:-ghcr.io/bexusflexus/fastsell-web:latest}
FASTSELL_MIGRATE_IMAGE=${FASTSELL_MIGRATE_IMAGE:-migrate/migrate:v4.18.3}
ENV

    as_root install -m 0600 "${tmp_env}" "${ENV_FILE}"
    as_root chown root:root "${ENV_FILE}"
    as_root chmod 0600 "${ENV_FILE}"
    rm -f "${tmp_env}"
}

compose() {
    "${DOCKER_CMD[@]}" compose \
        --env-file "${ENV_FILE}" \
        --project-name "${PROJECT_NAME}" \
        -f "${COMPOSE_DIR}/docker-compose.yml" \
        "$@"
}

prepare_runtime_tree() {
    echo "[OK] Creating runtime directories under ${ROOT}"
    as_root install -d -m 0755 "${ROOT}" "${COMPOSE_DIR}" "${CONFIG_DIR}" "${NGINX_DIR}" "${MIGRATIONS_DIR}"
    as_root install -d -m 0755 \
        "${DATA_DIR}/postgres" \
        "${DATA_DIR}/intake/incoming" \
        "${DATA_DIR}/intake/processing" \
        "${DATA_DIR}/intake/failed" \
        "${DATA_DIR}/images/originals" \
        "${DATA_DIR}/images/normalized" \
        "${DATA_DIR}/images/thumbnails" \
        "${DATA_DIR}/exports/listing-photos"
}

copy_release_files() {
    echo "[OK] Copying release files"
    as_root install -m 0644 "${REPO_ROOT}/docker-compose.yml" "${COMPOSE_DIR}/docker-compose.yml"
    as_root install -m 0644 "${REPO_ROOT}/docker/nginx/fastsell.conf" "${NGINX_DIR}/fastsell.conf"

    as_root find "${MIGRATIONS_DIR}" -type f -name '*.sql' -delete
    as_root cp "${REPO_ROOT}/db/migrations/"*.sql "${MIGRATIONS_DIR}/"
    as_root chmod 0644 "${MIGRATIONS_DIR}/"*.sql
}

env_value() {
    local key="$1"
    as_root awk -F= -v key="${key}" '$1 == key { value = substr($0, length(key) + 2) } END { print value }' "${ENV_FILE}"
}

discover_app_uid_gid() {
    local api_image
    api_image="$(env_value FASTSELL_API_IMAGE)"
    if [ -z "${api_image}" ]; then
        api_image="${DEFAULT_API_IMAGE}"
    fi

    echo "[OK] Discovering fastsell UID/GID from ${api_image}"
    "${DOCKER_CMD[@]}" image inspect "${api_image}" >/dev/null
    APP_UID="$("${DOCKER_CMD[@]}" run --rm --entrypoint id "${api_image}" -u fastsell)"
    APP_GID="$("${DOCKER_CMD[@]}" run --rm --entrypoint id "${api_image}" -g fastsell)"
    [[ "${APP_UID}" =~ ^[0-9]+$ ]] || {
        echo "[FAIL] Could not discover fastsell UID from API image."
        exit 1
    }
    [[ "${APP_GID}" =~ ^[0-9]+$ ]] || {
        echo "[FAIL] Could not discover fastsell GID from API image."
        exit 1
    }
}

fix_app_writable_permissions() {
    echo "[OK] Setting app-writable bind mount ownership to ${APP_UID}:${APP_GID}"
    as_root chown "${APP_UID}:${APP_GID}" "${DATA_DIR}"
    as_root chmod 2770 "${DATA_DIR}"
    as_root chown -R "${APP_UID}:${APP_GID}" \
        "${DATA_DIR}/intake" \
        "${DATA_DIR}/images" \
        "${DATA_DIR}/exports"
}

main() {
    check_docker
    prompt_password
    prepare_runtime_tree
    write_env_file
    copy_release_files

    echo "[OK] Pulling FastSell images"
    compose --profile tools pull
    discover_app_uid_gid
    fix_app_writable_permissions

    echo "[OK] Starting PostgreSQL"
    compose up -d postgres

    echo "[OK] Applying database migrations"
    compose run --rm migrate

    echo "[OK] Starting FastSell"
    compose up -d

    echo "[OK] FastSell install complete"
    compose ps
}

main "$@"
