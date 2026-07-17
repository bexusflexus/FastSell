#!/usr/bin/env bash

# Private helpers shared by release scripts. Operator-facing entry points must
# define fail() before sourcing this file.

release_origin_repository() {
    local origin_url
    local repository=""

    if ! origin_url="$(git config --get remote.origin.url 2>/dev/null)"; then
        return 1
    fi
    case "${origin_url}" in
        git@github.com:*) repository="${origin_url#git@github.com:}" ;;
        https://github.com/*) repository="${origin_url#https://github.com/}" ;;
        ssh://git@github.com/*) repository="${origin_url#ssh://git@github.com/}" ;;
        *) return 1 ;;
    esac
    repository="${repository%.git}"
    case "${repository}" in
        */*) ;;
        *) return 1 ;;
    esac
    [ -n "${repository%%/*}" ] && [ -n "${repository#*/}" ] || return 1
    [ "${repository#*/}" = "${repository##*/}" ] || return 1
    printf '%s\n' "${repository}"
}

release_validate_poll_settings() {
    local attempts="$1"
    local sleep_seconds="$2"

    [[ "${attempts}" =~ ^[0-9]+$ ]] && [ "${attempts}" -gt 0 ] || fail "Check polling attempts must be a positive integer."
    [[ "${sleep_seconds}" =~ ^[0-9]+$ ]] || fail "Check polling interval must be a nonnegative integer."
}

release_required_checks_json() {
    local repository="$1"
    local base_branch="$2"
    local protection_json
    local required_json

    if ! protection_json="$(gh api "repos/${repository}/branches/${base_branch}/protection/required_status_checks" 2>&1)"; then
        fail "Could not load required checks for ${repository}:${base_branch}: ${protection_json}"
    fi
    if ! required_json="$(jq -ce '
        [
          (.contexts[]?),
          (.checks[]? | .context)
        ]
        | map(select(type == "string" and length > 0))
        | unique
        | if length > 0 then . else error("required check set is empty") end
    ' <<<"${protection_json}" 2>&1)"; then
        fail "Required-check configuration for ${repository}:${base_branch} is invalid or empty: ${required_json}"
    fi
    printf '%s\n' "${required_json}"
}

release_wait_for_required_checks() {
    local repository="$1"
    local base_branch="$2"
    local pr_number="$3"
    local attempts="${FASTSELL_RELEASE_CHECK_ATTEMPTS:-180}"
    local sleep_seconds="${FASTSELL_RELEASE_CHECK_SLEEP_SECONDS:-10}"
    local required_json
    local required_count
    local checks_json
    local matches_json
    local match_count
    local name
    local state
    local bucket
    local missing
    local pending
    local index
    local attempt

    release_validate_poll_settings "${attempts}" "${sleep_seconds}"
    if ! required_json="$(release_required_checks_json "${repository}" "${base_branch}")"; then
        return 1
    fi
    if ! required_count="$(jq -er 'length' <<<"${required_json}" 2>&1)"; then
        fail "Could not count required checks: ${required_count}"
    fi

    attempt=1
    while [ "${attempt}" -le "${attempts}" ]; do
        if ! checks_json="$(gh pr checks "${pr_number}" \
            --repo "${repository}" \
            --json name,state,bucket,workflow 2>&1)"; then
            fail "Could not load checks for PR #${pr_number}: ${checks_json}"
        fi
        if ! jq -e 'type == "array"' <<<"${checks_json}" >/dev/null 2>&1; then
            fail "Checks for PR #${pr_number} were not returned as a JSON array."
        fi

        missing=""
        pending=""
        index=0
        while [ "${index}" -lt "${required_count}" ]; do
            if ! name="$(jq -er --argjson index "${index}" '.[$index]' <<<"${required_json}" 2>&1)"; then
                fail "Could not read required check ${index}: ${name}"
            fi
            if ! matches_json="$(jq -ce --arg name "${name}" '[.[] | select(.name == $name)]' <<<"${checks_json}" 2>&1)"; then
                fail "Could not inspect required check ${name}: ${matches_json}"
            fi
            if ! match_count="$(jq -er 'length' <<<"${matches_json}" 2>&1)"; then
                fail "Could not count check results for ${name}: ${match_count}"
            fi
            if [ "${match_count}" -eq 0 ]; then
                missing="${missing}${missing:+, }${name}"
                index=$((index + 1))
                continue
            fi
            [ "${match_count}" -eq 1 ] || fail "Required check ${name} has ${match_count} current results; refusing an ambiguous check set."
            if ! state="$(jq -er '.[0].state' <<<"${matches_json}" 2>&1)"; then
                fail "Required check ${name} has no state: ${state}"
            fi
            if ! bucket="$(jq -er '.[0].bucket' <<<"${matches_json}" 2>&1)"; then
                fail "Required check ${name} has no bucket: ${bucket}"
            fi

            case "${state}:${bucket}" in
                SUCCESS:pass) ;;
                EXPECTED:pending|REQUESTED:pending|WAITING:pending|QUEUED:pending|PENDING:pending|IN_PROGRESS:pending)
                    pending="${pending}${pending:+, }${name} (${state})"
                    ;;
                FAILURE:*|ERROR:*|CANCELLED:*|SKIPPED:*|NEUTRAL:*|TIMED_OUT:*|ACTION_REQUIRED:*|STARTUP_FAILURE:*|STALE:*)
                    fail "Required check ${name} ended in disallowed state ${state} (${bucket}); PR #${pr_number} was not merged."
                    ;;
                *)
                    fail "Required check ${name} returned unknown or inconsistent state ${state} (${bucket}); PR #${pr_number} was not merged."
                    ;;
            esac
            index=$((index + 1))
        done

        if [ -z "${missing}" ] && [ -z "${pending}" ]; then
            echo "[OK] Every required check explicitly passed for PR #${pr_number}."
            return 0
        fi
        if [ "${attempt}" -eq "${attempts}" ]; then
            fail "Timed out after ${attempts} check polls for PR #${pr_number}. Missing: ${missing:-none}. Pending: ${pending:-none}. Inspect with: gh pr checks ${pr_number} --repo ${repository}"
        fi
        echo "[OK] Required checks are incomplete for PR #${pr_number} (${attempt}/${attempts}). Missing: ${missing:-none}. Pending: ${pending:-none}."
        sleep "${sleep_seconds}"
        attempt=$((attempt + 1))
    done
    fail "Internal error: required-check polling ended unexpectedly."
}
