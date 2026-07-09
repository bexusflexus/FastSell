#!/usr/bin/env bash
set -euo pipefail

SHA=""
YES=0
FASTSELL_GITHUB_REPO="${FASTSELL_GITHUB_REPO:-bexusflexus/FastSell}"

usage() {
    cat <<'USAGE'
Usage:
  fetch_candidate_bundle.sh <full-sha> [--yes]

Run this from an existing FastSell setup workspace:
  cd <setup-workspace>/dev_only
  ./fetch_candidate_bundle.sh <full-sha>

The script downloads and applies candidate setup-bundle files to <setup-workspace>.
It does not run setup/linux/update.sh.
USAGE
}

require_cmd() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        echo "[FAIL] Required command is missing: ${cmd}" >&2
        exit 1
    fi
}

confirm() {
    local prompt="$1"
    local answer

    if [ "${YES}" -eq 1 ]; then
        return
    fi

    read -r -p "${prompt} [y/N] " answer
    case "${answer}" in
        y|Y|yes|YES) ;;
        *) echo "[FAIL] Cancelled."; exit 1 ;;
    esac
}

validate_sha() {
    local sha="$1"

    if [[ ! "${sha}" =~ ^[0-9a-f]{40}$ ]]; then
        echo "[FAIL] Expected a full 40-character lowercase git SHA." >&2
        exit 1
    fi
}

parse_args() {
    if [ "$#" -lt 1 ]; then
        usage >&2
        exit 1
    fi

    SHA="$1"
    shift

    while [ "$#" -gt 0 ]; do
        case "$1" in
            --yes|-y)
                YES=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                echo "[FAIL] Unknown argument: $1" >&2
                usage >&2
                exit 1
                ;;
        esac
    done
}

script_dir() {
    cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P
}

validate_setup_workspace() {
    local dev_only_dir="$1"
    local setup_workspace="$2"

    if [ "$(basename -- "${BASH_SOURCE[0]}")" != "fetch_candidate_bundle.sh" ]; then
        echo "[FAIL] This script must be named <setup-workspace>/dev_only/fetch_candidate_bundle.sh." >&2
        exit 1
    fi

    if [ "$(basename -- "${dev_only_dir}")" != "dev_only" ]; then
        echo "[FAIL] This script must live at <setup-workspace>/dev_only/fetch_candidate_bundle.sh." >&2
        echo "       Current directory: ${dev_only_dir}" >&2
        exit 1
    fi

    if [ ! -f "${setup_workspace}/docker-compose.yml" ] ||
        [ ! -f "${setup_workspace}/.env.example" ] ||
        [ ! -f "${setup_workspace}/setup/linux/update.sh" ]; then
        echo "[FAIL] Parent directory does not look like a FastSell setup workspace: ${setup_workspace}" >&2
        echo "       Expected docker-compose.yml, .env.example, and setup/linux/update.sh." >&2
        exit 1
    fi
}

run_info_for_sha() {
    local sha="$1"

    gh run list \
        --repo "${FASTSELL_GITHUB_REPO}" \
        --workflow publish-images.yml \
        --branch main \
        --limit 100 \
        --json databaseId,headSha,status,conclusion \
        --jq "map(select(.headSha == \"${sha}\")) | first | if . == null then \"\" else [.databaseId, .status, (.conclusion // \"\")] | @tsv end"
}

resolve_successful_run_id() {
    local sha="$1"
    local run_info
    local run_id
    local run_status
    local run_conclusion

    run_info="$(run_info_for_sha "${sha}")"
    if [ -z "${run_info}" ]; then
        echo "[FAIL] Could not find a Publish Images workflow run on main for ${sha}." >&2
        exit 1
    fi

    IFS=$'\t' read -r run_id run_status run_conclusion <<< "${run_info}"
    case "${run_status}" in
        queued|in_progress|waiting|requested|pending)
            echo "[OK] Publish Images run ${run_id} is ${run_status}." >&2
            confirm "Watch this run with gh run watch?"
            gh run watch "${run_id}" --repo "${FASTSELL_GITHUB_REPO}" >&2
            run_info="$(run_info_for_sha "${sha}")"
            IFS=$'\t' read -r run_id run_status run_conclusion <<< "${run_info}"
            ;;
    esac

    if [ "${run_status}" != "completed" ] || [ "${run_conclusion}" != "success" ]; then
        echo "[FAIL] Publish Images run is not successful for ${sha}." >&2
        echo "       run: ${run_id}" >&2
        echo "       status: ${run_status}" >&2
        echo "       conclusion: ${run_conclusion:-none}" >&2
        exit 1
    fi

    printf '%s' "${run_id}"
}

prepare_candidate_dirs() {
    local candidate_dir="$1"

    if [ -e "${candidate_dir}" ]; then
        confirm "Replace existing candidate cache ${candidate_dir}?"
        rm -rf -- "${candidate_dir}"
    fi

    mkdir -p "${candidate_dir}/artifact" "${candidate_dir}/extracted"
}

validate_extracted_bundle() {
    local bundle_dir="$1"

    if [ ! -f "${bundle_dir}/docker-compose.yml" ] ||
        [ ! -f "${bundle_dir}/.env.example" ] ||
        [ ! -f "${bundle_dir}/setup/linux/install.sh" ] ||
        [ ! -f "${bundle_dir}/setup/linux/update.sh" ] ||
        [ ! -f "${bundle_dir}/setup/linux/uninstall.sh" ] ||
        [ ! -d "${bundle_dir}/db/migrations" ]; then
        echo "[FAIL] Extracted candidate bundle does not have the expected setup-bundle layout: ${bundle_dir}" >&2
        exit 1
    fi
}

print_manifest_and_refs() {
    local manifest="$1"

    echo "[OK] Candidate manifest: ${manifest}"
    sed -n '1,220p' "${manifest}"
    echo "[OK] Candidate image refs:"
    rg -n '"ref":' "${manifest}" || true
}

apply_candidate_bundle() {
    local bundle_dir="$1"
    local setup_workspace="$2"

    echo "[OK] Candidate bundle source: ${bundle_dir}"
    echo "[OK] Setup workspace destination: ${setup_workspace}"
    confirm "Apply candidate setup-bundle files to this setup workspace?"

    rsync -a \
        --exclude '/.env' \
        --exclude '/dev_only/' \
        "${bundle_dir}/" \
        "${setup_workspace}/"
}

verify_applied_candidate() {
    local setup_workspace="$1"
    local sha="$2"

    if ! rg -q "api-sha-${sha}" "${setup_workspace}/.env.example"; then
        echo "[FAIL] Setup workspace .env.example does not contain api-sha-${sha}." >&2
        exit 1
    fi
    if ! rg -q "web-sha-${sha}" "${setup_workspace}/.env.example"; then
        echo "[FAIL] Setup workspace .env.example does not contain web-sha-${sha}." >&2
        exit 1
    fi
    if ! rg -q "system-agent-sha-${sha}" "${setup_workspace}/.env.example"; then
        echo "[FAIL] Setup workspace .env.example does not contain system-agent-sha-${sha}." >&2
        exit 1
    fi

    if ! rg -q 'FASTSELL_API_IMAGE' "${setup_workspace}/docker-compose.yml" ||
        ! rg -q 'FASTSELL_WEB_IMAGE' "${setup_workspace}/docker-compose.yml" ||
        ! rg -q 'FASTSELL_SYSTEM_AGENT_IMAGE' "${setup_workspace}/docker-compose.yml"; then
        echo "[FAIL] Setup workspace docker-compose.yml does not reference the expected FASTSELL_*_IMAGE variables." >&2
        exit 1
    fi

    if [ ! -f "${setup_workspace}/setup/linux/update.sh" ]; then
        echo "[FAIL] Setup workspace setup/linux/update.sh is missing after applying candidate bundle." >&2
        exit 1
    fi
}

main() {
    parse_args "$@"
    validate_sha "${SHA}"
    require_cmd gh
    require_cmd tar
    require_cmd rg
    require_cmd rsync
    require_cmd sed

    local dev_only_dir
    local setup_workspace
    dev_only_dir="$(script_dir)"
    setup_workspace="$(cd -- "${dev_only_dir}/.." && pwd -P)"
    validate_setup_workspace "${dev_only_dir}" "${setup_workspace}"

    local run_id
    run_id="$(resolve_successful_run_id "${SHA}")"

    local candidate_dir
    local artifact_dir
    local extract_dir
    local bundle_name
    local tarball
    local manifest
    local bundle_dir
    candidate_dir="${dev_only_dir}/candidates/${SHA}"
    artifact_dir="${candidate_dir}/artifact"
    extract_dir="${candidate_dir}/extracted"
    bundle_name="fastsell-setup-candidate-${SHA}"
    tarball="${artifact_dir}/${bundle_name}.tar.gz"
    manifest="${artifact_dir}/fastsell-release-candidate-${SHA}.json"
    bundle_dir="${extract_dir}/${bundle_name}"

    prepare_candidate_dirs "${candidate_dir}"

    echo "[OK] Downloading candidate artifact fastsell-candidate-${SHA} from workflow run ${run_id}"
    gh run download "${run_id}" \
        --repo "${FASTSELL_GITHUB_REPO}" \
        --name "fastsell-candidate-${SHA}" \
        --dir "${artifact_dir}"

    test -f "${tarball}" || { echo "[FAIL] Missing expected candidate tarball: ${tarball}" >&2; exit 1; }
    test -f "${manifest}" || { echo "[FAIL] Missing expected candidate manifest: ${manifest}" >&2; exit 1; }

    tar -xzf "${tarball}" -C "${extract_dir}"
    validate_extracted_bundle "${bundle_dir}"
    print_manifest_and_refs "${manifest}"
    apply_candidate_bundle "${bundle_dir}" "${setup_workspace}"
    verify_applied_candidate "${setup_workspace}" "${SHA}"

    cat <<NEXT
[OK] Candidate files were applied to ${setup_workspace}.
[OK] Next step:
     cd ${setup_workspace}
     sudo bash setup/linux/update.sh
NEXT
}

main "$@"
