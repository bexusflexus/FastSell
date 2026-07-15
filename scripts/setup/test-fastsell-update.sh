#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=setup/linux/fastsell-update
source "${REPO_ROOT}/setup/linux/fastsell-update"

TEST_ROOT="$(mktemp -d)"
cleanup_test() {
    rm -rf -- "${TEST_ROOT}"
}
trap cleanup_test EXIT

MOCK_BIN="${TEST_ROOT}/bin"
mkdir -p "${MOCK_BIN}"

cat > "${MOCK_BIN}/id" <<'MOCK'
#!/usr/bin/env bash
if [ "${1:-}" = "-u" ]; then
    if [ "${MOCK_ROOT:-true}" = true ]; then
        printf '0\n'
    else
        printf '1000\n'
    fi
    exit 0
fi
exec /usr/bin/id "$@"
MOCK

cat > "${MOCK_BIN}/curl" <<'MOCK'
#!/usr/bin/env bash
output=""
url=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --output)
            output="$2"
            shift 2
            ;;
        http://*|https://*)
            url="$1"
            shift
            ;;
        *)
            shift
            ;;
    esac
done
printf '%s\n' "${url}" >> "${MOCK_CURL_LOG}"
case "${url}" in
    https://api.github.com/*)
        [ "${MOCK_METADATA_FAIL:-false}" = true ] && exit 22
        cp "${MOCK_RELEASE_JSON}" "${output}"
        ;;
    *.tar.gz)
        [ "${MOCK_ARCHIVE_FAIL:-false}" = true ] && exit 22
        if [ "${MOCK_ARCHIVE_EMPTY:-false}" = true ]; then
            : > "${output}"
        else
            cp "${MOCK_ARCHIVE}" "${output}"
        fi
        ;;
    *.sha256)
        [ "${MOCK_CHECKSUM_FAIL:-false}" = true ] && exit 22
        cp "${MOCK_CHECKSUM}" "${output}"
        ;;
    *)
        exit 22
        ;;
esac
MOCK
chmod +x "${MOCK_BIN}/id" "${MOCK_BIN}/curl"
PATH="${MOCK_BIN}:${PATH}"
export PATH

make_case() {
    local name="$1"
    local installed="$2"
    local selected="$3"
    local archive_mode="${4:-normal}"
    local case_root="${TEST_ROOT}/cases/${name}"
    local bundle_root="${case_root}/fixture/fastsell-setup-${selected}"
    local archive="${case_root}/fastsell-setup-${selected}.tar.gz"

    rm -rf -- "${case_root}"
    mkdir -p \
        "${case_root}/runtime/config" \
        "${case_root}/runtime/compose" \
        "${case_root}/runtime/data/images" \
        "${case_root}/runtime/backups" \
        "${case_root}/tmp" \
        "${bundle_root}/setup/linux"
    printf 'FASTSELL_VERSION=%s\nPRESERVE_VALUE=unchanged\n' "${installed}" > "${case_root}/runtime/config/.env"
    printf 'services: {}\n' > "${case_root}/runtime/compose/docker-compose.yml"
    printf 'runtime-data' > "${case_root}/runtime/data/images/sentinel"
    printf 'backup-data' > "${case_root}/runtime/backups/sentinel"
    printf 'example\n' > "${bundle_root}/.env.example"
    printf 'services: {}\n' > "${bundle_root}/docker-compose.yml"
    cat > "${bundle_root}/setup/linux/fastsell-update" <<'UPDATER'
#!/usr/bin/env bash
printf 'fixture updater\n'
UPDATER
    chmod +x "${bundle_root}/setup/linux/fastsell-update"
    cat > "${bundle_root}/setup/linux/update.sh" <<'UPDATE'
#!/usr/bin/env bash
set -euo pipefail
printf 'called\n' > "${MOCK_UPDATE_CALLED}"
if [ "${MOCK_UPDATE_FAIL:-false}" = true ]; then
    exit 7
fi
install -m 0755 "$(dirname -- "${BASH_SOURCE[0]}")/fastsell-update" "${UPDATE_COMMAND}"
UPDATE
    chmod +x "${bundle_root}/setup/linux/update.sh"

    case "${archive_mode}" in
        normal)
            tar -C "${case_root}/fixture" -czf "${archive}" "fastsell-setup-${selected}"
            ;;
        missing-update)
            rm -- "${bundle_root}/setup/linux/update.sh"
            tar -C "${case_root}/fixture" -czf "${archive}" "fastsell-setup-${selected}"
            ;;
        unsafe)
            python3 - "${case_root}/fixture" "${archive}" "fastsell-setup-${selected}" <<'PY'
import io
import os
import sys
import tarfile

fixture, archive, top = sys.argv[1:]
with tarfile.open(archive, "w:gz") as output:
    output.add(os.path.join(fixture, top), arcname=top)
    entry = tarfile.TarInfo("../outside")
    payload = b"unsafe"
    entry.size = len(payload)
    output.addfile(entry, io.BytesIO(payload))
PY
            ;;
        *)
            echo "[FAIL] unknown fixture archive mode ${archive_mode}" >&2
            exit 1
            ;;
    esac

    printf '{"tag_name":"%s","draft":false,"prerelease":false}\n' "${selected}" > "${case_root}/release.json"
    (
        cd "${case_root}"
        sha256sum "$(basename -- "${archive}")" > "release.sha256"
    )

    ROOT="${case_root}/runtime"
    ENV_FILE="${ROOT}/config/.env"
    COMPOSE_FILE="${ROOT}/compose/docker-compose.yml"
    UPDATE_COMMAND="${case_root}/installed/fastsell-update"
    mkdir -p "$(dirname -- "${UPDATE_COMMAND}")"
    TMPDIR="${case_root}/tmp"
    MOCK_RELEASE_JSON="${case_root}/release.json"
    MOCK_ARCHIVE="${archive}"
    MOCK_CHECKSUM="${case_root}/release.sha256"
    MOCK_CURL_LOG="${case_root}/curl.log"
    MOCK_UPDATE_CALLED="${case_root}/update.called"
    export ROOT ENV_FILE COMPOSE_FILE UPDATE_COMMAND TMPDIR
    export MOCK_RELEASE_JSON MOCK_ARCHIVE MOCK_CHECKSUM MOCK_CURL_LOG MOCK_UPDATE_CALLED
    unset MOCK_METADATA_FAIL MOCK_ARCHIVE_FAIL MOCK_ARCHIVE_EMPTY MOCK_CHECKSUM_FAIL MOCK_UPDATE_FAIL
    MOCK_ROOT=true
    export MOCK_ROOT
    CASE_ROOT="${case_root}"
}

run_updater() {
    local input="$1"
    shift
    set +e
    RUN_OUTPUT="$(printf '%b' "${input}" | (fastsell_update_main "$@") 2>&1)"
    RUN_STATUS=$?
    set -e
}

assert_status() {
    local expected="$1"
    local name="$2"
    if [ "${RUN_STATUS}" -ne "${expected}" ]; then
        echo "[FAIL] ${name}: expected status ${expected}, got ${RUN_STATUS}" >&2
        echo "${RUN_OUTPUT}" >&2
        exit 1
    fi
    echo "[OK] ${name}"
}

assert_clean_temp() {
    if [ -n "$(find "${TMPDIR}" -mindepth 1 -print -quit)" ]; then
        echo "[FAIL] temporary files were not cleaned: ${TMPDIR}" >&2
        exit 1
    fi
}

make_case root-required v0.1.3 v0.1.4
MOCK_ROOT=false
export MOCK_ROOT
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] || { echo "[FAIL] updater allowed non-root execution" >&2; exit 1; }
[[ "${RUN_OUTPUT}" == *"root privileges are required"* ]] || { echo "[FAIL] root guidance missing" >&2; exit 1; }
echo "[OK] root requirement"

make_case missing-install v0.1.3 v0.1.4
rm -- "${ENV_FILE}"
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] || { echo "[FAIL] updater accepted missing installation" >&2; exit 1; }
[[ "${RUN_OUTPUT}" == *"FastSell is not installed"* ]] || { echo "[FAIL] missing-install message absent" >&2; exit 1; }
echo "[OK] missing installation"

make_case latest-success v0.1.3 v0.1.4
env_before="$(sha256sum "${ENV_FILE}")"
data_before="$(sha256sum "${ROOT}/data/images/sentinel")"
backup_before="$(sha256sum "${ROOT}/backups/sentinel")"
run_updater "" --yes
assert_status 0 "valid latest stable release and --yes"
rg -Fq '/releases/latest' "${MOCK_CURL_LOG}" || { echo "[FAIL] latest update did not use the stable latest-release endpoint" >&2; exit 1; }
[ -f "${MOCK_UPDATE_CALLED}" ] || { echo "[FAIL] downloaded update.sh did not run" >&2; exit 1; }
cmp -s "${MOCK_ARCHIVE%/*}/fixture/fastsell-setup-v0.1.4/setup/linux/fastsell-update" "${UPDATE_COMMAND}" || {
    echo "[FAIL] updater was not refreshed after success" >&2
    exit 1
}
[ "$(sha256sum "${ENV_FILE}")" = "${env_before}" ] || { echo "[FAIL] .env changed" >&2; exit 1; }
[ "$(sha256sum "${ROOT}/data/images/sentinel")" = "${data_before}" ] || { echo "[FAIL] runtime data changed" >&2; exit 1; }
[ "$(sha256sum "${ROOT}/backups/sentinel")" = "${backup_before}" ] || { echo "[FAIL] backup data changed" >&2; exit 1; }
assert_clean_temp
echo "[OK] updater refresh and runtime preservation"

make_case draft v0.1.3 v0.1.4
printf '{"tag_name":"v0.1.4","draft":true,"prerelease":false}\n' > "${MOCK_RELEASE_JSON}"
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"draft releases"* ]] || { echo "[FAIL] draft release accepted" >&2; exit 1; }
assert_clean_temp
echo "[OK] drafts ignored"

make_case prerelease v0.1.3 v0.1.4
printf '{"tag_name":"v0.1.4","draft":false,"prerelease":true}\n' > "${MOCK_RELEASE_JSON}"
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"prereleases"* ]] || { echo "[FAIL] prerelease accepted" >&2; exit 1; }
assert_clean_temp
echo "[OK] prereleases ignored"

make_case malformed-version v0.1.3 v0.1.4
run_updater "" --version candidate-main --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"exact vX.Y.Z"* ]] || { echo "[FAIL] malformed exact version accepted" >&2; exit 1; }
echo "[OK] exact version parsing"

make_case exact-version v0.1.3 v0.1.4
run_updater "" --version v0.1.4 --yes
assert_status 0 "valid exact version"
rg -Fq '/releases/tags/v0.1.4' "${MOCK_CURL_LOG}" || { echo "[FAIL] exact update did not use the exact stable release endpoint" >&2; exit 1; }

make_case already-current v0.1.4 v0.1.4
run_updater "" --yes
assert_status 0 "already-current exit"
[[ "${RUN_OUTPUT}" == *"already current"* ]] || { echo "[FAIL] already-current message missing" >&2; exit 1; }
[ ! -e "${MOCK_UPDATE_CALLED}" ] || { echo "[FAIL] already-current path ran update.sh" >&2; exit 1; }
if rg -n '\.tar\.gz$|\.sha256$' "${MOCK_CURL_LOG}" >/dev/null; then
    echo "[FAIL] already-current path downloaded release assets" >&2
    exit 1
fi
assert_clean_temp

make_case missing-asset v0.1.3 v0.1.4
MOCK_ARCHIVE_FAIL=true
export MOCK_ARCHIVE_FAIL
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"archive could not be downloaded"* ]] || { echo "[FAIL] missing release asset not rejected" >&2; exit 1; }
assert_clean_temp
echo "[OK] missing release asset"

make_case missing-checksum-asset v0.1.3 v0.1.4
MOCK_CHECKSUM_FAIL=true
export MOCK_CHECKSUM_FAIL
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"checksum file could not be downloaded"* ]] || { echo "[FAIL] missing checksum asset not rejected" >&2; exit 1; }
assert_clean_temp
echo "[OK] missing checksum release asset"

make_case failed-download v0.1.3 v0.1.4
MOCK_ARCHIVE_EMPTY=true
export MOCK_ARCHIVE_EMPTY
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"download was incomplete"* ]] || { echo "[FAIL] incomplete download not rejected" >&2; exit 1; }
assert_clean_temp
echo "[OK] failed/incomplete download"

make_case checksum-mismatch v0.1.3 v0.1.4
env_before="$(sha256sum "${ENV_FILE}")"
data_before="$(sha256sum "${ROOT}/data/images/sentinel")"
backup_before="$(sha256sum "${ROOT}/backups/sentinel")"
printf '%064d  fastsell-setup-v0.1.4.tar.gz\n' 0 > "${MOCK_CHECKSUM}"
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"checksum did not match"* ]] || { echo "[FAIL] checksum mismatch accepted" >&2; exit 1; }
[ "$(sha256sum "${ENV_FILE}")" = "${env_before}" ] || { echo "[FAIL] checksum failure changed .env" >&2; exit 1; }
[ "$(sha256sum "${ROOT}/data/images/sentinel")" = "${data_before}" ] || { echo "[FAIL] checksum failure changed runtime data" >&2; exit 1; }
[ "$(sha256sum "${ROOT}/backups/sentinel")" = "${backup_before}" ] || { echo "[FAIL] checksum failure changed backups" >&2; exit 1; }
[ ! -e "${MOCK_UPDATE_CALLED}" ] || { echo "[FAIL] checksum failure ran update.sh" >&2; exit 1; }
assert_clean_temp
echo "[OK] checksum mismatch"

make_case malformed-checksum v0.1.3 v0.1.4
printf 'not-a-checksum fastsell-setup-v0.1.4.tar.gz\n' > "${MOCK_CHECKSUM}"
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"checksum file is malformed"* ]] || { echo "[FAIL] malformed checksum accepted" >&2; exit 1; }
assert_clean_temp
echo "[OK] malformed checksum"

make_case unsafe-archive v0.1.3 v0.1.4 unsafe
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"unsafe path"* || "${RUN_OUTPUT}" == *"unexpected top-level"* ]] || { echo "[FAIL] unsafe archive accepted" >&2; exit 1; }
assert_clean_temp
echo "[OK] unsafe archive path"

make_case missing-update-sh v0.1.3 v0.1.4 missing-update
run_updater "" --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"missing required file setup/linux/update.sh"* ]] || { echo "[FAIL] archive missing update.sh accepted" >&2; exit 1; }
assert_clean_temp
echo "[OK] missing update.sh"

make_case confirmation-refusal v0.1.3 v0.1.4
run_updater "n\n"
assert_status 0 "confirmation refusal"
[ ! -e "${MOCK_UPDATE_CALLED}" ] || { echo "[FAIL] declined update ran update.sh" >&2; exit 1; }
[[ "${RUN_OUTPUT}" == *"Update cancelled"* ]] || { echo "[FAIL] cancellation message missing" >&2; exit 1; }
assert_clean_temp

make_case update-failure v0.1.3 v0.1.4
MOCK_UPDATE_FAIL=true
export MOCK_UPDATE_FAIL
run_updater "" --yes
assert_status 7 "update.sh failure propagation"
[ ! -e "${UPDATE_COMMAND}" ] || { echo "[FAIL] updater refreshed after failed update.sh" >&2; exit 1; }
assert_clean_temp

make_case rollback-protection v0.2.0 v0.1.4
run_updater "" --version v0.1.4 --yes
[ "${RUN_STATUS}" -ne 0 ] && [[ "${RUN_OUTPUT}" == *"Rollback may be unsafe"* ]] && [[ "${RUN_OUTPUT}" == *"--allow-rollback"* ]] || {
    echo "[FAIL] rollback warning/protection missing" >&2
    exit 1
}
[ ! -e "${MOCK_UPDATE_CALLED}" ] || { echo "[FAIL] protected rollback ran update.sh" >&2; exit 1; }
assert_clean_temp
echo "[OK] rollback warning and explicit protection"

make_case rollback-explicit v0.2.0 v0.1.4
run_updater "" --version v0.1.4 --allow-rollback --yes
assert_status 0 "explicit rollback option"

echo "[OK] all fastsell-update tests passed"
