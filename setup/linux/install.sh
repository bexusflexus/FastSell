#!/usr/bin/env bash
set -euo pipefail

ROOT="/srv/fastsell"
COMPOSE_DIR="${ROOT}/compose"
CONFIG_DIR="${ROOT}/config"
ENV_FILE="${CONFIG_DIR}/.env"
DATA_DIR="${ROOT}/data"
BACKUP_DIR="${ROOT}/backups"
MIGRATIONS_DIR="${ROOT}/db/migrations"
NGINX_DIR="${CONFIG_DIR}/nginx"
COMPOSE_FILE="${COMPOSE_DIR}/docker-compose.yml"
PROJECT_NAME="fastsell"
DEFAULT_HTTP_PORT="8888"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=setup/linux/lib/install_guard.sh
source "${SCRIPT_DIR}/lib/install_guard.sh"

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
  bash setup/linux/install.sh
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

docker_selinux_enabled() {
    local security_options

    security_options="$("${DOCKER_CMD[@]}" info --format '{{range .SecurityOptions}}{{println .}}{{end}}' 2>/dev/null || true)"
    printf '%s\n' "${security_options}" | grep -Eiq '(^|[=[:space:]])selinux($|[[:space:]])'
}

patch_compose_for_docker_selinux() {
    local tmp_compose

    if ! docker_selinux_enabled; then
        echo "[OK] Docker does not report SELinux enabled; keeping Compose bind mounts unchanged."
        return
    fi

    echo "[OK] Docker reports SELinux enabled; patching installed Compose bind mounts."
    tmp_compose="$(mktemp)"
    as_root awk '
        $0 == "      - /srv/fastsell/data/postgres:/var/lib/postgresql/data" {
            print "      - /srv/fastsell/data/postgres:/var/lib/postgresql/data:Z"
            next
        }
        $0 == "      - /srv/fastsell/db/migrations:/migrations:ro" {
            print "      - /srv/fastsell/db/migrations:/migrations:ro,Z"
            next
        }
        $0 == "      - /srv/fastsell/data/intake:/app/data/intake" ||
        $0 == "      - /srv/fastsell/data/images:/app/data/images" ||
        $0 == "      - /srv/fastsell/data/exports:/app/data/exports" ||
        $0 == "      - /srv/fastsell/data/videos:/app/data/videos" ||
        $0 == "      - /srv/fastsell/backups:/app/backups" {
            print $0 ":Z"
            next
        }
        $0 == "      - /srv/fastsell/db/migrations:/app/migrations:ro" {
            print "      - /srv/fastsell/db/migrations:/app/migrations:ro,Z"
            next
        }
        $0 == "      - /srv/fastsell/config/nginx/fastsell.conf:/etc/nginx/conf.d/default.conf:ro" {
            print "      - /srv/fastsell/config/nginx/fastsell.conf:/etc/nginx/conf.d/default.conf:ro,Z"
            next
        }
        { print }
    ' "${COMPOSE_FILE}" > "${tmp_compose}"
    as_root install -m 0644 "${tmp_compose}" "${COMPOSE_FILE}"
    rm -f "${tmp_compose}"

    assert_selinux_compose_patch
    cat <<PATCHED
[OK] Applied Docker SELinux mount labels in ${COMPOSE_FILE}:
     /srv/fastsell/data/postgres:/var/lib/postgresql/data:Z
     /srv/fastsell/db/migrations:/migrations:ro,Z
     /srv/fastsell/config/nginx/fastsell.conf:/etc/nginx/conf.d/default.conf:ro,Z
     /srv/fastsell/data/intake:/app/data/intake:Z
     /srv/fastsell/data/images:/app/data/images:Z
     /srv/fastsell/data/exports:/app/data/exports:Z
     /srv/fastsell/data/videos:/app/data/videos:Z
     /srv/fastsell/backups:/app/backups:Z
     /srv/fastsell/db/migrations:/app/migrations:ro,Z
PATCHED
}

assert_selinux_compose_patch() {
    local expected_mount

    for expected_mount in \
        "      - /srv/fastsell/data/postgres:/var/lib/postgresql/data:Z" \
        "      - /srv/fastsell/db/migrations:/migrations:ro,Z" \
        "      - /srv/fastsell/config/nginx/fastsell.conf:/etc/nginx/conf.d/default.conf:ro,Z" \
        "      - /srv/fastsell/data/intake:/app/data/intake:Z" \
        "      - /srv/fastsell/data/images:/app/data/images:Z" \
        "      - /srv/fastsell/data/exports:/app/data/exports:Z" \
        "      - /srv/fastsell/data/videos:/app/data/videos:Z" \
        "      - /srv/fastsell/backups:/app/backups:Z" \
        "      - /srv/fastsell/db/migrations:/app/migrations:ro,Z"; do
        if ! as_root grep -Fxq "${expected_mount}" "${COMPOSE_FILE}"; then
            echo "[FAIL] Expected SELinux Compose mount is missing: ${expected_mount#      - }"
            exit 1
        fi
    done

    if as_root grep -Fq "/var/run/docker.sock:/var/run/docker.sock:ro,Z" "${COMPOSE_FILE}"; then
        echo "[FAIL] Refusing to add SELinux relabeling to /var/run/docker.sock."
        exit 1
    fi
}

http_port_value() {
    local port

    port="${FASTSELL_HTTP_PORT:-}"
    if [ -z "${port}" ] && [ -f "${ENV_FILE}" ]; then
        port="$(as_root awk -F= '$1 == "FASTSELL_HTTP_PORT" { value = $2 } END { print value }' "${ENV_FILE}")"
    fi
    port="${port%\"}"
    port="${port#\"}"
    port="${port%\'}"
    port="${port#\'}"

    printf '%s' "${port:-${DEFAULT_HTTP_PORT}}"
}

configure_firewalld() {
    local http_port

    http_port="$(http_port_value)"

    if ! command -v firewall-cmd >/dev/null 2>&1; then
        echo "[OK] firewalld is not installed; leaving firewall rules unchanged."
        return
    fi

    if ! as_root firewall-cmd --state >/dev/null 2>&1; then
        echo "[OK] firewalld is installed but inactive; leaving firewall rules unchanged."
        return
    fi

    echo "[OK] firewalld is active; opening FastSell ports ${http_port}/tcp and 5432/tcp."
    as_root firewall-cmd --permanent --add-port="${http_port}/tcp"
    as_root firewall-cmd --permanent --add-port="5432/tcp"
    as_root firewall-cmd --reload
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

bundle_env_value() {
    local key="$1"
    awk -F= -v key="${key}" '$1 == key { value = substr($0, length(key) + 2) } END { print value }' "${REPO_ROOT}/.env.example"
}

bundle_version() {
    local value
    value="$(bundle_env_value FASTSELL_VERSION)"
    if [ -z "${value}" ] || [[ "${value}" == *[[:space:]=]* ]]; then
        echo "[FAIL] Setup bundle does not contain a valid FASTSELL_VERSION." >&2
        exit 1
    fi
    printf '%s' "${value}"
}

write_env_file() {
    local encoded_password
    local tmp_env
    local version
    encoded_password="$(urlencode "${POSTGRES_PASSWORD_VALUE}")"
    version="$(bundle_version)"
    tmp_env="$(mktemp)"
    chmod 600 "${tmp_env}"

    cat > "${tmp_env}" <<ENV
POSTGRES_DB=fastsell
POSTGRES_USER=fastsell
POSTGRES_PASSWORD=${POSTGRES_PASSWORD_VALUE}
DATABASE_URL=postgres://fastsell:${encoded_password}@postgres:5432/fastsell?sslmode=disable
FASTSELL_VERSION=${version}

DATA_ROOT=/app/data
FASTSELL_BACKUP_ROOT=/app/backups
FASTSELL_MIGRATION_ROOT=/app/migrations
INTAKE_DIR=/app/data/intake/incoming
INTAKE_PROCESSING_DIR=/app/data/intake/processing
INTAKE_FAILED_DIR=/app/data/intake/failed
IMAGE_ROOT=/app/data/images
MAX_UPLOAD_MB=25
ITEM_IMAGE_MAX_UPLOAD_MB=10
ITEM_IMAGE_MAX_COUNT=50

FRONTEND_HOSTING_MODE=nginx
FRONTEND_PUBLIC_URL=${FASTSELL_PUBLIC_URL:-http://localhost:${FASTSELL_HTTP_PORT:-${DEFAULT_HTTP_PORT}}}
SYSTEM_AGENT_URL=http://system-agent:8081

LISTING_PHOTO_EXPORT_ROOT=/app/data/exports/listing-photos
LISTING_PHOTO_EXPORT_HOST_ROOT=/srv/fastsell/data/exports/listing-photos
LISTING_PHOTO_EXPORT_TTL_HOURS=24

FASTSELL_ENV_FILE=/srv/fastsell/config/.env
FASTSELL_HTTP_PORT=${FASTSELL_HTTP_PORT:-${DEFAULT_HTTP_PORT}}
FASTSELL_API_IMAGE=${FASTSELL_API_IMAGE:-ghcr.io/bexusflexus/fastsell:api-latest}
FASTSELL_SYSTEM_AGENT_IMAGE=${FASTSELL_SYSTEM_AGENT_IMAGE:-ghcr.io/bexusflexus/fastsell:system-agent-latest}
FASTSELL_WEB_IMAGE=${FASTSELL_WEB_IMAGE:-ghcr.io/bexusflexus/fastsell:web-latest}
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
        -f "${COMPOSE_FILE}" \
        "$@"
}

prepare_runtime_tree() {
    echo "[OK] Creating runtime directories under ${ROOT}"
    as_root install -d -m 0755 "${ROOT}" "${COMPOSE_DIR}" "${CONFIG_DIR}" "${NGINX_DIR}" "${MIGRATIONS_DIR}"
    as_root install -d -m 0755 \
        "${DATA_DIR}" \
        "${DATA_DIR}/intake/incoming" \
        "${DATA_DIR}/intake/processing" \
        "${DATA_DIR}/intake/failed" \
        "${DATA_DIR}/images/originals" \
        "${DATA_DIR}/images/normalized" \
        "${DATA_DIR}/images/thumbnails" \
        "${DATA_DIR}/exports/listing-photos" \
        "${DATA_DIR}/videos"
    as_root install -d -m 0700 -o 70 -g 70 "${DATA_DIR}/postgres"
    as_root install -d -m 0700 \
        "${BACKUP_DIR}" \
        "${BACKUP_DIR}/database" \
        "${BACKUP_DIR}/media" \
        "${BACKUP_DIR}/jobs" \
        "${BACKUP_DIR}/restore-staging"
}

copy_release_files() {
    echo "[OK] Copying release files"
    as_root install -m 0644 "${REPO_ROOT}/docker-compose.yml" "${COMPOSE_FILE}"
    as_root install -m 0644 "${REPO_ROOT}/docker/nginx/fastsell.conf" "${NGINX_DIR}/fastsell.conf"

    as_root find "${MIGRATIONS_DIR}" -type f -name '*.sql' -delete
    as_root cp "${REPO_ROOT}/db/migrations/"*.sql "${MIGRATIONS_DIR}/"
    as_root chmod 0644 "${MIGRATIONS_DIR}/"*.sql
    patch_compose_for_docker_selinux
}

repair_runtime_permissions() {
    echo "[OK] Setting root-owned host runtime permissions"
    as_root find "${ROOT}" \( -path "${DATA_DIR}/postgres" -o -path "${BACKUP_DIR}" \) -prune -o -exec chown root:root {} +
    as_root find "${ROOT}" \( -path "${DATA_DIR}/postgres" -o -path "${BACKUP_DIR}" \) -prune -o -type d -exec chmod 0755 {} +
    as_root find "${ROOT}" \( -path "${DATA_DIR}/postgres" -o -path "${BACKUP_DIR}" \) -prune -o -type f ! -path "${ENV_FILE}" -exec chmod 0644 {} +
    as_root chown -R root:root "${BACKUP_DIR}"
    as_root find "${BACKUP_DIR}" -type d -exec chmod 0700 {} +
    as_root find "${BACKUP_DIR}" -type f -exec chmod 0600 {} +
    as_root chown root:root "${ENV_FILE}"
    as_root chmod 0600 "${ENV_FILE}"
}

show_runtime_status() {
    as_root ls -ld "${ROOT}"
    as_root ls -ld "${DATA_DIR}"
    as_root ls -ld "${DATA_DIR}/exports"
    as_root ls -ld "${DATA_DIR}/exports/listing-photos"
    as_root ls -ld "${DATA_DIR}/postgres"
    as_root ls -ld "${BACKUP_DIR}" "${BACKUP_DIR}/database" "${BACKUP_DIR}/media" "${BACKUP_DIR}/jobs" "${BACKUP_DIR}/restore-staging"
    compose ps
}

main() {
    if fastsell_runtime_has_state "${ROOT}"; then
        fastsell_print_existing_install_failure "${ROOT}"
        exit 1
    fi

    check_docker

    if fastsell_docker_resources_exist; then
        fastsell_print_existing_install_failure "${ROOT}"
        exit 1
    fi

    prompt_password
    prepare_runtime_tree
    write_env_file
    copy_release_files
    configure_firewalld

    echo "[OK] Pulling FastSell images"
    compose --profile tools pull
    repair_runtime_permissions

    echo "[OK] Starting PostgreSQL"
    compose up -d postgres

    echo "[OK] Applying database migrations"
    compose run --rm migrate

    echo "[OK] Starting FastSell"
    compose up -d

    echo "[OK] FastSell install complete"
    show_runtime_status
}

main "$@"
