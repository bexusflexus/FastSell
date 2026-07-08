#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  install_candidate.sh <full-sha>

Optional staging environment variables:
  FASTSELL_STAGING_HOST          Remote staging host
  FASTSELL_STAGING_USER          Remote SSH user
  FASTSELL_STAGING_PATH          Remote destination path
  FASTSELL_STAGING_INSTALL_MODE  update or install, default update
USAGE
}

require_cmd() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        echo "[FAIL] Required command is missing: ${cmd}" >&2
        exit 1
    fi
}

repo_root() {
    git rev-parse --show-toplevel
}

validate_sha() {
    local sha="$1"
    if [[ ! "${sha}" =~ ^[0-9a-f]{40}$ ]]; then
        echo "[FAIL] Expected a full 40-character lowercase git SHA." >&2
        exit 1
    fi
}

artifact_run_id() {
    local sha="$1"

    gh run list \
        --workflow publish-images.yml \
        --branch main \
        --limit 100 \
        --json databaseId,headSha,conclusion \
        --jq ".[] | select(.headSha == \"${sha}\" and .conclusion == \"success\") | .databaseId" \
        | awk 'NR == 1 { print }'
}

print_manifest_summary() {
    local manifest="$1"

    echo "[OK] Candidate manifest: ${manifest}"
    sed -n '1,220p' "${manifest}"
}

remote_target() {
    if [ -n "${FASTSELL_STAGING_USER:-}" ]; then
        printf '%s@%s' "${FASTSELL_STAGING_USER}" "${FASTSELL_STAGING_HOST}"
    else
        printf '%s' "${FASTSELL_STAGING_HOST}"
    fi
}

maybe_install_remote() {
    local sha="$1"
    local tarball="$2"
    local manifest="$3"
    local bundle_dir="$4"

    if [ -z "${FASTSELL_STAGING_HOST:-}" ]; then
        cat <<NEXT
[OK] FASTSELL_STAGING_HOST is not set, so no remote staging install was attempted.
[OK] Manual next steps:
     1. Copy ${tarball} to your staging host.
     2. Extract it.
     3. Run sudo bash ${bundle_dir}/setup/linux/update.sh for an existing install,
        or sudo bash ${bundle_dir}/setup/linux/install.sh for a fresh staging install.
NEXT
        return
    fi

    require_cmd ssh
    require_cmd scp

    local mode
    local remote_path
    local target
    local answer
    mode="${FASTSELL_STAGING_INSTALL_MODE:-update}"
    remote_path="${FASTSELL_STAGING_PATH:-~/fastsell-candidate-${sha:0:12}}"
    target="$(remote_target)"

    case "${mode}" in
        update|install) ;;
        *) echo "[FAIL] FASTSELL_STAGING_INSTALL_MODE must be update or install." >&2; exit 1 ;;
    esac

    case "${remote_path}" in
        *[!A-Za-z0-9._~/-]*)
            echo "[FAIL] FASTSELL_STAGING_PATH may only contain letters, numbers, dots, underscores, dashes, slashes, and ~." >&2
            exit 1
            ;;
    esac

    echo "[OK] Remote staging target: ${target}:${remote_path}"
    read -r -p "Copy and run candidate ${mode} on the staging host? [y/N] " answer
    case "${answer}" in
        y|Y|yes|YES) ;;
        *) echo "[OK] Remote staging install skipped."; return ;;
    esac

    ssh "${target}" "mkdir -p ${remote_path}"
    scp "${tarball}" "${manifest}" "${target}:${remote_path}/"
    ssh "${target}" "cd ${remote_path} && tar -xzf $(basename -- "${tarball}")"
    ssh "${target}" "cd ${remote_path}/${bundle_dir} && sudo bash setup/linux/${mode}.sh"
}

main() {
    if [ "$#" -ne 1 ]; then
        usage >&2
        exit 1
    fi

    require_cmd git
    require_cmd gh
    require_cmd tar
    require_cmd awk

    local sha="$1"
    validate_sha "${sha}"

    cd "$(repo_root)"

    local run_id
    run_id="$(artifact_run_id "${sha}")"
    if [ -z "${run_id}" ]; then
        echo "[FAIL] Could not find a successful Publish Images workflow run for ${sha}." >&2
        echo "       Check GitHub Actions, then retry after candidate artifacts are uploaded." >&2
        exit 1
    fi

    local work_dir
    local artifact_dir
    local extract_dir
    local bundle_name
    local tarball
    local manifest
    work_dir="dist/release-candidates/${sha}"
    artifact_dir="${work_dir}/artifact"
    extract_dir="${work_dir}/extracted"
    bundle_name="fastsell-setup-candidate-${sha}"
    tarball="${artifact_dir}/${bundle_name}.tar.gz"
    manifest="${artifact_dir}/fastsell-release-candidate-${sha}.json"

    rm -rf "${work_dir}"
    mkdir -p "${artifact_dir}" "${extract_dir}"

    echo "[OK] Downloading candidate artifact from workflow run ${run_id}"
    gh run download "${run_id}" \
        --name "fastsell-candidate-${sha}" \
        --dir "${artifact_dir}"

    test -f "${tarball}" || { echo "[FAIL] Missing ${tarball}" >&2; exit 1; }
    test -f "${manifest}" || { echo "[FAIL] Missing ${manifest}" >&2; exit 1; }

    tar -xzf "${tarball}" -C "${extract_dir}"

    print_manifest_summary "${manifest}"

    echo "[OK] Extracted bundle: ${extract_dir}/${bundle_name}"
    maybe_install_remote "${sha}" "${tarball}" "${manifest}" "${bundle_name}"
}

main "$@"
