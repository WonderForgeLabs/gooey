export const meta = {
  name: 'gooey-epic-decompose',
  description: 'Turn a design doc into a project-ops epic: child issues tied to doc sections, xrefs written back into the doc',
  whenToUse: 'When a decision record or design doc in docs/ should become tracked work: an epic issue plus child issues, each anchored to the doc section it implements, the section cross-referencing its issue, everything on the gooey board. Callable standalone or from another workflow (gooey-new-component calls it after the spec is written). Interactive: the user approves the decomposition before anything is filed.',
  phases: [
    { title: 'Plan', detail: 'decompose the doc into per-section work items, user approves' },
    { title: 'File', detail: 'epic + child issues, board add, fields, dependencies' },
    { title: 'Xref', detail: 'write issue links back into the doc sections' },
  ],
}

// Doc -> epic decomposition, project-ops style. Reusable: run it on any
// spec/design doc, or let gooey-new-component invoke it after the spec
// lands. Nothing is filed until the user approves the plan.
//
//   Workflow({ name: 'gooey-epic-decompose',
//              args: { doc: 'docs/specs/2026-08-10-foo.md' } })
//
// args:
//   doc      (required) repo-relative path of the doc to decompose
//   title    (optional) epic title; defaults to "Epic: <doc's H1>"
//   context  (optional) extra framing from a calling workflow
//
// GitHub issues are outward-facing state: this workflow files them only
// after the user approves the exact list in Plan. It edits the doc (file
// edit only) and returns a staging list; the coordinator owns the index.
const REPO = (typeof args === 'object' && args?.repo) || '/home/elan/repos/WonderForgeLabs/gooey'
const DOC = (typeof args === 'object' && args?.doc) || null
const TITLE = (typeof args === 'object' && args?.title) || null
const CONTEXT = (typeof args === 'object' && args?.context) || ''

if (!DOC) return { aborted: 'args.doc is required — the repo-relative doc to decompose' }

const GIT_RULES = `
GIT & FILE DISCIPLINE (hard rules):
- NEVER run mutating git: no add/commit/push/stash/checkout/restore/reset, no \`git mv\`/\`git rm\` — plain \`mv\`/\`rm\` only. The coordinator that invoked this workflow owns the index. Read-only git (status/diff/log/ls-files) is fine. Read-only \`gh\` (issue view/list, project item queries) is fine in EVERY phase — Plan is expected to read the house model issues. MUTATING \`gh\` — issue create/edit, project item add/edit — is this workflow's job but only in the File/Xref phases, never during Plan.
- Never \`git add -A\` — never \`git add\` at all.
- Build nothing into the repo; anything you must compile goes to /tmp.
- \`git status\` collapses untracked directories; report staging candidates as explicit file paths from \`git status --porcelain -uall\`, never directories.
- Other sessions write this repo concurrently: re-read state in the turn you assert it.`

const INTERACTION_RULES = `
YOU ARE INTERACTIVE — this is a human-in-the-loop workflow:
- Ask the user with the AskUserQuestion tool (load via ToolSearch "select:AskUserQuestion" if missing from your tool list). Up to 4 questions per call, 4 options each, automatic "Other" free text.
- Never assume an answer the user has not given; never mark your own plan approved. If you cannot reach the user, return with approved=false and your open questions.`

const HOUSE_EPIC = `
HOUSE EPIC CONVENTIONS (verified against the live repo — spot-check with gh before filing):
- Epic: label "epic" + "enhancement", title "Epic: <scope>", body states the scope and the house rules the children follow, ends with a task list (- [ ] #NNN per child, added after children exist) so GitHub tracks progress. Model: gh issue view 72.
- Child: plain issue, body says what and why, quotes its doc section, and ends with "Child of <epic title> #<epic>". Model: gh issue view 166.
- Attribution footer on every issue body: "🤖 Filed by gooey-epic-decompose via Claude Code".
- BOARD HAS NO AUTO-ADD: read ${REPO}/.claude/project-ops.yaml for project_id and field/option ids, then for EVERY issue (epic + children) run addProjectV2ItemById BEFORE setting fields. Set Status=Todo, Priority per the approved plan, and Design Doc = the doc path. Field ids in that yaml are per-board and current; do not copy ids from anywhere else.
- Blocking relationships between children: invoke the Skill tool with skill "project-ops:manage-dependencies" and follow it rather than inventing a mechanism.`

const PLAN_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['approved', 'epicTitle', 'epicBody', 'items', 'userNotes'],
  properties: {
    approved: { type: 'boolean', description: 'true only if the user explicitly approved this exact list' },
    epicTitle: { type: 'string' },
    epicBody: { type: 'string' },
    items: {
      type: 'array',
      items: {
        type: 'object', additionalProperties: false,
        required: ['title', 'section', 'anchor', 'body', 'priority', 'blockedBy'],
        properties: {
          title: { type: 'string' },
          section: { type: 'string', description: 'exact heading text of the doc section this item implements' },
          anchor: { type: 'string', description: 'GitHub anchor slug for that heading' },
          body: { type: 'string', description: 'issue body: the work, the section quote, acceptance criteria' },
          priority: { type: 'string', description: 'board Priority option name, e.g. "P1 - High"' },
          blockedBy: { type: 'array', items: { type: 'integer' }, description: 'indices into items[] that must land first' },
        },
      },
    },
    userNotes: { type: 'string', description: 'anything the user said that changes filing or xref behavior' },
  },
}

const FILE_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['epic', 'issues', 'boardOk', 'notes'],
  properties: {
    epic: { type: 'integer', description: 'epic issue number' },
    issues: {
      type: 'array',
      items: {
        type: 'object', additionalProperties: false,
        required: ['number', 'title', 'section', 'anchor'],
        properties: {
          number: { type: 'integer' },
          title: { type: 'string' },
          section: { type: 'string' },
          anchor: { type: 'string' },
        },
      },
    },
    boardOk: { type: 'boolean', description: 'every issue added to the board with fields set' },
    notes: { type: 'string' },
  },
}

const XREF_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['docEdited', 'stagingList', 'notes'],
  properties: {
    docEdited: { type: 'boolean' },
    stagingList: { type: 'array', items: { type: 'string' }, description: 'explicit file paths, from git status --porcelain -uall' },
    notes: { type: 'string' },
  },
}

// ---------------------------------------------------------------------------

phase('Plan')
let plan = null
let feedback = ''
for (let round = 1; round <= 5; round++) {
  plan = await agent(
    `You are planning a project-ops epic decomposition of ${REPO}/${DOC}.${CONTEXT ? ` Calling context: ${CONTEXT}` : ''}${feedback ? ` The user rejected the previous plan round — their feedback: ${JSON.stringify(feedback)}. Address it.` : ''}
Read the doc fully. Decompose the work it describes into child issues, each tied to the ACTUAL doc section (## / ### heading) it implements — an item that cannot name its section is not decomposed enough or the doc has a gap (tell the user which). Derive each anchor the way GitHub slugs headings (lowercase, spaces to hyphens, punctuation dropped). Draft the epic (title ${TITLE ? `"${TITLE}"` : 'from the doc\'s H1'}, body per house convention) and each child body: what to build, a short quote of its section, the section link as ${DOC}#<anchor>, acceptance criteria, "Child of <epic> #<n>" placeholder, priority, and blocking order.
Also read ${REPO}/.claude/project-ops.yaml so priorities you propose are real board options, and gh issue view 72 / 166 for the house shape.
Then put the WHOLE plan in front of the user with AskUserQuestion — epic summary plus the item list (title -> section, priority, blockers) — and iterate on their edits. approved=true only when they explicitly approve the exact list. DO NOT create any issue in this phase.
${INTERACTION_RULES}${GIT_RULES}`,
    { label: `plan:round-${round}`, phase: 'Plan', schema: PLAN_SCHEMA, agentType: 'general-purpose', effort: 'high' },
  )
  if (!plan) return { aborted: `plan round ${round} skipped/died` }
  if (plan.approved) break
  if (!plan.userNotes) return { aborted: 'plan not approved and no user feedback captured — user unreachable', plan }
  feedback = plan.userNotes
}
if (!plan.approved) return { aborted: 'plan not approved after 5 rounds', plan }

phase('File')
const filed = await agent(
  `File the user-APPROVED epic decomposition for ${REPO}/${DOC}. The plan (file exactly this — the user approved this list, not your improvements): ${JSON.stringify(plan)}
Order of operations:
1. gh issue create the epic (title/body/labels per plan + house convention).
2. Create each child issue, substituting the real epic number into "Child of ... #<n>"; keep a map index->number for blockedBy.
3. Edit the epic body to append the task list: - [ ] #<child> for each, in plan order.
4. Board: per the convention below, add EVERY issue and set Status/Priority/Design Doc.
5. Wire blockedBy relationships via the project-ops dependency skill.
6. Re-read each created issue (gh issue view) to confirm body, labels, and board fields landed; boardOk=false with notes if anything is off.
${HOUSE_EPIC}${GIT_RULES}`,
  { label: 'file:issues', phase: 'File', schema: FILE_SCHEMA, effort: 'high' },
)
if (!filed) return { aborted: 'filing agent skipped/died AFTER plan approval — check gh for partially created issues before re-running', plan }

phase('Xref')
const xref = await agent(
  `Write the cross-references back into ${REPO}/${DOC}. Issues just filed: ${JSON.stringify(filed)}
For each issue, add under its section heading (directly after the heading line) a tracking line in house style: "> Tracked: [#<n>](https://github.com/WonderForgeLabs/gooey/issues/<n>)" — one line per issue, no duplicates if a line for that issue already exists (re-read the doc NOW, another session may have touched it). Also add the epic reference near the doc's title the way docs/specs/2026-08-10-tabs.md carries "(issue #166, child of toolkit epic #72)" in its H1 — adapt, don't clone.
File edits only — no git. Then run git status --porcelain -uall and return the explicit paths this workflow touched as stagingList.
${GIT_RULES}`,
  { label: 'xref:doc', phase: 'Xref', schema: XREF_SCHEMA },
)

return {
  doc: DOC,
  epic: filed.epic,
  issues: filed.issues,
  boardOk: filed.boardOk,
  xref: xref || 'xref agent skipped — doc lacks issue backlinks; re-run Xref by hand',
  staging: xref?.stagingList || [],
  note: 'Issues are live on GitHub. The doc edit is uncommitted; the coordinator stages the paths above and commits.',
}
