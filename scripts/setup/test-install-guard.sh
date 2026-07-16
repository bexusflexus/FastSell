#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=setup/linux/lib/install_guard.sh
source "${REPO_ROOT}/setup/linux/lib/install_guard.sh"

TEST_ROOT="$(mktemp -d)"
cleanup() {
    rm -rf -- "${TEST_ROOT}"
}
trap cleanup EXIT

MOCK_LOG="${TEST_ROOT}/docker.log"
MOCK_DOCKER="${TEST_ROOT}/docker"
cat > "${MOCK_DOCKER}" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${MOCK_LOG}"
case "${1:-} ${2:-}" in
    "ps -a")
        [ "${MOCK_COMPOSE_CONTAINER:-false}" = true ] && printf 'container-id\n'
        ;;
    "container inspect")
        [ "${MOCK_NAMED_CONTAINER:-false}" = true ] && exit 0
        exit 1
        ;;
    "network inspect")
        [ "${MOCK_NETWORK:-false}" = true ] && exit 0
        exit 1
        ;;
    "volume ls")
        [ "${MOCK_VOLUME:-false}" = true ] && printf 'fastsell-volume\n'
        ;;
esac
exit 0
MOCK
chmod +x "${MOCK_DOCKER}"
export MOCK_LOG
DOCKER_CMD=("${MOCK_DOCKER}")

guard() {
    local root="$1"
    if fastsell_runtime_has_state "${root}" || fastsell_docker_resources_exist; then
        fastsell_print_existing_install_failure "${root}"
        return 1
    fi
}

expect_refusal() {
    local name="$1"
    local root="$2"
    local watched="$3"
    local before_hash=""
    local before_time=""
    local output

    if [ -e "${watched}" ]; then
        before_hash="$(sha256sum "${watched}")"
        before_time="$(stat -c '%y' "${watched}")"
    fi
    if output="$(guard "${root}" 2>&1)"; then
        echo "[FAIL] ${name}: guard unexpectedly allowed installation" >&2
        exit 1
    fi
    [[ "${output}" == *"An existing or partial FastSell installation was detected"* ]] || {
        echo "[FAIL] ${name}: refusal message missing" >&2
        exit 1
    }
    [[ "${output}" == *"sudo fastsell-update"* ]] || {
        echo "[FAIL] ${name}: update guidance missing" >&2
        exit 1
    }
    if [ -e "${watched}" ]; then
        [ "$(sha256sum "${watched}")" = "${before_hash}" ] || {
            echo "[FAIL] ${name}: file contents changed" >&2
            exit 1
        }
        [ "$(stat -c '%y' "${watched}")" = "${before_time}" ] || {
            echo "[FAIL] ${name}: file timestamp changed" >&2
            exit 1
        }
    fi
    echo "[OK] ${name}"
}

new_case_root() {
    local name="$1"
    local root="${TEST_ROOT}/${name}/srv/fastsell"
    mkdir -p "${root}"
    printf '%s' "${root}"
}

root="$(new_case_root existing-env)"
mkdir -p "${root}/config"
printf 'FASTSELL_VERSION=v0.1.3\n' > "${root}/config/.env"
expect_refusal "existing .env" "${root}" "${root}/config/.env"

root="$(new_case_root postgres-data)"
mkdir -p "${root}/data/postgres/base"
printf 'database-page' > "${root}/data/postgres/base/1"
expect_refusal "nonempty PostgreSQL directory" "${root}" "${root}/data/postgres/base/1"

root="$(new_case_root preserved-images)"
mkdir -p "${root}/data/images/originals"
printf 'image' > "${root}/data/images/originals/item.jpg"
expect_refusal "preserved images" "${root}" "${root}/data/images/originals/item.jpg"

root="$(new_case_root preserved-intake)"
mkdir -p "${root}/data/intake/incoming"
printf 'incoming-data' > "${root}/data/intake/incoming/item.bin"
expect_refusal "preserved intake data" "${root}" "${root}/data/intake/incoming/item.bin"

root="$(new_case_root preserved-backups)"
mkdir -p "${root}/backups/database"
printf 'dump' > "${root}/backups/database/backup.dump"
expect_refusal "preserved backups" "${root}" "${root}/backups/database/backup.dump"

root="$(new_case_root compose-file)"
mkdir -p "${root}/compose"
printf 'services: {}\n' > "${root}/compose/docker-compose.yml"
expect_refusal "installed Compose file" "${root}" "${root}/compose/docker-compose.yml"

root="$(new_case_root partial-directory)"
mkdir -p "${root}/data/exports"
printf 'export' > "${root}/data/exports/listing.csv"
expect_refusal "other nonempty runtime directory" "${root}" "${root}/data/exports/listing.csv"

root="$(new_case_root partial-file)"
printf 'partial-install' > "${root}/.install-in-progress"
expect_refusal "obvious partial-install file" "${root}" "${root}/.install-in-progress"

root="$(new_case_root docker-container)"
container_state="${TEST_ROOT}/docker-container-state"
printf 'unchanged' > "${container_state}"
MOCK_COMPOSE_CONTAINER=true
export MOCK_COMPOSE_CONTAINER
expect_refusal "existing Compose container resource" "${root}" "${container_state}"
unset MOCK_COMPOSE_CONTAINER

root="$(new_case_root named-container)"
MOCK_NAMED_CONTAINER=true
export MOCK_NAMED_CONTAINER
expect_refusal "existing named FastSell container" "${root}" "${container_state}"
unset MOCK_NAMED_CONTAINER

root="$(new_case_root docker-network)"
MOCK_NETWORK=true
export MOCK_NETWORK
expect_refusal "existing Compose network resource" "${root}" "${container_state}"
unset MOCK_NETWORK

root="$(new_case_root docker-volume)"
MOCK_VOLUME=true
export MOCK_VOLUME
expect_refusal "existing labeled Compose volume resource" "${root}" "${container_state}"
unset MOCK_VOLUME

empty_root="$(new_case_root empty-root)"
guard "${empty_root}" >/dev/null || {
    echo "[FAIL] completely empty runtime root was refused" >&2
    exit 1
}
echo "[OK] completely empty /srv/fastsell is allowed"

missing_root="${TEST_ROOT}/missing/srv/fastsell"
guard "${missing_root}" >/dev/null || {
    echo "[FAIL] missing runtime root was refused" >&2
    exit 1
}
echo "[OK] missing /srv/fastsell is allowed"

if rg -n -- '--force' "${REPO_ROOT}/setup/linux/install.sh" >/dev/null; then
    echo "[FAIL] install.sh contains a force bypass" >&2
    exit 1
fi

filesystem_guard_line="$(rg -n '^    if fastsell_runtime_has_state ' "${REPO_ROOT}/setup/linux/install.sh" | cut -d: -f1)"
docker_guard_line="$(rg -n '^    if fastsell_docker_resources_exist' "${REPO_ROOT}/setup/linux/install.sh" | cut -d: -f1)"
for mutation in prompt_password prepare_runtime_tree write_env_file copy_release_files configure_firewalld install_update_command; do
    mutation_line="$(rg -n "^    ${mutation}([[:space:]]|$)" "${REPO_ROOT}/setup/linux/install.sh" | tail -n 1 | cut -d: -f1)"
    if [ "${filesystem_guard_line}" -ge "${mutation_line}" ] || [ "${docker_guard_line}" -ge "${mutation_line}" ]; then
        echo "[FAIL] install guard does not precede ${mutation}" >&2
        exit 1
    fi
done

if rg -n '(^|[[:space:]])(rm|stop|kill|down|up|pull|create|start)([[:space:]]|$)' "${MOCK_LOG}" >/dev/null; then
    echo "[FAIL] Docker guard issued a mutating Docker command" >&2
    exit 1
fi

echo "[OK] install guard is read-only, precedes every installer mutation, and has no force bypass"
