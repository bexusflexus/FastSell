#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

require_literal() {
    local file="$1"
    local value="$2"
    if ! rg -Fq -- "${value}" "${REPO_ROOT}/${file}"; then
        echo "[FAIL] ${file} is missing required backup runtime value: ${value}" >&2
        exit 1
    fi
}

for script in setup/linux/install.sh setup/linux/update.sh; do
    require_literal "${script}" 'install -d -m 0700'
    require_literal "${script}" '"${BACKUP_DIR}/database"'
    require_literal "${script}" '"${BACKUP_DIR}/media"'
    require_literal "${script}" '"${BACKUP_DIR}/jobs"'
    require_literal "${script}" '"${BACKUP_DIR}/restore-staging"'
    require_literal "${script}" 'find "${BACKUP_DIR}" -type d -exec chmod 0700'
    require_literal "${script}" 'find "${BACKUP_DIR}" -type f -exec chmod 0600'
done

require_literal docker-compose.yml '/srv/fastsell/backups:/app/backups'
require_literal docker-compose.yml '/srv/fastsell/db/migrations:/app/migrations:ro'
require_literal docker-compose.yml '    init: true'
require_literal api/Dockerfile 'postgresql16-client tar tzdata zstd'

if rg -Fq -- '- /srv/fastsell/data:/app/data' "${REPO_ROOT}/docker-compose.yml"; then
    echo "[FAIL] API must use scoped data mounts and must not receive the raw PostgreSQL data root." >&2
    exit 1
fi

require_literal docker-compose.yml '/var/run/docker.sock:/var/run/docker.sock:ro'
require_literal docker-compose.yml 'SYSTEM_AGENT_URL: ${SYSTEM_AGENT_URL:-http://system-agent:8081}'

api_block="$(awk '
    /^  api:/ { in_api = 1 }
    /^  [A-Za-z0-9_-]+:/ && in_api && $1 != "api:" { exit }
    in_api { print }
' "${REPO_ROOT}/docker-compose.yml")"
if printf '%s\n' "${api_block}" | rg -Fq -- '/var/run/docker.sock'; then
    echo "[FAIL] The main API must not mount the Docker socket." >&2
    exit 1
fi

echo "[OK] Backup directories, secure modes, scoped mounts, PostgreSQL clients, and isolated system-agent are configured."
