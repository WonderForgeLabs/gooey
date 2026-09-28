#!/usr/bin/env bash
# Smoke tests for check.sh.
#
# Pure — no network, no real `gh`: each case puts a stub `gh` on PATH that
# records its arguments, so "was the resource labelled?" is observable without
# touching GitHub. Run by ci.yml's discover job.
#
# The load-bearing case is "recovered denials": a run that completed its task
# but hit a permission denial must NOT be labelled, or `gh issue list --label
# failed:issue-intake` stops meaning "needs re-driving" (see check.sh).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/check.sh"

FAILURES=0
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

mkdir -p "${WORK}/bin"
cat > "${WORK}/bin/gh" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${WORK}/gh-calls"
exit 0
EOF
chmod +x "${WORK}/bin/gh"

# execution_file <is_error> <denial_count> -> path
execution_file() {
    local is_error="$1"
    local denials="$2"
    local path="${WORK}/exec-${is_error}-${denials}.json"
    {
        printf '[{"type":"system","subtype":"init"},'
        printf '{"type":"result","is_error":%s,"permission_denials":[' "${is_error}"
        local i
        for ((i = 0; i < denials; i++)); do
            [ "${i}" -gt 0 ] && printf ','
            printf '{"tool_name":"Bash"}'
        done
        printf ']}]'
    } > "${path}"
    echo "${path}"
}

# run_check <execution_file> <agent_step_outcome> -> exit status
run_check() {
    : > "${WORK}/gh-calls"
    : > "${WORK}/output"
    : > "${WORK}/stdout"
    local status=0
    env PATH="${WORK}/bin:${PATH}" \
        GITHUB_OUTPUT="${WORK}/output" \
        GH_TOKEN=stub \
        REPO="owner/repo" \
        RESOURCE_NUMBER="123" \
        LABEL="failed:issue-intake" \
        EXECUTION_FILE="$1" \
        AGENT_STEP_OUTCOME="$2" \
        bash "${SCRIPT}" > "${WORK}/stdout" 2>&1 || status=$?
    return "${status}"
}

labelled() {
    grep -q 'labels\[\]=failed:issue-intake' "${WORK}/gh-calls" && echo yes || echo no
}
output_of() { grep -E "^$1=" "${WORK}/output" | tail -1 | cut -d= -f2-; }
warned_on_denials() {
    grep -q '::warning::Hit .* permission denial' "${WORK}/stdout" && echo yes || echo no
}

# check <name> <want_status> <want_labelled> <want_failed> <got_status>
check() {
    local name="$1" want_status="$2" want_labelled="$3" want_failed="$4" got_status="$5"
    local got_labelled got_failed
    got_labelled="$(labelled)"
    got_failed="$(output_of failed)"
    if [ "${got_status}" != "${want_status}" ] \
        || [ "${got_labelled}" != "${want_labelled}" ] \
        || [ "${got_failed}" != "${want_failed}" ]; then
        echo "FAIL: ${name} — status=${got_status} (want ${want_status}), labelled=${got_labelled} (want ${want_labelled}), failed=${got_failed} (want ${want_failed})"
        FAILURES=$((FAILURES + 1))
        return
    fi
    echo "ok: ${name}"
}

expect_denial_warning() {
    local name="$1" want="$2" got
    got="$(warned_on_denials)"
    if [ "${got}" != "${want}" ]; then
        echo "FAIL: ${name} — denial warning=${got} (want ${want})"
        FAILURES=$((FAILURES + 1))
        return
    fi
    echo "ok: ${name}"
}

# --- the regression: a completed run with recovered denials is NOT a failure --
# Reproduces forge run 30423537171 on forge#3217 — intake finished (is_error=false,
# needs-triage cleared) with 4 denials, and used to be stamped failed anyway.
s=0; run_check "$(execution_file false 4)" success || s=$?
check "recovered permission denials do not label the resource" 0 no false "${s}"
expect_denial_warning "recovered denials still warn" yes

# --- real failures must still be loud and labelled --------------------------
s=0; run_check "$(execution_file true 0)" success || s=$?
check "an aborted agent is labelled and fails" 1 yes true "${s}"

s=0; run_check "$(execution_file true 3)" success || s=$?
check "an aborted agent with denials is labelled and fails" 1 yes true "${s}"

# A job-timeout kill leaves no execution file at all; agent-step-outcome is the
# only signal that the run died rather than nothing being there to check.
s=0; run_check "" cancelled || s=$?
check "a cancelled agent step with no execution file is labelled and fails" 1 yes true "${s}"

# --- clean runs stay clean --------------------------------------------------
s=0; run_check "$(execution_file false 0)" success || s=$?
check "a clean run is not labelled" 0 no false "${s}"
expect_denial_warning "a clean run does not warn about denials" no

# No execution file but the step itself succeeded: nothing to verify, and
# nothing to blame the resource for.
s=0; run_check "" success || s=$?
check "no execution file with a successful step is not labelled" 0 no false "${s}"

# --- an empty resource-number skips labelling but still fails loudly ---------
# The scheduled-sweep shape (issue-reopen-audit's cron has no single target).
: > "${WORK}/gh-calls"; : > "${WORK}/output"
s=0
env PATH="${WORK}/bin:${PATH}" GITHUB_OUTPUT="${WORK}/output" GH_TOKEN=stub \
    REPO="owner/repo" RESOURCE_NUMBER="" LABEL="failed:issue-reopen-audit" \
    EXECUTION_FILE="$(execution_file true 0)" AGENT_STEP_OUTCOME=success \
    bash "${SCRIPT}" >/dev/null 2>&1 || s=$?
if [ "${s}" -eq 1 ] && [ "$(labelled)" = "no" ]; then
    echo "ok: an empty resource-number still fails the job without labelling"
else
    echo "FAIL: an empty resource-number still fails the job without labelling — status=${s}, labelled=$(labelled)"
    FAILURES=$((FAILURES + 1))
fi

if [ "${FAILURES}" -gt 0 ]; then
    echo "${FAILURES} test(s) failed"
    exit 1
fi
echo "All label-on-agent-failure tests passed"
