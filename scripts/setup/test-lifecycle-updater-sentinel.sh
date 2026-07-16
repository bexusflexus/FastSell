#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
TEST_ROOT="$(mktemp -d)"

cleanup() {
    rm -rf -- "${TEST_ROOT}"
}
trap cleanup EXIT

MOCK_BIN="${TEST_ROOT}/bin"
mkdir -p "${MOCK_BIN}"

cat > "${MOCK_BIN}/sudo" <<'MOCK'
#!/usr/bin/env bash
exec "$@"
MOCK

cat > "${MOCK_BIN}/docker" <<'MOCK'
#!/usr/bin/env bash
case "${1:-}" in
    ps)
        exit 0
        ;;
    compose)
        exit 0
        ;;
    info)
        exit 0
        ;;
    container|network)
        exit 1
        ;;
    volume|rm)
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
MOCK
chmod +x "${MOCK_BIN}/sudo" "${MOCK_BIN}/docker"

prepare_case() {
    local name="$1"
    CASE_ROOT="${TEST_ROOT}/${name}"
    CASE_REPO="${CASE_ROOT}/repo"
    CASE_RUNTIME="${CASE_ROOT}/srv/fastsell"
    LEGACY_SENTINEL="${CASE_ROOT}/usr/local/bin/fastsell-update"

    mkdir -p "${CASE_REPO}/setup" "$(dirname -- "${LEGACY_SENTINEL}")"
    cp -a "${REPO_ROOT}/setup/linux" "${CASE_REPO}/setup/linux"
    printf 'isolated legacy updater sentinel\n' > "${LEGACY_SENTINEL}"
    chmod 0740 "${LEGACY_SENTINEL}"
    LEGACY_HASH="$(sha256sum "${LEGACY_SENTINEL}")"
    LEGACY_MODE="$(stat -c '%a' "${LEGACY_SENTINEL}")"

    sed -i "s#^ROOT=\"/srv/fastsell\"#ROOT=\"${CASE_RUNTIME}\"#" \
        "${CASE_REPO}/setup/linux/install.sh" \
        "${CASE_REPO}/setup/linux/update.sh" \
        "${CASE_REPO}/setup/linux/uninstall.sh"
}

assert_legacy_unchanged() {
    local name="$1"
    local output="$2"

    [ -e "${LEGACY_SENTINEL}" ] || { echo "[FAIL] ${name} removed the isolated legacy sentinel" >&2; exit 1; }
    [ "$(sha256sum "${LEGACY_SENTINEL}")" = "${LEGACY_HASH}" ] || { echo "[FAIL] ${name} changed the isolated legacy sentinel contents" >&2; exit 1; }
    [ "$(stat -c '%a' "${LEGACY_SENTINEL}")" = "${LEGACY_MODE}" ] || { echo "[FAIL] ${name} changed the isolated legacy sentinel mode" >&2; exit 1; }
    [[ "${output}" != *"/usr/local/bin/fastsell-update"* ]] || { echo "[FAIL] ${name} referred to the global updater" >&2; exit 1; }
}

prepare_case install
mkdir -p "${CASE_RUNTIME}/data/images"
printf 'preserved runtime data\n' > "${CASE_RUNTIME}/data/images/sentinel"
set +e
output="$(PATH="${MOCK_BIN}:${PATH}" bash "${CASE_REPO}/setup/linux/install.sh" 2>&1)"
status=$?
set -e
[ "${status}" -ne 0 ] && [[ "${output}" == *"existing or partial FastSell installation"* ]] || { echo "[FAIL] install.sh did not stop for the isolated existing-install reason" >&2; exit 1; }
assert_legacy_unchanged "install.sh" "${output}"
echo "[OK] install.sh existing-install path leaves the legacy sentinel untouched"

prepare_case update
set +e
output="$(PATH="${MOCK_BIN}:${PATH}" bash "${CASE_REPO}/setup/linux/update.sh" 2>&1)"
status=$?
set -e
[ "${status}" -ne 0 ] && [[ "${output}" == *"does not exist. Install FastSell first"* ]] || { echo "[FAIL] update.sh did not stop for the isolated missing-install reason" >&2; exit 1; }
assert_legacy_unchanged "update.sh" "${output}"
echo "[OK] update.sh missing-install path leaves the legacy sentinel untouched"

prepare_case uninstall
set +e
output="$(PATH="${MOCK_BIN}:${PATH}" bash "${CASE_REPO}/setup/linux/uninstall.sh" 2>&1)"
status=$?
set -e
[ "${status}" -ne 0 ] && [[ "${output}" == *"Refusing to remove unexpected root"* ]] || { echo "[FAIL] uninstall.sh did not stop at the isolated destructive-root guard" >&2; exit 1; }
assert_legacy_unchanged "uninstall.sh" "${output}"
echo "[OK] uninstall.sh guarded lifecycle path leaves the legacy sentinel untouched"

echo "[OK] isolated lifecycle updater sentinel tests passed"
