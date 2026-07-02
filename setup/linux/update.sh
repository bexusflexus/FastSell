#!/usr/bin/env bash
set -euo pipefail

ROOT="/srv/fastsell"
COMPOSE_DIR="${ROOT}/compose"
CONFIG_DIR="${ROOT}/config"
ENV_FILE="${CONFIG_DIR}/.env"
DATA_DIR="${ROOT}/data"
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
  bash setup/linux/update.sh
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
        $0 == "      - /srv/fastsell/data:/app/data" {
            print "      - /srv/fastsell/data/intake:/app/data/intake:Z"
            print "      - /srv/fastsell/data/images:/app/data/images:Z"
            print "      - /srv/fastsell/data/exports:/app/data/exports:Z"
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
        "      - /srv/fastsell/data/exports:/app/data/exports:Z"; do
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
        echo "       bash setup/linux/install.sh"
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
    as_root install -d -m 0755 \
        "${DATA_DIR}" \
        "${DATA_DIR}/intake/incoming" \
        "${DATA_DIR}/intake/processing" \
        "${DATA_DIR}/intake/failed" \
        "${DATA_DIR}/images/originals" \
        "${DATA_DIR}/images/normalized" \
        "${DATA_DIR}/images/thumbnails" \
        "${DATA_DIR}/exports/listing-photos"
    as_root install -m 0644 "${REPO_ROOT}/docker-compose.yml" "${COMPOSE_FILE}"
    as_root install -m 0644 "${REPO_ROOT}/docker/nginx/fastsell.conf" "${NGINX_DIR}/fastsell.conf"

    as_root find "${MIGRATIONS_DIR}" -type f -name '*.sql' -delete
    as_root cp "${REPO_ROOT}/db/migrations/"*.sql "${MIGRATIONS_DIR}/"
    as_root chmod 0644 "${MIGRATIONS_DIR}/"*.sql
    patch_compose_for_docker_selinux
}

repair_runtime_permissions() {
    echo "[OK] Repairing root-owned host runtime permissions"
    as_root find "${ROOT}" -path "${DATA_DIR}/postgres" -prune -o -exec chown root:root {} +
    as_root find "${ROOT}" -path "${DATA_DIR}/postgres" -prune -o -type d -exec chmod 0755 {} +
    as_root find "${ROOT}" -path "${DATA_DIR}/postgres" -prune -o -type f ! -path "${ENV_FILE}" -exec chmod 0644 {} +
    as_root chown root:root "${ENV_FILE}"
    as_root chmod 0600 "${ENV_FILE}"
}

bundle_env_value() {
    local key="$1"
    awk -F= -v key="${key}" '$1 == key { value = substr($0, length(key) + 2) } END { print value }' "${REPO_ROOT}/.env.example"
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

set_env_value() {
    local key="$1"
    local value="$2"
    local tmp_env

    tmp_env="$(mktemp)"
    chmod 600 "${tmp_env}"
    as_root awk -v key="${key}" -v value="${value}" '
        BEGIN { prefix = key "="; seen = 0 }
        index($0, prefix) == 1 { print prefix value; seen = 1; next }
        { print }
        END {
            if (!seen) {
                print prefix value
            }
        }
    ' "${ENV_FILE}" > "${tmp_env}"
    as_root install -m 0600 "${tmp_env}" "${ENV_FILE}"
    as_root chown root:root "${ENV_FILE}"
    rm -f "${tmp_env}"
}

is_managed_fastsell_image() {
    local component="$1"
    local value="$2"

    [[ "${value}" =~ ^ghcr\.io/bexusflexus/fastsell:${component}-(latest|v[0-9]+\.[0-9]+(\.[0-9]+)?)$ ]]
}

update_managed_image_tag() {
    local key="$1"
    local component="$2"
    local current
    local desired

    current="$(env_value "${key}")"
    desired="$(bundle_env_value "${key}")"
    if [ -z "${desired}" ]; then
        return
    fi

    if [ -z "${current}" ] || is_managed_fastsell_image "${component}" "${current}"; then
        if [ "${current}" != "${desired}" ]; then
            echo "[OK] Updating ${key} to ${desired}"
            set_env_value "${key}" "${desired}"
        fi
    else
        echo "[OK] Keeping custom ${key}: ${current}"
    fi
}

update_managed_image_tags() {
    update_managed_image_tag "FASTSELL_API_IMAGE" "api"
    update_managed_image_tag "FASTSELL_SYSTEM_AGENT_IMAGE" "system-agent"
    update_managed_image_tag "FASTSELL_WEB_IMAGE" "web"
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
    show_runtime_status
}

show_runtime_status() {
    as_root ls -ld "${ROOT}"
    as_root ls -ld "${DATA_DIR}"
    as_root ls -ld "${DATA_DIR}/exports"
    as_root ls -ld "${DATA_DIR}/exports/listing-photos"
    as_root ls -ld "${DATA_DIR}/postgres"
    compose ps
}

main() {
    check_docker
    require_existing_install
    update_repo_checkout
    copy_release_files
    update_managed_image_tags
    configure_firewalld
    repair_runtime_permissions
    pull_images
    apply_migrations
    restart_services
    check_health
}

main "$@"
