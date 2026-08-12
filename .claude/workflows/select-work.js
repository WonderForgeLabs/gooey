export const meta = {
  name: 'select-work',
  description: 'Pick ONE project off the board by value-per-risk, decide whether it needs a decision record, and shape the PR stack',
  whenToUse: 'When you have a backlog and need to choose what to work on next — not when the work is already chosen. Produces one issue number, an ADR/DDR verdict, an ordered PR stack, and the CI surface the change must update.',
  phases: [
    { title: 'Recon', detail: 'value ranking + CI surface, in parallel' },
    { title: 'Select', detail: 'one project, docs-first verdict, PR stack' },
  ],
}

// Selection is a judgement, so this workflow's job is to make the
// judgement CHECKABLE rather than to automate it away: every candidate
// comes back with its score and rationale, not just the winner, so the
// coordinator can disagree with the ranking rather than only the result.
//
// Pass a conflict map from `peer-canvass` as args.conflicts — without it
// this will happily select work that collides with a peer.
//
//   Workflow({ name: 'select-work' })
//   Workflow({ name: 'select-work', args: { conflicts: {...}, exclude: [130, 206] } })
//
// Read-only. Chooses; does not start.
const REPO = (typeof args === 'object' && args?.repo) || '/home/elan/repos/WonderForgeLabs/gooey'
const SLUG = (typeof args === 'object' && args?.slug) || 'WonderForgeLabs/gooey'
const FINDWORK = (typeof args === 'object' && args?.findWorkScript) ||
  '/home/elan/.claude/plugins/cache/wonderforgelabs-project-ops/project-ops/0.9.1/scripts/find-unblocked-work.sh'
const CONFLICTS = (typeof args === 'object' && args?.conflicts)
  ? JSON.stringify(args.conflicts, null, 2)
  : '(no conflict map supplied — treat every area as UNKNOWN risk and say so in your risks)'
const EXCLUDE = (typeof args === 'object' && args?.exclude) || []

const RULES = `You are working in ${REPO} (GitHub: ${SLUG}).

HARD RULES:
- READ-ONLY. No edits, commits, pushes, PRs or issues.
- Never \`go build ./...\` if this is a Go repo — main packages write executables into the repo root.
- \`command grep\`, not \`grep\`.
- Verify every claim you repeat from an issue body. Issue text goes stale; the tree does not.
- Say "I did not check this" rather than implying you did.`

phase('Recon')

const RANK_SCHEMA = {
  type: 'object',
  required: ['candidates'],
  properties: {
    candidates: {
      type: 'array',
      items: {
        type: 'object',
        required: ['issue', 'title', 'valueScore', 'rationale', 'inThisRepo', 'needsDecisionRecord', 'recordReason', 'estimatedPRs', 'blockedBy'],
        properties: {
          issue: { type: 'integer' }, title: { type: 'string' },
          valueScore: { type: 'integer', description: '1-10, value delivered per unit of risk' },
          rationale: { type: 'string' },
          inThisRepo: { type: 'boolean', description: 'false if the actual fix lives in another repo' },
          needsDecisionRecord: { type: 'boolean' },
          recordReason: { type: 'string' },
          estimatedPRs: { type: 'integer' },
          blockedBy: { type: 'string' },
        },
      },
    },
  },
}

const RANK_PROMPT = `${RULES}

Rank the unblocked backlog by VALUE — value delivered per unit of risk, not by priority label.

Candidates: \`${FINDWORK}\`${EXCLUDE.length ? `\n\nEXCLUDE these issues, already claimed: ${EXCLUDE.join(', ')}` : ''}

For the top ~12, read each with \`gh issue view N\` and judge:

1. **valueScore 1-10.** High: unblocks other people or agents, removes a silent-failure class, or ships something asked for. Low: polish, or a hypothetical beneficiary. Weigh how many people or sessions are exposed to the problem *today*.
2. **inThisRepo.** Does the actual fix live here? An issue whose fix is in another repo scores low for this repo's agents no matter how important — they cannot do it. Check, do not assume: a workflow file here may be a thin shim calling a reusable workflow elsewhere.
3. **needsDecisionRecord.** Does this need an ADR/DDR *before* code? Read a few existing records in \`docs/specs/\` (or equivalent) to calibrate. Rule of thumb: a new public API, a new module, a cross-cutting contract, or a decision with more than one defensible answer earns a record. A fix with one obvious shape does not — and a record that restates a verified fact is a document that decides nothing.
4. **estimatedPRs** — how many PRs done properly, with tests and docs? >1 needs a stacked chain.
5. **blockedBy** — anything real, including "blocked on a PR in another repo that is currently red".

Return every candidate assessed, with its score and reasoning — not just your favourite. The coordinator needs to be able to disagree with the ranking.`

const CI_SCHEMA = {
  type: 'object',
  required: ['checks', 'required', 'gaps'],
  properties: {
    checks: {
      type: 'array',
      items: {
        type: 'object',
        required: ['name', 'verifies', 'whenToUpdate'],
        properties: { name: { type: 'string' }, verifies: { type: 'string' }, whenToUpdate: { type: 'string' } },
      },
    },
    required: { type: 'string', description: 'which checks are REQUIRED vs advisory — check branch protection and rulesets and report what you actually find, including "none"' },
    gaps: { type: 'array', items: { type: 'string' }, description: 'ways CI reports success while covering less than it claims' },
  },
}

const CI_PROMPT = `${RULES}

Map the CI surface, so whoever takes this work knows what they must update alongside it.

Read every workflow file, not just the main one. For each check: what it actually verifies, and when a change would require updating it.

Hunt specifically for:
- lint / format gates
- **freshness** checks (generated code vs source, e.g. codegen + \`git diff --exit-code\`) and what they cover — often narrower than assumed
- docs or render-manifest checks
- per-module or per-package loops, and whether they DISCOVER their targets or ENUMERATE them (an enumerated list silently skips anything added later)
- **required vs advisory**: \`gh api repos/${SLUG}/branches/main/protection\` and \`.../rulesets\`. Report exactly what you find. "No required checks" is a critical finding, not a footnote — it means every green tick is advisory.

Then list **gaps**: places CI can report success while covering less than it claims. Find them by reading the workflow logic, not by recalling known issues. A guard whose failure branch cannot fail, a loop that matches nothing and exits 0, a step that swallows a failed sub-step — these are the shapes.`

const [ranking, ciSurface] = await parallel([
  () => agent(RANK_PROMPT, { schema: RANK_SCHEMA, phase: 'Recon', label: 'value-ranking' }),
  () => agent(CI_PROMPT, { schema: CI_SCHEMA, phase: 'Recon', label: 'ci-surface' }),
])

if (!ranking || !ciSurface) {
  log(`WARNING: recon incomplete — ranking=${ranking ? 'ok' : 'FAILED'}, ci-surface=${ciSurface ? 'ok' : 'FAILED'}. Selection below rests on partial data; treat it as provisional.`)
}

phase('Select')

const SELECT_SCHEMA = {
  type: 'object',
  required: ['chosenIssue', 'chosenTitle', 'why', 'needsDecisionRecord', 'docPlan', 'prStack', 'ciWork', 'doNotTouch', 'risks', 'runnerUp'],
  properties: {
    chosenIssue: { type: 'integer' },
    chosenTitle: { type: 'string' },
    why: { type: 'string' },
    needsDecisionRecord: { type: 'boolean' },
    docPlan: { type: 'string', description: 'what the record must DECIDE and where it goes — or why none is needed' },
    prStack: {
      type: 'array',
      items: {
        type: 'object',
        required: ['order', 'title', 'scope', 'dependsOn', 'severable'],
        properties: {
          order: { type: 'integer' }, title: { type: 'string' }, scope: { type: 'string' },
          dependsOn: { type: 'string' },
          severable: { type: 'boolean', description: 'can this PR be dropped without losing the others?' },
        },
      },
    },
    ciWork: { type: 'array', items: { type: 'string' } },
    doNotTouch: { type: 'array', items: { type: 'string' }, description: 'paths that would break someone else — with the consequence' },
    risks: { type: 'array', items: { type: 'string' } },
    runnerUp: { type: 'string' },
  },
}

const selection = await agent(`${RULES}

Select exactly ONE project. Synthesize the reports below, and **re-verify the deciding claims yourself** — do not inherit a conclusion you can check in a minute.

VALUE RANKING:
${JSON.stringify(ranking, null, 2)}

CI SURFACE:
${JSON.stringify(ciSurface, null, 2)}

CONFLICT MAP:
${CONFLICTS}

Selection rules, in order:
1. **Doable in THIS repo.** Otherwise it is not a candidate, however valuable. Say so and move on.
2. **No collision** with the conflict map. If the highest-value item collides, say so EXPLICITLY and pick the best non-colliding item — never substitute silently.
3. **Highest value per unit of risk** among what remains.
4. **Coherent as one project** — one thing done properly, not a grab-bag.

Then produce:
- **needsDecisionRecord / docPlan.** If yes: the record is a SEPARATE FIRST PR containing only documentation, which must be green and human-approved before any code. Name what it must actually decide.
- **prStack** — ordered, element 1 merges first, each scope tight enough to review alone. Mark which are **severable** so a reviewer can drop one without losing the rest.
- **ciWork** — which lint/freshness/manifest checks this change must update. If the answer is "none", say that *and* flag it: a change nothing verifies needs a human reader.
- **doNotTouch** — paths that would break someone else, with the consequence spelled out. A file that is another PR's entire diff belongs here.
- **risks** — including any way this change could go green without being verified.

Be decisive: one issue. If everything valuable is blocked or colliding, say that rather than forcing a pick.`,
  { schema: SELECT_SCHEMA, phase: 'Select', label: 'select-project' })

log(`#${selection.chosenIssue}: ${selection.chosenTitle} — record=${selection.needsDecisionRecord}, ${selection.prStack.length} PR(s), ${selection.doNotTouch.length} hands-off`)

return { selection, ranking, ciSurface }
