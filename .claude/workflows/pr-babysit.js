export const meta = {
  name: 'pr-babysit',
  description: 'Drive a PR (or a stack) from draft to 100%: watch checks, read every comment surface, distinguish infra from real failures, back off up to 8h',
  whenToUse: 'After opening a draft PR. Loops until the PR is green AND every review finding is resolved. Use for a stack too — it reports per-PR so the coordinator can merge in order.',
  phases: [
    { title: 'Assess', detail: 'per-PR: checks, verdict, findings — in parallel' },
    { title: 'Decide', detail: 'what to fix, what to wait on, how long to sleep' },
  ],
}

// A PR is not done when it is green. It is done when it is green AND
// every finding is resolved AND a review actually rendered a verdict.
// Those are three different facts and CI reports only the first.
//
// This workflow ASSESSES and RECOMMENDS; it does not push, and it does
// not merge. The coordinator (or the owning agent) acts on its verdict.
// Run it repeatedly — it is one turn of the babysit cycle, not the loop.
//
//   Workflow({ name: 'pr-babysit', args: { prs: [207, 208] } })
//   Workflow({ name: 'pr-babysit', args: { prs: [207], attempt: 3 } })
//
// `attempt` drives the backoff recommendation: infra faults here recover
// on their own timescale, and hammering them wastes runner capacity that
// other sessions are queued for.
const REPO = (typeof args === 'object' && args?.repo) || '/home/elan/repos/WonderForgeLabs/gooey'
const SLUG = (typeof args === 'object' && args?.slug) || 'WonderForgeLabs/gooey'
const PRS = (typeof args === 'object' && args?.prs) || []
const ATTEMPT = (typeof args === 'object' && args?.attempt) || 1

if (!PRS.length) {
  return { error: 'pr-babysit needs args.prs — e.g. { prs: [207] }. Nothing to assess.' }
}

const RULES = `You are assessing pull requests in ${SLUG} (checkout at ${REPO}).

READ-ONLY: no pushes, no merges, no edits, no new comments. You report; someone else acts.

## Two things about this repo's checks that will mislead you

**1. A green check is not a review.** \`review / pr-review\` can conclude SUCCESS in at least three situations that all mean NO REVIEW EXISTS:
  - the run aborted mid-way (checklist has unchecked boxes, no summary) and still exited 0;
  - the run completed and posted nothing at all;
  - a LATER run OVERWROTE a completed review — the comment is sticky and edited in place, so a finished 12-finding review can be replaced by an unfinished checklist.

  So: require a **rendered verdict** in the comment BODY — a summary section, or an explicit "No issues found". NEVER infer from "no unchecked boxes": a just-started review has none either, because those comments are edited in place and every read is a snapshot of a live document. Require BOTH a completed run AND a rendered verdict.

  If a verdict seems to have vanished, it is often recoverable:
  \`\`\`
  gh api repos/OWNER/REPO/issues/comments/<id> --jq .node_id
  gh api graphql -f query='{node(id:"<node_id>"){... on IssueComment{
    userContentEdits(first:20){nodes{editedAt diff}}}}}'
  \`\`\`

**2. A re-run hides the deciding attempt.** \`gh run view\`, check-runs, and \`/actions/runs/<id>/jobs\` all report ONLY the latest attempt. A job that failed on attempt 1 shows as queued. Read it explicitly:
  \`gh api repos/OWNER/REPO/actions/runs/<id>/attempts/1/jobs\`

## Read ALL THREE comment surfaces
\`gh pr view N --comments\` (issue-level), \`gh api repos/OWNER/REPO/pulls/N/comments\` (inline), \`gh api repos/OWNER/REPO/pulls/N/reviews\` plus replies. Green checks are not consent, and a finding raised in a thread reply is still a finding.`

phase('Assess')

const PR_SCHEMA = {
  type: 'object',
  required: ['pr', 'draft', 'mergeable', 'checksGreen', 'failingChecks', 'verdictRendered', 'verdictSummary', 'openFindings', 'infraFailures', 'realFailures'],
  properties: {
    pr: { type: 'integer' },
    draft: { type: 'boolean' },
    mergeable: { type: 'string' },
    checksGreen: { type: 'boolean', description: 'code lanes only — review lanes are assessed separately' },
    failingChecks: { type: 'array', items: { type: 'string' } },
    verdictRendered: { type: 'boolean', description: 'a completed run AND a real verdict in the comment body' },
    verdictSummary: { type: 'string' },
    openFindings: {
      type: 'array',
      items: {
        type: 'object',
        required: ['finding', 'surface', 'addressed'],
        properties: {
          finding: { type: 'string' },
          surface: { type: 'string', description: 'issue-level | inline | review | thread reply' },
          addressed: { type: 'string', description: 'fixed in branch (cite the commit) | declined with reason | OPEN' },
        },
      },
    },
    infraFailures: { type: 'array', items: { type: 'string' }, description: 'failures NOT caused by this branch' },
    realFailures: { type: 'array', items: { type: 'string' }, description: 'failures this branch must fix' },
  },
}

const assessments = await parallel(PRS.map((n) => () => agent(`${RULES}

Assess **PR #${n}** and report its true state.

Gather: draft status, mergeable state, every check with its conclusion, and all three comment surfaces.

**Classify every failure as INFRA or REAL — this is the field that decides what happens next:**

- **INFRA** — not caused by this branch. TLS certificate altname errors, OIDC token fetch failures, jobs queued with no runner assigned, a review lane that never renders a verdict, a shim propagating another workflow's failure. These need waiting or a re-run, never a code change.
- **REAL** — this branch must change. A failing test, a vet/lint error, a reviewer's finding, a merge conflict.

When you cannot tell, say so explicitly and put it in neither list. A misclassified infra failure wastes a fix; a misclassified real failure wastes hours of sleeping.

For **openFindings**, list every finding from every surface with whether it is fixed (cite the commit), explicitly declined (with the reason), or still OPEN. A finding nobody answered is open even if the PR is green.

If \`review / pr-review\` is green but no verdict rendered, set verdictRendered=false and say which of the three situations it looks like. Attempt the edit-history recovery before concluding a verdict is lost.`,
  { schema: PR_SCHEMA, phase: 'Assess', label: `assess:#${n}` })))

phase('Decide')

const DECIDE_SCHEMA = {
  type: 'object',
  required: ['perPR', 'sleepSeconds', 'sleepReason', 'escalate'],
  properties: {
    perPR: {
      type: 'array',
      items: {
        type: 'object',
        required: ['pr', 'state', 'nextAction', 'readyForReview', 'blockedBy'],
        properties: {
          pr: { type: 'integer' },
          state: { type: 'string', description: 'DONE | FIX | WAIT | ASK' },
          nextAction: { type: 'string', description: 'the single next thing to do' },
          readyForReview: { type: 'boolean', description: 'if draft: should it be marked ready now?' },
          blockedBy: { type: 'string' },
        },
      },
    },
    sleepSeconds: { type: 'integer', description: 'how long before the next babysit turn' },
    sleepReason: { type: 'string' },
    escalate: { type: 'string', description: 'what to put to a human, or "nothing"' },
  },
}

const decision = await agent(`${RULES}

Decide the next move for each PR, and how long to wait before looking again.

ASSESSMENTS (attempt ${ATTEMPT}):
${JSON.stringify(assessments.filter(Boolean), null, 2)}

Per PR, set **state**:
- **DONE** — code checks green, a verdict RENDERED, and every finding fixed or explicitly declined. All three. Green alone is not DONE.
- **FIX** — there are realFailures or open findings. Name the single next action.
- **WAIT** — only infra failures, or checks still running. Nothing to do but wait.
- **ASK** — genuinely ambiguous, or a decision that is not yours: whether to merge, whether to drop a severable PR, whether an unclassifiable failure is infra.

**readyForReview**: a draft PR whose code checks are green and which has something coherent to look at should usually be marked ready — early feedback is cheaper than late rework. Say so when it applies.

**sleepSeconds** — the backoff, and be deliberate:
- REAL failures present → small (60-300s). There is work to do; sleeping does nothing.
- Checks genuinely running → moderate (300-900s).
- INFRA failures, attempt ${ATTEMPT} → back off progressively. Roughly: attempt 1-2 → 900s, 3-4 → 3600s, 5+ → up to **28800s (8 hours)**. These faults recover on their own timescale, and hammering them burns runner capacity other sessions are queued behind.
- Everything DONE → 0.

**escalate** — put to a human anything that is a decision rather than a task: a merge, dropping a severable PR, a finding you disagree with, or infra that has not recovered after several long waits. When in doubt, escalate: a needless question costs a message, a wrong autonomous call costs a revert.`,
  { schema: DECIDE_SCHEMA, phase: 'Decide', label: 'decide' })

const done = decision.perPR.filter((p) => p.state === 'DONE').length
log(`${done}/${decision.perPR.length} done · sleep ${decision.sleepSeconds}s · ${decision.escalate !== 'nothing' ? 'ESCALATE' : 'no escalation'}`)

return { decision, assessments: assessments.filter(Boolean) }
