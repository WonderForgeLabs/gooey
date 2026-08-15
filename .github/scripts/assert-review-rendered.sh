#!/bin/sh
# Does a rendered review exist for THIS head commit?
#
# `review-with-tracking` is the job whose name exists so it can be made a
# required status check. It used to answer a different question — "did the
# last run of the reusable workflow avoid failing?" — and those come apart,
# because GitHub shows the latest run's conclusion per check name per SHA
# while a required check has to assert a property of the COMMIT (#198).
#
# The gap was reachable with triage permission and no push: add any label,
# every internal job of the reusable workflow legitimately skips, the mirror
# saw `skipped`, exited 0, and a previously-red check went green on the same
# commit with nothing having reviewed it.
#
# Two fixes that look right and are not, both killed by checking rather than
# reasoning:
#
#   - Skipping the mirror job on label events. A job skipped by its `if:`
#     still publishes a check run, and GitHub counts a skipped required
#     check as SATISFIED — the same bypass wearing a different hat.
#   - Comparing the review comment's `updated_at` against the head commit's
#     date. The sticky is only edited on some paths: on PR #258's healthy
#     review `updatedAt` is EMPTY, so the comparison would have degraded
#     silently on exactly the case it needed to pass.
#
# What is actually available is exact. The review sticky carries a job link:
#
#   **Claude finished @user's task in 4m 51s** —— [View job](…/actions/runs/31857725687)
#
# and that run's `head_sha` IS the commit it reviewed. So the run id in the
# comment resolves to a SHA with no prose parsing beyond the URL, and no
# dependence on forge stamping one (#185's suggestion 3 would be a better
# mechanism, and this does not wait for it).
#
#   sh .github/scripts/assert-review-rendered.sh <repo> <pr> <review-result>
#
# `GH` overrides the gh binary so the test suite can stub the API. Every
# fetch asks for raw JSON and pipes through jq here, rather than using
# `gh api --jq`, so the jq expressions are exercised by the stub too.

set -eu

repo=${1:?repo}
pr=${2:?pr number}
result=${3:?needs.review.result}
GH=${GH:-gh}

fail() { echo "::error::$1"; exit 1; }

echo "Reusable review workflow result: $result"

# A call that FAILED still fails the mirror — that half was always right.
# `success` and `skipped` both fall through to the real question below.
# `skipped` staying satisfiable is load-bearing: every internal job of the
# reusable workflow has its own `if:`, and unrelated label churn legitimately
# skips all of them. An earlier review can still cover this head.
case "$result" in
  success | skipped) ;;
  *) fail "The claude-code-review.reusable.yml call did not succeed (result=$result)." ;;
esac

head=$($GH api "repos/$repo/pulls/$pr" | jq -r '.head.sha')
[ -n "$head" ] && [ "$head" != "null" ] || fail "Could not read the head SHA of $repo#$pr."

# Comments come back oldest-first; the newest one carrying a job link is the
# review sticky for the most recent attempt.
body=$($GH api "repos/$repo/issues/$pr/comments?per_page=100" |
  jq -r '[.[] | select(.body | test("\\[View job\\]\\(https://[^)]*/actions/runs/[0-9]+\\)"))] | last | .body // ""')

[ -n "$body" ] || fail "No review comment on $repo#$pr — nothing has reviewed $head."

# Unreachable by construction, and kept deliberately: the jq above already
# DEFINES a review comment as one carrying a job link, so anything it selects
# has one for sed to find. The guard is a backstop for the two patterns
# drifting apart in a later edit — without it an empty $run builds the URL
# `…/actions/runs/` and the failure surfaces three lines down as a confusing
# SHA mismatch instead of naming itself.
#
# Its unreachability was found by the suite, not by reading: asserting the
# MESSAGE rather than just the exit code turned this from a passing case into
# a failing one.
run=$(printf '%s' "$body" | sed -n 's|.*/actions/runs/\([0-9][0-9]*\).*|\1|p' | tail -1)
[ -n "$run" ] || fail "The newest review comment on $repo#$pr carries no job link, so what it reviewed cannot be established."

sha=$($GH api "repos/$repo/actions/runs/$run" | jq -r '.head_sha')
[ "$sha" = "$head" ] || fail "The newest review covers $sha; this PR's head is $head. A verdict against an older commit does not satisfy the check for a newer one."

# The run existed and matched, which says the attempt was ABOUT this commit.
# Whether it produced anything is a separate question, and the two failure
# shapes look nothing alike in the comment.
case "$body" in
  *"encountered an error"*)
    fail "The review for $head aborted before rendering a verdict (run $run). A run that dies before its first turn leaves this comment and a job that still reports success — see #273."
    ;;
esac

# An unfinished checklist is the other way a review ends with no verdict: the
# job completes, the sticky says how long it took, and `Post final review` is
# still unticked. That is #193's shape, and it is invisible to run status.
if printf '%s' "$body" | grep -q -- '- \[ \]'; then
  fail "The review for $head never finished — unchecked items remain in the checklist (run $run)."
fi

echo "A rendered review covers $head (run $run)."
