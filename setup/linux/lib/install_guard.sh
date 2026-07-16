#!/usr/bin/env bash

# Read-only fresh-install checks shared with the focused setup tests.

fastsell_runtime_has_state() {
    local root="$1"
    local entry

    if [ ! -e "${root}" ] && [ ! -L "${root}" ]; then
        return 1
    fi
    if [ ! -d "${root}" ] || [ -L "${root}" ]; then
        return 0
    fi
    if declare -F as_root >/dev/null 2>&1; then
        if ! entry="$(as_root find "${root}" -mindepth 1 -print -quit 2>/dev/null)"; then
            return 0
        fi
    elif ! entry="$(find "${root}" -mindepth 1 -print -quit 2>/dev/null)"; then
        return 0
    fi
    [ -n "${entry}" ]
}

fastsell_docker_resources_exist() {
    local name
    local found

    found="$("${DOCKER_CMD[@]}" ps -a \
        --filter 'label=com.docker.compose.project=fastsell' \
        --format '{{.ID}}')"
    if [ -n "${found}" ]; then
        return 0
    fi

    for name in fastsell_web fastsell_api fastsell_system_agent fastsell_postgres; do
        if "${DOCKER_CMD[@]}" container inspect "${name}" >/dev/null 2>&1; then
            return 0
        fi
    done

    if "${DOCKER_CMD[@]}" network inspect fastsell-net >/dev/null 2>&1; then
        return 0
    fi

    found="$("${DOCKER_CMD[@]}" volume ls \
        --filter 'label=com.docker.compose.project=fastsell' \
        --format '{{.Name}}')"
    [ -n "${found}" ]
}

fastsell_print_existing_install_failure() {
    local root="$1"

    cat >&2 <<FAILURE
[FAIL] An existing or partial FastSell installation was detected at ${root}.

FastSell installation was not changed.

To update the existing installation, run:

  sudo fastsell-update

For a manual update from an extracted setup bundle, run:

  sudo bash setup/linux/update.sh

To intentionally remove FastSell and all preserved data, first create a verified
backup and use the documented destructive uninstall procedure.
FAILURE
}
