#!/usr/bin/env bash
# Body of the label-on-agent-failure composite action. See action.yml for the
# input contract; every value arrives via the environment.
#
# Lives here rather than inline in action.yml so it can be unit-tested
# (check.test.sh, run by ci.yml's discover job). Ported from
# WonderForgeLabs/forge#3219 — keep in sync with forge's copy by hand.
set -uo pipefail

is_error=false
denials=0

if [ -n "${EXECUTION_FILE:-}" ] && [ -f "$EXECUTION_FILE" ]; then
  # The execution file is a JSON array of SDK messages; the final
  # type=result element carries the run's status.
  result="$(jq -c '.[] | select(.type == "result")' "$EXECUTION_FILE" | tail -n1)"
  if [ -n "$result" ]; then
    is_error="$(echo "$result" | jq -r '.is_error // false')"
    denials="$(echo "$result" | jq -r '(.permission_denials | length) // 0')"
  else
    echo "::warning::No result record found in the execution file; cannot verify completion."
  fi
else
  echo "::warning::No execution file produced by the agent step; cannot verify completion from its output."
  if [ -n "${AGENT_STEP_OUTCOME:-}" ] && [ "$AGENT_STEP_OUTCOME" != "success" ]; then
    echo "::error::Agent step outcome was '${AGENT_STEP_OUTCOME}' (not success) and produced no execution file — treating as failed rather than silently passing."
    is_error=true
  fi
fi

echo "Agent result: is_error=$is_error, permission_denials=$denials"
echo "is_error=$is_error" >> "$GITHUB_OUTPUT"
echo "denials=$denials" >> "$GITHUB_OUTPUT"

# Only a real failure (the agent aborted, or the agent step itself did not
# succeed — both surface as is_error above) marks the resource.
#
# A permission denial the agent recovered from must NOT. The intake prompt
# reliably provokes benign denials (piping `gh` into an interpreter, reaching
# for a temp file to compose a comment body), so stamping on denials made
# `gh issue list --label failed:issue-intake` conflate "this needs re-driving"
# with "this completed fine" — defeating the purpose the label was added for
# (forge#3053). Verified live in forge: run 30423537171 on forge#3217 completed intake
# in full (is_error=false, 54 turns, needs-triage cleared) with 4 recovered
# denials, reported success, and was still labelled failed:issue-intake.
failed=false
if [ "$is_error" = "true" ]; then
  failed=true
fi
echo "failed=$failed" >> "$GITHUB_OUTPUT"

if [ "$failed" = "true" ] && [ -n "${RESOURCE_NUMBER:-}" ]; then
  # Idempotent — 422s if the label already exists, which is fine and
  # expected on every run after the first.
  gh label create "$LABEL" --repo "$REPO" --color "b60205" \
    --description "$LABEL: the workflow's agent aborted while processing this issue/PR — it needs re-driving" >/dev/null 2>&1 || true

  # The unified issues/labels endpoint works for both issues and PRs
  # (a PR is an issue under the hood) — unlike `gh issue edit`, which
  # can reject a PR number.
  gh api "repos/${REPO}/issues/${RESOURCE_NUMBER}/labels" -f "labels[]=${LABEL}" >/dev/null \
    || echo "::warning::Failed to apply ${LABEL} to #${RESOURCE_NUMBER}"
fi

# Advisory only, deliberately neither a failure nor a label: the agent
# routinely recovers from these. It is a signal to widen the caller's
# --allowedTools (or reword its prompt), investigated via the uploaded
# transcript — not a verdict on this issue/PR.
if [ "$denials" != "0" ]; then
  echo "::warning::Hit $denials permission denial(s) — a tool is likely missing from --allowedTools. Not treated as a failure; see the uploaded execution transcript for which calls were denied."
fi

if [ "$is_error" = "true" ]; then
  echo "::error::Agent terminated abnormally (is_error=true) with $denials permission denial(s)."
  exit 1
fi
