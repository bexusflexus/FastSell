#!/usr/bin/env bash
set -euo pipefail

SOURCE_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "${TEST_ROOT}"' EXIT

CASE_ROOT=""
WORK=""
ORIGIN=""
STATE=""
MOCK_BIN=""
CANDIDATE=""
RUN_OUTPUT=""
RUN_STATUS=0
REAL_GIT="$(command -v git)"

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

assert_contains() {
    local expected="$1"
    [[ "${RUN_OUTPUT}" == *"${expected}"* ]] || fail "output did not contain: ${expected}\n${RUN_OUTPUT}"
}

assert_not_contains() {
    local unexpected="$1"
    [[ "${RUN_OUTPUT}" != *"${unexpected}"* ]] || fail "output unexpectedly contained: ${unexpected}\n${RUN_OUTPUT}"
}

assert_success() {
    [ "${RUN_STATUS}" -eq 0 ] || fail "status ${RUN_STATUS}, expected success\n${RUN_OUTPUT}"
}

assert_failure() {
    [ "${RUN_STATUS}" -ne 0 ] || fail "command unexpectedly succeeded\n${RUN_OUTPUT}"
}

assert_clean() {
    local status_output
    if ! status_output="$(git -C "${WORK}" status --porcelain)"; then fail "could not inspect fixture"; fi
    [ -z "${status_output}" ] || fail "fixture worktree is dirty: ${status_output}"
}

state_count() {
    local name="$1"
    if [ -f "${STATE}/${name}" ]; then cat "${STATE}/${name}"; else echo 0; fi
}

write_git_mock() {
    cat >"${MOCK_BIN}/git" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [ -f "${MOCK_GH_STATE}/fail_git_status" ] && [ "${1:-}" = "status" ]; then
    echo "injected git status failure" >&2
    exit 71
fi
if [ -f "${MOCK_GH_STATE}/fail_patch_id" ] && [ "${1:-}" = "patch-id" ]; then
    echo "injected patch-id failure" >&2
    exit 72
fi
exec "${MOCK_REAL_GIT}" "$@"
MOCK
    chmod +x "${MOCK_BIN}/git"
}

write_gh_mock() {
    cat >"${MOCK_BIN}/gh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail

echo "$*" >>"${MOCK_GH_STATE}/calls"

die() { echo "mock gh rejected invocation: $*" >&2; exit 90; }

increment() {
    local file="${MOCK_GH_STATE}/$1"
    local value=0
    if [ -f "${file}" ]; then value="$(cat "${file}")"; fi
    echo $((value + 1)) >"${file}"
}

has_arg() {
    local target="$1"
    shift
    local arg
    for arg in "$@"; do [ "${arg}" != "${target}" ] || return 0; done
    return 1
}

arg_after() {
    local target="$1"
    shift
    while [ "$#" -gt 0 ]; do
        if [ "$1" = "${target}" ] && [ "$#" -ge 2 ]; then printf '%s\n' "$2"; return 0; fi
        shift
    done
    return 1
}

require_pair() {
    local flag="$1"
    local expected="$2"
    shift 2
    local actual
    if ! actual="$(arg_after "${flag}" "$@")"; then die "missing ${flag}"; fi
    [ "${actual}" = "${expected}" ] || die "${flag} expected ${expected}, got ${actual}"
}

pr_json() {
    local number state head base base_oid owner cross draft oid merge title url
    number="$(cat "${MOCK_GH_STATE}/pr_number")"
    state="$(cat "${MOCK_GH_STATE}/pr_state")"
    head="$(cat "${MOCK_GH_STATE}/pr_head")"
    base="$(cat "${MOCK_GH_STATE}/pr_base")"
    base_oid="$(cat "${MOCK_GH_STATE}/pr_base_oid")"
    owner="$(cat "${MOCK_GH_STATE}/pr_owner")"
    cross="$(cat "${MOCK_GH_STATE}/pr_cross")"
    draft="$(cat "${MOCK_GH_STATE}/pr_draft")"
    oid="$(cat "${MOCK_GH_STATE}/pr_head_oid")"
    merge=""
    if [ -f "${MOCK_GH_STATE}/merge_commit" ]; then merge="$(cat "${MOCK_GH_STATE}/merge_commit")"; fi
    title="Revert failed candidate"
    url="https://example.invalid/pr/${number}"
    jq -cn \
      --argjson number "${number}" --arg state "${state}" --arg head "${head}" \
      --arg base "${base}" --arg base_oid "${base_oid}" --arg owner "${owner}" --argjson cross "${cross}" \
      --argjson draft "${draft}" --arg oid "${oid}" --arg merge "${merge}" \
      --arg title "${title}" --arg url "${url}" \
      '{number:$number,state:$state,isDraft:$draft,baseRefName:$base,baseRefOid:$base_oid,headRefName:$head,
        headRefOid:$oid,headRepositoryOwner:{login:$owner},isCrossRepository:$cross,
        mergeCommit:(if $merge == "" then null else {oid:$merge} end),title:$title,url:$url,
        author:{login:$owner}}'
}

next_check_state() {
    local token="success"
    local rest
    if [ -s "${MOCK_GH_STATE}/check_sequence" ]; then
        token="$(sed -n '1p' "${MOCK_GH_STATE}/check_sequence")"
        rest="$(sed -n '2,$p' "${MOCK_GH_STATE}/check_sequence")"
        printf '%s\n' "${rest}" >"${MOCK_GH_STATE}/check_sequence"
    fi
    printf '%s\n' "${token}"
}

checks_json() {
    local token="$1"
    case "${token}" in
        success)
            touch "${MOCK_GH_STATE}/last_checks_success"
            jq -cn '[{name:"Test",state:"SUCCESS",bucket:"pass",workflow:"CI"},{name:"Build",state:"SUCCESS",bucket:"pass",workflow:"CI"}]'
            ;;
        pending)
            rm -f "${MOCK_GH_STATE}/last_checks_success"
            jq -cn '[{name:"Test",state:"IN_PROGRESS",bucket:"pending",workflow:"CI"},{name:"Build",state:"QUEUED",bucket:"pending",workflow:"CI"}]'
            ;;
        partial)
            rm -f "${MOCK_GH_STATE}/last_checks_success"
            jq -cn '[{name:"Test",state:"SUCCESS",bucket:"pass",workflow:"CI"}]'
            ;;
        missing)
            rm -f "${MOCK_GH_STATE}/last_checks_success"
            echo '[]'
            ;;
        failure|cancelled|skipped|neutral|timed_out|startup_failure|stale|action_required)
            rm -f "${MOCK_GH_STATE}/last_checks_success"
            local state bucket
            state="${token^^}"
            bucket="fail"
            case "${token}" in
                cancelled) bucket="cancel" ;;
                skipped|neutral) bucket="skipping" ;;
                stale) bucket="pending" ;;
            esac
            jq -cn --arg state "${state}" --arg bucket "${bucket}" \
              '[{name:"Test",state:$state,bucket:$bucket,workflow:"CI"},{name:"Build",state:"SUCCESS",bucket:"pass",workflow:"CI"}]'
            ;;
        *) die "unknown check state ${token}" ;;
    esac
}

case "${1:-} ${2:-}" in
    "auth status")
        [ "$#" -eq 2 ] || die "unexpected auth arguments"
        [ ! -f "${MOCK_GH_STATE}/fail_auth" ] || exit 1
        ;;
    "repo view")
        [ "$#" -eq 7 ] || die "unexpected repo view arguments"
        has_arg --repo "$@" && die "repo view does not support --repo"
        [ "${3:-}" = testowner/testrepo ] || die "repository must be the positional repo view argument"
        require_pair --json nameWithOwner "$@"
        require_pair --jq .nameWithOwner "$@"
        echo testowner/testrepo
        ;;
    "api repos/testowner/testrepo/branches/main/protection/required_status_checks")
        [ "$#" -eq 2 ] || die "unexpected api arguments"
        [ ! -f "${MOCK_GH_STATE}/fail_required_api" ] || { echo "injected api failure" >&2; exit 1; }
        cat "${MOCK_GH_STATE}/required_checks"
        ;;
    "pr list")
        require_pair --repo testowner/testrepo "$@"
        require_pair --head "$(cat "${MOCK_GH_STATE}/expected_revert_branch")" "$@"
        require_pair --base main "$@"
        require_pair --state all "$@"
        require_pair --limit 100 "$@"
        require_pair --json "number,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,mergeCommit" "$@"
        if [ -f "${MOCK_GH_STATE}/pr_number" ]; then pr_json | jq -c '[.]'; else echo '[]'; fi
        ;;
    "pr create")
        require_pair --repo testowner/testrepo "$@"
        require_pair --base main "$@"
        require_pair --head "testowner:$(cat "${MOCK_GH_STATE}/expected_revert_branch")" "$@"
        has_arg --title "$@" || die "missing title"
        has_arg --body "$@" || die "missing body"
        [ ! -f "${MOCK_GH_STATE}/pr_number" ] || die "duplicate PR creation"
        increment pr_create_count
        echo 17 >"${MOCK_GH_STATE}/pr_number"
        echo OPEN >"${MOCK_GH_STATE}/pr_state"
        cat "${MOCK_GH_STATE}/expected_revert_branch" >"${MOCK_GH_STATE}/pr_head"
        echo main >"${MOCK_GH_STATE}/pr_base"
        "${MOCK_REAL_GIT}" rev-parse origin/main >"${MOCK_GH_STATE}/pr_base_oid"
        echo testowner >"${MOCK_GH_STATE}/pr_owner"
        echo false >"${MOCK_GH_STATE}/pr_cross"
        echo false >"${MOCK_GH_STATE}/pr_draft"
        "${MOCK_REAL_GIT}" rev-parse "origin/$(cat "${MOCK_GH_STATE}/pr_head")" >"${MOCK_GH_STATE}/pr_head_oid"
        echo "https://example.invalid/pr/17"
        ;;
    "pr view")
        require_pair --repo testowner/testrepo "$@"
        [ ! -f "${MOCK_GH_STATE}/fail_pr_view" ] || { echo "injected pr view failure" >&2; exit 1; }
        [ -f "${MOCK_GH_STATE}/pr_number" ] || die "no PR exists"
        selector="${3:-}"
        [ "${selector}" = "$(cat "${MOCK_GH_STATE}/pr_number")" ] || die "wrong PR selector ${selector}"
        requested_fields="$(arg_after --json "$@")"
        case "${requested_fields}" in
            number,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,mergeCommit)
                pr_json
                ;;
            number,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,mergeCommit,title,url,author)
                pr_json
                ;;
            *) die "unexpected pr view JSON fields ${requested_fields}" ;;
        esac
        ;;
    "pr checks")
        [ "${3:-}" = "$(cat "${MOCK_GH_STATE}/pr_number")" ] || die "wrong checks PR"
        require_pair --repo testowner/testrepo "$@"
        require_pair --json name,state,bucket,workflow "$@"
        has_arg --watch "$@" && die "--watch is forbidden"
        token="$(next_check_state)"
        checks_json "${token}"
        if [ "${token}" = success ] && [ -f "${MOCK_GH_STATE}/change_head_after_checks" ]; then
            printf '%040d\n' 9 >"${MOCK_GH_STATE}/pr_head_oid"
            rm -f "${MOCK_GH_STATE}/change_head_after_checks"
        fi
        if [ "${token}" = success ] && [ -f "${MOCK_GH_STATE}/change_base_after_checks" ]; then
            printf '%040d\n' 7 >"${MOCK_GH_STATE}/pr_base_oid"
            rm -f "${MOCK_GH_STATE}/change_base_after_checks"
        fi
        ;;
    "pr merge")
        [ "${3:-}" = "$(cat "${MOCK_GH_STATE}/pr_number")" ] || die "wrong merge PR"
        require_pair --repo testowner/testrepo "$@"
        has_arg --squash "$@" || die "merge must use --squash"
        require_pair --match-head-commit "$(cat "${MOCK_GH_STATE}/pr_head_oid")" "$@"
        [ "$(cat "${MOCK_GH_STATE}/pr_state")" = OPEN ] || die "PR is not open"
        [ "$(cat "${MOCK_GH_STATE}/pr_draft")" = false ] || die "PR is draft"
        [ -f "${MOCK_GH_STATE}/last_checks_success" ] || die "merge attempted before successful checks"
        increment pr_merge_count
        head="$(cat "${MOCK_GH_STATE}/pr_head")"
        merge_dir="$(mktemp -d "${MOCK_GH_STATE}/merge.XXXXXX")"
        "${MOCK_REAL_GIT}" clone -q "${MOCK_ORIGIN}" "${merge_dir}"
        "${MOCK_REAL_GIT}" -C "${merge_dir}" config user.name "Test User"
        "${MOCK_REAL_GIT}" -C "${merge_dir}" config user.email "test@example.invalid"
        "${MOCK_REAL_GIT}" -C "${merge_dir}" switch -q main
        "${MOCK_REAL_GIT}" -C "${merge_dir}" merge --squash "origin/${head}" >/dev/null
        "${MOCK_REAL_GIT}" -C "${merge_dir}" commit -q -m "Revert failed QA candidate"
        "${MOCK_REAL_GIT}" -C "${merge_dir}" push -q origin main
        "${MOCK_REAL_GIT}" -C "${merge_dir}" rev-parse HEAD >"${MOCK_GH_STATE}/merge_commit"
        echo MERGED >"${MOCK_GH_STATE}/pr_state"
        rm -rf -- "${merge_dir}"
        ;;
    *) die "unexpected gh invocation: $*" ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/gh"
}

new_fixture() {
    local name="$1"
    CASE_ROOT="${TEST_ROOT}/${name}"
    WORK="${CASE_ROOT}/work"
    ORIGIN="${CASE_ROOT}/origin.git"
    STATE="${CASE_ROOT}/gh-state"
    MOCK_BIN="${CASE_ROOT}/bin"
    mkdir -p "${WORK}" "${STATE}" "${MOCK_BIN}"
    git init -q --bare "${ORIGIN}"
    git init -q -b main "${WORK}"
    git -C "${WORK}" config user.name "Test User"
    git -C "${WORK}" config user.email "test@example.invalid"
    git -C "${WORK}" config url."${ORIGIN}".insteadOf https://github.com/testowner/testrepo.git
    git -C "${WORK}" remote add origin https://github.com/testowner/testrepo.git
    mkdir -p "${WORK}/scripts/release"
    cp "${SOURCE_ROOT}/scripts/release/fail_qa.sh" "${WORK}/scripts/release/fail_qa.sh"
    cp "${SOURCE_ROOT}/scripts/release/pr_workflow_lib.sh" "${WORK}/scripts/release/pr_workflow_lib.sh"
    cp "${SOURCE_ROOT}/scripts/release/squash_merge_pull_req.sh" "${WORK}/scripts/release/squash_merge_pull_req.sh"
    echo base >"${WORK}/feature.txt"
    git -C "${WORK}" add .
    git -C "${WORK}" commit -q -m "Base"
    git -C "${WORK}" push -q -u origin main
    echo candidate >"${WORK}/feature.txt"
    git -C "${WORK}" commit -q -am "Candidate"
    CANDIDATE="$(git -C "${WORK}" rev-parse HEAD)"
    git -C "${WORK}" push -q origin main
    printf '%s\n' "revert/failed-candidate-${CANDIDATE:0:12}" >"${STATE}/expected_revert_branch"
    printf '%s\n' '{"contexts":["Test","Build"],"checks":[]}' >"${STATE}/required_checks"
    write_git_mock
    write_gh_mock
}

run_fail() {
    set +e
    RUN_OUTPUT="$(
        cd "${WORK}" &&
        PATH="${MOCK_BIN}:${PATH}" \
        MOCK_GH_STATE="${STATE}" \
        MOCK_ORIGIN="${ORIGIN}" \
        MOCK_REAL_GIT="${REAL_GIT}" \
        FASTSELL_RELEASE_CHECK_ATTEMPTS=4 \
        FASTSELL_RELEASE_CHECK_SLEEP_SECONDS=0 \
        bash scripts/release/fail_qa.sh "$@" 2>&1
    )"
    RUN_STATUS="$?"
    set -e
}

run_squash_merge() {
    set +e
    RUN_OUTPUT="$(
        cd "${WORK}" &&
        PATH="${MOCK_BIN}:${PATH}" \
        MOCK_GH_STATE="${STATE}" \
        MOCK_ORIGIN="${ORIGIN}" \
        MOCK_REAL_GIT="${REAL_GIT}" \
        FASTSELL_RELEASE_CHECK_ATTEMPTS=4 \
        FASTSELL_RELEASE_CHECK_SLEEP_SECONDS=0 \
        bash scripts/release/squash_merge_pull_req.sh "$@" 2>&1
    )"
    RUN_STATUS="$?"
    set -e
}

revert_branch() { cat "${STATE}/expected_revert_branch"; }

prepare_revert_branch() {
    local extra_before="${1:-0}"
    local extra_after="${2:-0}"
    local branch
    branch="$(revert_branch)"
    git -C "${WORK}" switch -q -c "${branch}" origin/main
    if [ "${extra_before}" -eq 1 ]; then
        echo before >"${WORK}/before.txt"
        git -C "${WORK}" add before.txt
        git -C "${WORK}" commit -q -m "Unrelated before revert"
    fi
    git -C "${WORK}" revert --no-edit "${CANDIDATE}" >/dev/null
    if [ "${extra_after}" -eq 1 ]; then
        echo after >"${WORK}/after.txt"
        git -C "${WORK}" add after.txt
        git -C "${WORK}" commit -q -m "Unrelated after revert"
    fi
    git -C "${WORK}" push -q -u origin "${branch}"
    git -C "${WORK}" switch -q main
}

prepare_open_pr() {
    local branch
    branch="$(revert_branch)"
    prepare_revert_branch
    echo 17 >"${STATE}/pr_number"
    echo OPEN >"${STATE}/pr_state"
    echo "${branch}" >"${STATE}/pr_head"
    echo main >"${STATE}/pr_base"
    git -C "${WORK}" rev-parse origin/main >"${STATE}/pr_base_oid"
    echo testowner >"${STATE}/pr_owner"
    echo false >"${STATE}/pr_cross"
    echo false >"${STATE}/pr_draft"
    git -C "${WORK}" rev-parse "origin/${branch}" >"${STATE}/pr_head_oid"
}

assert_main_synchronized() {
    [ "$(git -C "${WORK}" rev-parse main)" = "$(git -C "${WORK}" rev-parse origin/main)" ] || fail "local main is not synchronized"
}

test_clean_and_idempotent() {
    new_fixture clean
    printf '%s\n' pending success >"${STATE}/check_sequence"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_success
    assert_contains "Every required check explicitly passed"
    assert_not_contains "Expected candidate image tags"
    [ "$(git -C "${WORK}" branch --show-current)" = retry/fix ] || fail "retry branch not selected"
    [ "$(git -C "${WORK}" rev-list --count origin/main..retry/fix)" -eq 1 ] || fail "retry topology is not one commit"
    [ "$(git -C "${WORK}" rev-list --count "${CANDIDATE}..$(revert_branch)")" -eq 1 ] || fail "revert topology is not one commit"
    assert_main_synchronized
    assert_clean
    local creates merges retry_tip
    creates="$(state_count pr_create_count)"; merges="$(state_count pr_merge_count)"; retry_tip="$(git -C "${WORK}" rev-parse retry/fix)"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_success
    [ "$(state_count pr_create_count)" -eq "${creates}" ] || fail "duplicate PR"
    [ "$(state_count pr_merge_count)" -eq "${merges}" ] || fail "duplicate merge"
    [ "$(git -C "${WORK}" rev-parse retry/fix)" = "${retry_tip}" ] || fail "duplicate cherry-pick"
    echo "[OK] exact full workflow and idempotent rerun"
}

test_basic_refusals() {
    new_fixture dirty
    echo dirty >"${WORK}/dirty.txt"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "Working tree has uncommitted changes"

    new_fixture invalid
    run_fail invalid retry/fix --yes
    assert_failure; assert_contains "40-character"

    new_fixture absent
    git -C "${WORK}" switch -q --orphan absent
    git -C "${WORK}" rm -q -rf --ignore-unmatch .
    echo absent >"${WORK}/absent.txt"
    git -C "${WORK}" add .
    git -C "${WORK}" commit -q -m Absent
    local absent_sha
    absent_sha="$(git -C "${WORK}" rev-parse HEAD)"
    git -C "${WORK}" switch -q main
    run_fail "${absent_sha}" retry/fix --yes
    assert_failure; assert_contains "not reachable from origin/main"
    echo "[OK] dirty, invalid SHA, and absent-candidate refusals"
}

test_pr_identity_refusals() {
    local field message
    for field in fork head base base_oid draft; do
        new_fixture "identity_${field}"
        prepare_open_pr
        case "${field}" in
            fork) echo forkowner >"${STATE}/pr_owner"; echo true >"${STATE}/pr_cross"; message="cross-repository" ;;
            head) echo unrelated/head >"${STATE}/pr_head"; message="head unrelated/head" ;;
            base) echo release >"${STATE}/pr_base"; message="not main" ;;
            base_oid) printf '%040d\n' 6 >"${STATE}/pr_base_oid"; message="base changed" ;;
            draft) echo true >"${STATE}/pr_draft"; message="draft" ;;
        esac
        run_fail "${CANDIDATE}" retry/fix --yes
        assert_failure; assert_contains "${message}"
        [ "$(state_count pr_merge_count)" -eq 0 ] || fail "identity-invalid PR merged"
    done
    new_fixture head_oid
    prepare_open_pr
    printf '%040d\n' 8 >"${STATE}/pr_head_oid"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "head changed"
    echo "[OK] exact repository owner, cross-repo, base, draft, and OID validation"
}

test_head_change_after_checks() {
    new_fixture changed_head
    prepare_open_pr
    touch "${STATE}/change_head_after_checks"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "head changed"
    [ "$(state_count pr_merge_count)" -eq 0 ] || fail "changed PR head merged"

    new_fixture changed_base
    prepare_open_pr
    touch "${STATE}/change_base_after_checks"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "base changed"
    [ "$(state_count pr_merge_count)" -eq 0 ] || fail "changed PR base merged"
    echo "[OK] PR head and base changes after checks are rejected"
}

test_disallowed_check_states() {
    local state
    for state in failure cancelled skipped neutral timed_out startup_failure stale action_required; do
        new_fixture "check_${state}"
        prepare_open_pr
        echo "${state}" >"${STATE}/check_sequence"
        run_fail "${CANDIDATE}" retry/fix --yes
        assert_failure; assert_contains "disallowed state"
        [ "$(state_count pr_merge_count)" -eq 0 ] || fail "${state} checks merged"
    done
    echo "[OK] failed, cancelled, skipped, neutral, timed-out, startup-failure, stale, and action-required checks rejected"
}

test_missing_and_pending_timeouts() {
    local state
    for state in missing partial pending; do
        new_fixture "timeout_${state}"
        prepare_open_pr
        printf '%s\n' "${state}" "${state}" "${state}" "${state}" >"${STATE}/check_sequence"
        run_fail "${CANDIDATE}" retry/fix --yes
        assert_failure; assert_contains "Timed out after 4 check polls"
        [ "$(state_count pr_merge_count)" -eq 0 ] || fail "${state} checks merged"
    done
    echo "[OK] missing, partial, and permanently pending checks reach finite timeout"
}

test_revert_branch_topology() {
    new_fixture earlier_extra
    prepare_revert_branch 1 0
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "exactly one commit"

    new_fixture later_extra
    prepare_revert_branch 0 1
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "exactly one commit"

    new_fixture branch_only
    git -C "${WORK}" switch -q -c "$(revert_branch)" origin/main
    git -C "${WORK}" switch -q main
    run_fail "${CANDIDATE}" --rollback-only --yes
    assert_success; assert_contains "unchanged at its expected base"
    echo "[OK] strict revert topology and branch-created interruption resume"
}

test_retry_branch_topology() {
    new_fixture retry_extra
    run_fail "${CANDIDATE}" --rollback-only --yes
    assert_success
    git -C "${WORK}" switch -q -c retry/collision main
    git -C "${WORK}" cherry-pick -x "${CANDIDATE}" >/dev/null
    echo unrelated >"${WORK}/unrelated.txt"
    git -C "${WORK}" add unrelated.txt
    git -C "${WORK}" commit -q -m Unrelated
    run_fail "${CANDIDATE}" retry/collision --yes
    assert_failure; assert_contains "exactly one commit"
    echo "[OK] candidate-equivalent retry branch with unrelated work rejected"
}

test_revert_conflict_recovery() {
    new_fixture revert_conflict
    echo later >"${WORK}/feature.txt"
    git -C "${WORK}" commit -q -am "Later conflicting main change"
    git -C "${WORK}" push -q origin main
    run_fail "${CANDIDATE}" --rollback-only --allow-non-head --yes
    assert_failure
    assert_contains "git revert --continue"
    assert_contains "git revert --abort"
    [ -f "${WORK}/.git/REVERT_HEAD" ] || fail "revert conflict state not preserved"
    git -C "${WORK}" revert --abort
    echo "[OK] revert conflict state and recovery instructions"
}

test_direct_resume() {
    new_fixture direct_resume
    git -C "${WORK}" switch -q main
    git -C "${WORK}" revert --no-edit "${CANDIDATE}" >/dev/null
    local direct_tip
    direct_tip="$(git -C "${WORK}" rev-parse HEAD)"
    run_fail "${CANDIDATE}" --direct --yes
    assert_success; assert_contains "Resuming interrupted direct mode"
    [ "$(git -C "${WORK}" rev-parse origin/main)" = "${direct_tip}" ] || fail "direct resume did not push exact revert"
    run_fail "${CANDIDATE}" --direct --yes
    assert_success; assert_contains "already completed and pushed"
    [ "$(git -C "${WORK}" rev-parse origin/main)" = "${direct_tip}" ] || fail "direct rerun duplicated revert"

    new_fixture direct_not_implicit
    run_fail "${CANDIDATE}"
    assert_failure; assert_contains "Retry branch name is required"
    echo "[OK] direct interrupted/completed resume and explicit-only selection"
}

test_unmasked_command_failures() {
    new_fixture status_failure
    touch "${STATE}/fail_git_status"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "Could not inspect the working tree"

    new_fixture gh_failure
    prepare_open_pr
    touch "${STATE}/fail_pr_view"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "Could not load revert PR"

    new_fixture patch_failure
    touch "${STATE}/fail_patch_id"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_failure; assert_contains "Could not compute"
    echo "[OK] git, gh, and patch-ID failures are not masked"
}

test_merged_resume_and_rollback_only() {
    new_fixture merged_resume
    run_fail "${CANDIDATE}" --rollback-only --yes
    assert_success
    [ "$(git -C "${WORK}" branch --show-current)" = main ] || fail "rollback-only did not stop on main"
    local merges
    merges="$(state_count pr_merge_count)"
    run_fail "${CANDIDATE}" retry/fix --yes
    assert_success; assert_contains "already merged and exactly verified"
    [ "$(state_count pr_merge_count)" -eq "${merges}" ] || fail "merged resume duplicated merge"
    echo "[OK] already-merged resume, rollback-only, and exact final topology"
}

test_shared_squash_merge_guard() {
    new_fixture shared_merge
    prepare_open_pr
    printf '%s\n' pending success >"${STATE}/check_sequence"
    run_squash_merge 17 --yes
    assert_success
    assert_contains "Every required check explicitly passed"
    assert_contains "Expected candidate image tags"
    [ "$(state_count pr_merge_count)" -eq 1 ] || fail "shared helper did not merge exactly once"
    assert_main_synchronized
    echo "[OK] shared squash helper uses finite strict checks and head-bound merge"
}

test_mock_rejects_incomplete_commands() {
    local output status branch
    new_fixture mock_contract
    branch="$(revert_branch)"
    set +e
    output="$(
        cd "${WORK}" &&
        PATH="${MOCK_BIN}:${PATH}" MOCK_GH_STATE="${STATE}" MOCK_ORIGIN="${ORIGIN}" MOCK_REAL_GIT="${REAL_GIT}" \
            gh repo view --repo testowner/testrepo --json nameWithOwner --jq .nameWithOwner 2>&1
    )"
    status="$?"
    set -e
    [ "${status}" -ne 0 ] || fail "mock accepted unsupported gh repo view --repo form"
    [[ "${output}" == *"repo view"* ]] || fail "mock did not reject unsupported repo view arguments"

    set +e
    output="$(
        cd "${WORK}" &&
        PATH="${MOCK_BIN}:${PATH}" MOCK_GH_STATE="${STATE}" MOCK_ORIGIN="${ORIGIN}" MOCK_REAL_GIT="${REAL_GIT}" \
            gh pr list --head "${branch}" --base main --state all --limit 100 \
            --json "number,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,mergeCommit" 2>&1
    )"
    status="$?"
    set -e
    [ "${status}" -ne 0 ] || fail "mock accepted pr list without --repo"
    [[ "${output}" == *"missing --repo"* ]] || fail "mock did not identify missing repository context"

    prepare_open_pr
    set +e
    output="$(
        cd "${WORK}" &&
        PATH="${MOCK_BIN}:${PATH}" MOCK_GH_STATE="${STATE}" MOCK_ORIGIN="${ORIGIN}" MOCK_REAL_GIT="${REAL_GIT}" \
            gh pr checks 17 --repo testowner/testrepo --json name,state,bucket,workflow --watch 2>&1
    )"
    status="$?"
    set -e
    [ "${status}" -ne 0 ] || fail "mock accepted forbidden --watch"
    [[ "${output}" == *"--watch is forbidden"* ]] || fail "mock did not reject --watch"

    touch "${STATE}/last_checks_success"
    set +e
    output="$(
        cd "${WORK}" &&
        PATH="${MOCK_BIN}:${PATH}" MOCK_GH_STATE="${STATE}" MOCK_ORIGIN="${ORIGIN}" MOCK_REAL_GIT="${REAL_GIT}" \
            gh pr merge 17 --repo testowner/testrepo --squash 2>&1
    )"
    status="$?"
    set -e
    [ "${status}" -ne 0 ] || fail "mock accepted merge without --match-head-commit"
    [[ "${output}" == *"missing --match-head-commit"* ]] || fail "mock did not require head binding"
    echo "[OK] gh mock rejects repo view --repo, missing PR context, JSON-watch fallback, and unbound merge"
}

test_clean_and_idempotent
test_basic_refusals
test_pr_identity_refusals
test_head_change_after_checks
test_disallowed_check_states
test_missing_and_pending_timeouts
test_revert_branch_topology
test_retry_branch_topology
test_revert_conflict_recovery
test_direct_resume
test_unmasked_command_failures
test_merged_resume_and_rollback_only
test_shared_squash_merge_guard
test_mock_rejects_incomplete_commands

echo "[OK] all isolated fail_qa adversarial workflow tests passed"
