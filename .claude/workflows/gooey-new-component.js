export const meta = {
  name: 'gooey-new-component',
  description: 'Interview-first component pipeline: spec, live-harness design loop, build, then reconcile every doc and adopter it touches',
  whenToUse: 'When the user wants a new framework component in components/. Interactive: interviews the user, iterates a live MCP-driven prototype until they approve, writes the decision record first, files a project-ops epic from it, builds with damage pins and load-error cases, then fans out to reconcile stale docs and adopt the component wherever hand-rolled equivalents exist (the Tabs/kanbandemo model). Not for demos (use gooey-new-demo) or small fixes.',
  phases: [
    { title: 'Interview', detail: 'brainstorm with the user until the component is decided' },
    { title: 'Explore', detail: 'component idioms, existing pressure/adopters, spec contracts' },
    { title: 'Design', detail: 'build-run-inspect prototype loop over MCP, looped on user feedback' },
    { title: 'Spec', detail: 'docs/specs decision record before any implementation' },
    { title: 'Epic', detail: 'gooey-epic-decompose files tracked issues from the spec' },
    { title: 'Build', detail: 'components/ + markup builder + load errors + damage pins' },
    { title: 'Reconcile', detail: 'update stale docs, adopt in shipped code, docs-and-demos' },
    { title: 'Verify', detail: 'all modules, all examples, links, tracked files, staging list' },
  ],
}

// New-component pipeline. INTERACTIVE — agents interview the user, and
// nothing is implemented until they approve a live prototype and the
// decision record exists. The reconciliation fan-out afterwards is the
// point: a component that ships without updating its docs and adopters
// leaves the repo lying about itself.
//
//   Workflow({ name: 'gooey-new-component' })
//   Workflow({ name: 'gooey-new-component', args: { idea: 'a DatePicker' } })
//
// Calls gooey-epic-decompose after the spec, and (user-gated) the
// gooey-docs-and-demos regen during reconciliation. Because of that
// nesting, run this workflow top-level only.
//
// The coordinator that invoked this workflow owns the git index: this
// workflow returns an explicit staging list and NEVER commits.
const REPO = (typeof args === 'object' && args?.repo) || '/home/elan/repos/WonderForgeLabs/gooey'
const IDEA = (typeof args === 'object' && args?.idea) || null

const GIT_RULES = `
GIT & FILE DISCIPLINE (hard rules):
- NEVER run mutating git: no add/commit/push/stash/checkout/restore/reset, and no \`git mv\`/\`git rm\` — plain \`mv\`/\`rm\` only. The coordinator that invoked this workflow owns the index. Read-only git (status/diff/log/ls-files) is fine.
- Never \`git add -A\` — never \`git add\` at all.
- Build binaries to /tmp only (go build -o /tmp/...). Never build into the repo; \`go build ./...\` at the root drops executables next to tracked ones — use \`go vet ./...\` for whole-repo checks.
- Cross-worktree or gitignored-path searches: use \`command grep\` — the plain grep wrapper honors .gitignore and silently skips .claude/worktrees/.
- \`git status\` collapses an untracked directory to one line, hiding files inside it. When reporting files for staging, list explicit file paths from \`git status --porcelain -uall\`, never directories.
- Other sessions write this repo concurrently: re-read state (git status, file contents) in the turn you assert it; never report from memory of an earlier step.`

const INTERACTION_RULES = `
YOU ARE INTERACTIVE — this is a human-in-the-loop workflow:
- Ask the user real questions with the AskUserQuestion tool (if it is not in your tool list, load it first: ToolSearch "select:AskUserQuestion"). Concrete options with tradeoffs beat open prose. Limits: up to 4 questions per call, 4 options each, plus an automatic "Other" free-text escape.
- When the user is choosing between visual or API alternatives, put side-by-side ASCII mockups (or screen_text captures) in each option's \`preview\` field (single-select questions only).
- Never assume an answer the user has not given, and never mark your own work approved. If you cannot reach the user, return your open questions in the structured result instead of guessing.`

const INVARIANTS = `
FRAMEWORK INVARIANTS — the checklist the build must honor EXPLICITLY (violating one is a defect, not a style choice):
- No reflection anywhere in the framework or components.
- Property subscription happens at the Get call site: dependencies are recorded by the Get that actually runs. Hoist Gets above early returns; never short-circuit past one (\`a || b.Get()\` silently drops the b dependency when a is true). prop.Set never compares values — a same-value Set still notifies.
- Containers paint their own chrome; parents never pre-clear a child's cells.
- Anything Startable must close-and-join: close(done) alone lets one tick post after Close — pair it with <-stopped or lifetime tests flake in CI.
- Event/command fields are gooey.Action, not bare func(); test presence with gooey.CanExecute, never != nil. Follow the wave-1 rules: absent handler is inert, false CanExecute paints dim and refuses, and the condition is read WHILE painting so a flip repaints the right node.
- Component properties are *prop.Property[T]; focus via FocusState, hover via HoverState; bindable markup attrs ride the existing boundProp machinery, never a new registry.
- Markup contract: a builder for the element, and LOAD ERRORS (not silent tolerance) for every malformed use — model the error contract and its tests on the Tabs entry (components/tabs*.go + markup/tabs_test.go).
- Damage-count tests pin repaint behavior: idle, hover, focus move, state change — exact paint counts, in components/<name>_test.go, following components/tabs_test.go.
- docs/specs/2026-08-10-*.md are the contracts for their subsystems — read the ones adjacent to what you are building before writing code.`

// The build-run-inspect harness is this workflow's core mechanic: the
// component does not exist yet, so every design round compiles a scratch
// app hosting the in-progress prototype and drives it over MCP.
const HARNESS = `
BUILD-RUN-INSPECT HARNESS (the core mechanic — every design round goes through it):
- Scratch module OUTSIDE the repo at /tmp/gooey-proto-<component>/: own go.mod (module scratch/proto) requiring github.com/WonderForgeLabs/gooey and github.com/WonderForgeLabs/gooey/mcp, with replace directives at ${REPO} and ${REPO}/mcp. mcp/ is a nested module — anything importing it needs its own module (see the header comment in ${REPO}/examples/kanbandemo/go.mod).
- The prototype component lives IN THE SCRATCH MODULE (a local package), not in the repo: implement it against the real gooey interfaces (Component/Base, FocusState, HoverState, *prop.Property[T]) so the eventual move into components/ is a copy, not a rewrite. Register a scratch markup builder for it if the markup shape is under design; hosting it from Go composition is fine for early rounds.
- Host app modeled on ${REPO}/mcp/cmd/mcpdemo/main.go: a .gooey page (dev watcher, hot reload) exercising the prototype in realistic surroundings, viewmodel of prop handles, one mcp.Serve(app, mcp.Options{Addr: "127.0.0.1:<port>", Context: ctx}) call. Pick a free port (not 7777).
- Loop: edit prototype -> go build -o /tmp/gooey-proto-<component>/proto . -> run under script -qec "stty cols 110 rows 32; .../proto" /dev/null in the background (the stty MUST set a size or the pty is 0x0 and paints nothing) -> drive over MCP -> screen_text -> judge -> kill -> edit again. Restarts are seconds; do many small loops, not one big one.
- Drive it over MCP streamable HTTP at http://127.0.0.1:<port>/mcp. Plain curl JSON-RPC works: initialize, notifications/initialized (carry the Mcp-Session-Id header), then tools/call — read ${REPO}/mcp/e2e_linux_test.go for the exact wire sequence, and leave a drive.sh wrapper in the scratch dir so later rounds reuse it.
- The tools: screen_text (your screenshot — quote it in results and AskUserQuestion previews), tree_snapshot, swap_markup / patch_markup / validate_markup, send_keys, send_mouse, focus, set_value / list_values, invoke_command, list_styles, register_properties.
- Exercise the component's whole surface each round you change it: focus in/out (tab), its key map, hover + click + wheel via send_mouse, disabled state, and its behavior inside a Grid/stack with neighbors. Kill your app instance and its script wrapper before you exit; leave the scratch dir in place for the next round.`

const INTERVIEW_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['decided', 'name', 'problem', 'pressure', 'apiSketch', 'markupSketch', 'scopeCuts', 'openQuestions'],
  properties: {
    decided: { type: 'boolean', description: 'true only if the user explicitly picked a direction' },
    name: { type: 'string', description: 'component name, e.g. DatePicker' },
    problem: { type: 'string', description: 'what UI problem it solves, in the user\'s words' },
    pressure: { type: 'array', items: { type: 'string' }, description: 'existing hand-rolled equivalents in the repo that prove the need (file paths)' },
    apiSketch: { type: 'string', description: 'the Go API direction the user picked' },
    markupSketch: { type: 'string', description: 'the markup element/attribute direction the user picked' },
    scopeCuts: { type: 'array', items: { type: 'string' }, description: 'explicitly out of scope ("Not here" candidates)' },
    openQuestions: { type: 'array', items: { type: 'string' } },
  },
}

const DESIGN_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['verdict', 'artifactUrl', 'harnessDir', 'prototypeFiles', 'designSummary', 'decisions', 'screenText', 'userFeedback'],
  properties: {
    verdict: { type: 'string', enum: ['approved', 'revise', 'restart-interview'], description: 'the USER\'s verdict from AskUserQuestion, never your own' },
    artifactUrl: { type: 'string', description: 'published mockup artifact URL ("" if none)' },
    harnessDir: { type: 'string' },
    prototypeFiles: { type: 'array', items: { type: 'string' }, description: 'the prototype source files in the scratch module' },
    designSummary: { type: 'string', description: 'the component as designed: anatomy, states, key map, mouse behavior, sizing' },
    decisions: { type: 'array', items: { type: 'string' }, description: 'each design decision WITH its rationale — these become the spec\'s decision sections' },
    screenText: { type: 'string', description: 'final screen_text snapshot of the prototype this round' },
    userFeedback: { type: 'string', description: 'verbatim gist of what the user asked to change ("" when approved)' },
  },
}

const SPEC_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['specPath', 'confirmed', 'sections'],
  properties: {
    specPath: { type: 'string', description: 'repo-relative path of the decision record' },
    confirmed: { type: 'boolean', description: 'user confirmed the record captures the approved design' },
    sections: { type: 'array', items: { type: 'string' } },
  },
}

const BUILD_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['files', 'tests', 'invariantChecklist', 'notes'],
  properties: {
    files: { type: 'array', items: { type: 'string' }, description: 'repo-relative paths written' },
    tests: { type: 'array', items: { type: 'string' }, description: 'test names and the exact behavior each pins (damage counts included)' },
    invariantChecklist: {
      type: 'array',
      items: {
        type: 'object', additionalProperties: false,
        required: ['rule', 'how'],
        properties: {
          rule: { type: 'string' },
          how: { type: 'string', description: 'where/how this build honors it — file:line or test name' },
        },
      },
      description: 'one entry per invariant in the brief, none skipped',
    },
    notes: { type: 'string' },
  },
}

const SURVEY_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['findings'],
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object', additionalProperties: false,
        required: ['path', 'what', 'kind'],
        properties: {
          path: { type: 'string', description: 'file or dir the change made stale / that should adopt' },
          what: { type: 'string', description: 'the stale claim or the hand-rolled equivalent, specifically' },
          kind: { type: 'string', enum: ['doc-stale', 'adopt'], description: 'doc-stale: a doc now under- or over-claims; adopt: shipped code that should use the new component' },
        },
      },
    },
  },
}

const RECONCILE_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['owned', 'changed', 'skipped', 'notes'],
  properties: {
    owned: { type: 'array', items: { type: 'string' }, description: 'the write set this agent was assigned' },
    changed: { type: 'array', items: { type: 'string' }, description: 'files actually edited (must be within owned)' },
    skipped: { type: 'array', items: { type: 'string' }, description: 'assigned findings judged NOT stale/adoptable, with a reason each' },
    notes: { type: 'string' },
  },
}

const REGEN_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['scriptUpdated', 'runRegen', 'notes'],
  properties: {
    scriptUpdated: { type: 'boolean', description: 'docs-and-demos.js needed and received updates' },
    runRegen: { type: 'boolean', description: 'user chose to run the full gooey-docs-and-demos regen now' },
    notes: { type: 'string' },
  },
}

const VERIFY_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['green', 'checks', 'problems', 'stagingList', 'stray'],
  properties: {
    green: { type: 'boolean' },
    checks: { type: 'array', items: { type: 'string' } },
    problems: { type: 'array', items: { type: 'string' } },
    stagingList: { type: 'array', items: { type: 'string' }, description: 'explicit file paths from git status --porcelain -uall belonging to this work' },
    stray: { type: 'array', items: { type: 'string' }, description: 'untracked junk that must NOT be staged' },
  },
}

// ---------------------------------------------------------------------------

phase('Interview')
let interview = await agent(
  `You are running the Interview phase of the gooey-new-component workflow in ${REPO}: the user wants a new component${IDEA ? ` — their seed idea: "${IDEA}"` : ''} and YOUR job is to land a decided direction with them, not to design or build anything.
FIRST invoke the brainstorming process skill: call the Skill tool with skill "superpowers:brainstorming" and follow it — it owns this phase's process.
Ground yourself before asking anything: skim ${REPO}/components/ (what exists), docs/markup-reference.md, the toolkit epic (gh issue view 72), and search for hand-rolled equivalents of the idea across cmd/ examples/ docs/learn/examples/ (use command grep) — pressure sites are the strongest argument for a component and the Tabs precedent (kanbandemo's hand-rolled switcher, deleted on adoption) is the model.
Then interview the user (AskUserQuestion, multiple rounds as needed): what problem does it solve, what is the API direction (bound properties? commands? key map?), what is the markup shape, what is explicitly OUT of scope. Present competing API/markup sketches as concrete options with previews when the conversation narrows.
Do NOT proceed on assumptions: decided=true only when the user has explicitly picked a direction. If they are still torn, put what remains in openQuestions and set decided=false.
${INTERACTION_RULES}
${GIT_RULES}`,
  { label: 'interview', phase: 'Interview', schema: INTERVIEW_SCHEMA, agentType: 'general-purpose' },
)
if (!interview) return { aborted: 'interview agent skipped/died — nothing was decided or written' }

let design = null
let grounding = null
let interviewRounds = 1

while (!design) {
  if (!interview.decided) {
    return { aborted: 'no direction decided', openQuestions: interview.openQuestions, interview }
  }

  phase('Explore')
  grounding = (await parallel([
    () => agent(
      `Read-only exploration in ${REPO}. The component-author's idiom brief for building "${interview.name}": read component.go, base.go, components/ (pick the 2 closest components to this one and read them fully, plus components/tabs.go as the canonical modern example), how FocusState/HoverState/boundProp work, how a markup builder registers and what its load-error contract looks like (markup/ + markup/tabs_test.go), and what a damage-count test asserts (components/tabs_test.go). Return a compact idiom brief with exact names and file:line pointers a builder can trust.${GIT_RULES}`,
      { label: 'explore:idioms', phase: 'Explore' },
    ),
    () => agent(
      `Read-only exploration in ${REPO}. Find every existing hand-rolled equivalent or near-equivalent of a "${interview.name}" (${interview.problem}) across cmd/, examples/, docs/learn/examples/, and code embedded in docs pages (use command grep — the wrapper skips gitignored paths). User-claimed pressure sites to verify: ${JSON.stringify(interview.pressure)}. For each: path, how it works today, and whether adopting the new component would delete it (the kanbandemo/Tabs model) or is contract surface to leave alone (the cmd/reader model — its panes were deliberately not migrated to Tabs). Return a compact list.${GIT_RULES}`,
      { label: 'explore:pressure', phase: 'Explore' },
    ),
    () => agent(
      `Read-only exploration in ${REPO}/docs/specs/. Find the 2026-08-10-*.md specs whose contracts govern a new "${interview.name}" component (likely: toolkit waves, input-2, bindable-visibility, markup-declared-properties, styles-and-resources — but judge from the idea: ${interview.problem}). Extract the binding rules, dedup, cite spec file per rule. Also record the decision-record HOUSE STYLE from 2026-08-10-tabs.md and 2026-08-10-popup.md: section shapes, how decisions carry rationale, the "Not here" section, and the "## Executed" convention. Return a compact contract sheet + style note.${GIT_RULES}`,
      { label: 'explore:specs', phase: 'Explore' },
    ),
  ])).filter(Boolean).join('\n\n---\n\n')

  phase('Design')
  let feedback = []
  let harnessState = ''
  for (let round = 1; ; round++) {
    if (round > 8) {
      return {
        aborted: 'design did not converge in 8 rounds — resume with resumeFromRunId after regrouping with the user',
        interview, lastFeedback: feedback,
      }
    }
    const r = await agent(
      `You are running Design round ${round} of the gooey-new-component workflow in ${REPO}. Nothing ships from this phase — you are here to get a component design the USER approves, and the way you design is by BUILDING IT for real in a scratch harness and driving it.
THE DIRECTION (user-decided): ${JSON.stringify(interview)}
GROUNDING (from the explore agents; trust it, spot-check what you build on): ${grounding}
${feedback.length ? `PRIOR ROUNDS' USER FEEDBACK, oldest first — the current round exists because of the last entry, address it concretely: ${JSON.stringify(feedback)}` : 'This is the first round: produce the initial design and the first working prototype.'}
${harnessState ? `EXISTING HARNESS from earlier rounds (reuse it): ${harnessState}` : ''}
Process:
1. Invoke the Skill tool with skill "frontend-design:frontend-design" for aesthetic direction (terminal UI: hierarchy, density, and intentionality still apply in cells).
2. ${HARNESS}
3. Mockups the user can open: load the artifact-design skill (Skill tool) BEFORE writing the page, then publish an Artifact showing the design — the component's anatomy and states (captured screen_text, not drawings, wherever the prototype can show it), API surface, markup contract, key map. Load artifact-diagramming if you draw structural diagrams. Keep ONE artifact across rounds: same file path, and pass a previous round's artifactUrl as \`url\` so the link stays stable.
4. Put it in front of the user: AskUserQuestion with screen_text excerpts as previews where a visual/behavioral choice is being made, and finish with the gate question — approve this design / revise (say what) / rethink the premise (back to interview). Their answer is your verdict verbatim: approved | revise | restart-interview. NEVER return approved without the user having chosen it this round.
5. Record every decision WITH rationale in \`decisions\` — they become the spec's decision sections, in the house voice where a decision explains the alternatives it beat.
${INTERACTION_RULES}
${INVARIANTS}
${GIT_RULES}`,
      { label: `design:round-${round}`, phase: 'Design', schema: DESIGN_SCHEMA, agentType: 'general-purpose', effort: 'high' },
    )
    if (!r) return { aborted: `design round ${round} skipped/died`, interview, lastFeedback: feedback }
    harnessState = `dir=${r.harnessDir} prototype=${JSON.stringify(r.prototypeFiles)} artifact=${r.artifactUrl}`
    if (r.verdict === 'approved') { design = r; break }
    if (r.verdict === 'restart-interview') {
      log(`user sent design back to the interview after round ${round}`)
      interviewRounds++
      interview = await agent(
        `Re-run the Interview phase of gooey-new-component (round ${interviewRounds}) in ${REPO}. The user rejected the current premise during design. Prior direction: ${JSON.stringify(interview)}. Their feedback when rejecting: ${JSON.stringify(r.userFeedback)}. Invoke Skill "superpowers:brainstorming" again, then re-interview with AskUserQuestion — start from what the rejection revealed, not from zero. Same bar: decided=true only on an explicit user pick.${INTERACTION_RULES}${GIT_RULES}`,
        { label: `interview:round-${interviewRounds}`, phase: 'Interview', schema: INTERVIEW_SCHEMA, agentType: 'general-purpose' },
      )
      if (!interview) return { aborted: 'interview re-run skipped/died', lastDesignFeedback: r.userFeedback }
      break // outer while re-explores under the new direction
    }
    feedback.push(r.userFeedback)
  }
}

phase('Spec')
const spec = await agent(
  `Write the decision record for the approved "${interview.name}" component in ${REPO} — BEFORE any implementation exists. Path: docs/specs/$(date +%F)-<component-kebab-name>.md (run date +%F yourself; kebab-case the name).
House style — model it on docs/specs/2026-08-10-tabs.md and 2026-08-10-popup.md, whose shapes the explore agents recorded: Status line ("**Status:** approved, not yet executed."), Date, the design in one sentence, one section per DECISION with the rationale and the alternatives it beat, the markup contract (including every load error), the damage pins the tests will assert, a verification plan, and "Not here" from the scope cuts. Write it from the approved design, not your own preferences:
DIRECTION: ${JSON.stringify(interview)}
APPROVED DESIGN: ${JSON.stringify({ designSummary: design.designSummary, decisions: design.decisions, screenText: design.screenText })}
Then show the user the record (point them at the file; summarize the decision list) and ask ONE AskUserQuestion: does this capture what you approved? Iterate on corrections until confirmed. The "## Executed" section is NOT written now — the Verify phase appends it after the build proves out.
${INTERACTION_RULES}${GIT_RULES}`,
  { label: 'spec', phase: 'Spec', schema: SPEC_SCHEMA, agentType: 'general-purpose', effort: 'high' },
)
if (!spec) return { aborted: 'spec agent skipped/died', interview, design }
if (!spec.confirmed) return { aborted: 'user did not confirm the decision record', spec, interview, design }

phase('Epic')
// The spec becomes tracked work: gooey-epic-decompose has its own user
// gate before filing anything, so calling it unconditionally is safe.
const epic = await workflow('gooey-epic-decompose', {
  repo: REPO,
  doc: spec.specPath,
  context: `Called from gooey-new-component for the approved "${interview.name}" component. The build/reconcile/verify work happens in this same workflow run — decompose so each child issue maps to a spec section (API, markup contract, damage pins, adoption sites, docs), and note in the epic body that the epic tracks the work of THIS run plus any follow-ups.`,
})
if (epic?.aborted) log(`epic decomposition did not file: ${epic.aborted} — continuing; work is untracked`)

phase('Build')
const build = await agent(
  `Build the approved "${interview.name}" component for real in ${REPO}. The design is user-approved and the decision record at ${spec.specPath} is the contract — implement THAT, do not redesign.
DIRECTION: ${JSON.stringify(interview)}
APPROVED DESIGN: ${JSON.stringify({ designSummary: design.designSummary, decisions: design.decisions })}
PROTOTYPE (start from it — it is the approved behavior; harden, don't rewrite): ${harnessNote(design)}
GROUNDING: ${grounding}
Deliverables, per the spec:
- components/<name>.go (+ siblings as the idiom brief dictates), moved/adapted from the prototype.
- The markup builder, and a LOAD ERROR for every malformed use the spec's markup contract lists — with tests in markup/ mirroring markup/tabs_test.go.
- Damage-count pins in components/<name>_test.go asserting the exact counts the spec promises (idle, hover, focus, state change, the money interaction).
- Re-point the scratch harness at the REAL component (drop the local prototype package, import components) and drive the whole surface over MCP once more as a live smoke; capture screen_text into your notes.
- The invariantChecklist in your result must cover EVERY rule below with where/how it is honored — an empty or partial checklist fails the phase.
${INVARIANTS}
${GIT_RULES}`,
  { label: 'build', phase: 'Build', schema: BUILD_SCHEMA, effort: 'high' },
)
if (!build) return { aborted: 'build agent skipped/died', interview, design, spec, epic }

phase('Reconcile')
// Read-only survey first: what did this change make stale, and who
// should adopt it.
const survey = await parallel([
  () => agent(
    `Read-only survey in ${REPO}. The new "${interview.name}" component just landed in the working tree (files: ${JSON.stringify(build.files)}; spec ${spec.specPath}). Find every DOC whose claims are now stale — under- OR over-claiming: docs/markup-reference.md (missing element entry), docs/learn/** tutorials/how-tos/concepts (statements like "gooey has no X", lists of components, embedded code samples), README.md (capability matrix rows, status claims), docs/architecture.md, and every docs/specs/*.md whose "## Executed"/"Not here" sections this change touches (a spec that said "no X exists" or deferred X now under-claims). Read the actual claims; kind=doc-stale; \`what\` quotes the stale sentence. Use command grep for sweeps. Do NOT edit anything.${GIT_RULES}`,
    { label: 'survey:stale-docs', phase: 'Reconcile', schema: SURVEY_SCHEMA },
  ),
  () => agent(
    `Read-only survey in ${REPO}. Find every piece of SHIPPED CODE that should adopt the new "${interview.name}" component: cmd/ demos, examples/**, docs/learn/examples/**, and code embedded in docs pages, wherever a hand-rolled equivalent now exists. The explore phase found these candidates — re-verify each in the CURRENT tree (other sessions write concurrently) and finish the sweep: ${grounding.slice(0, 4000)}
The Tabs precedent is the bar: kanbandemo's hand-rolled switcher was deleted in the same PR that shipped Tabs — but cmd/reader's panes were contract surface and left alone. Judge each site: adopt (kind=adopt) or leave (omit, note why in \`what\` of a doc-stale finding only if a doc claims otherwise). Do NOT edit anything.${GIT_RULES}`,
    { label: 'survey:adopters', phase: 'Reconcile', schema: SURVEY_SCHEMA },
  ),
])
const findings = survey.filter(Boolean).flatMap(s => s.findings)
const staleDocs = findings.filter(f => f.kind === 'doc-stale')
const adopters = findings.filter(f => f.kind === 'adopt')
log(`reconcile survey: ${staleDocs.length} stale doc claims, ${adopters.length} adoption sites`)

// Disjoint write sets — each updater owns an explicit set and may touch
// nothing else, so they cannot collide. The new spec's own file belongs
// to Verify (it appends ## Executed); nobody here touches it.
const inSet = (f, preds) => preds.some(p => p(f.path))
const coreDocs = staleDocs.filter(f => inSet(f, [p => p.startsWith('docs/markup-reference'), p => p.startsWith('docs/architecture'), p => p === 'README.md' || p.startsWith('README')]))
const learnDocs = staleDocs.filter(f => f.path.startsWith('docs/learn/') && !f.path.startsWith('docs/learn/examples/'))
const specDocs = staleDocs.filter(f => f.path.startsWith('docs/specs/') && f.path !== spec.specPath)
const otherDocs = staleDocs.filter(f => !coreDocs.includes(f) && !learnDocs.includes(f) && !specDocs.includes(f))

const updaterBrief = (owned, assigned, extra) => `Reconciliation updater in ${REPO}. The new "${interview.name}" component landed (spec: ${spec.specPath}; files: ${JSON.stringify(build.files)}; design summary: ${design.designSummary}).
YOUR WRITE SET — you may edit ONLY these paths, other agents own everything else, and the survey findings assigned to you are: ${JSON.stringify(assigned)}
Owned: ${JSON.stringify(owned)}
For each finding: re-read the file NOW (concurrent sessions; the finding's quote may have moved), judge it, and fix stale claims to tell the truth about the component as built — under-claims and over-claims both. ${extra}
List any assigned finding you judged wrong under skipped, with the reason. changed[] must stay inside your write set.${GIT_RULES}`

const updaters = []
if (coreDocs.length) updaters.push(() => agent(
  updaterBrief(['docs/markup-reference.md', 'docs/architecture.md', 'README.md'], coreDocs,
    `markup-reference gets the component's full element entry (attributes, binding, load errors) matching the existing entries' shape; README's capability matrix/status rows must match what the spec says shipped.`),
  { label: 'update:core-docs', phase: 'Reconcile', schema: RECONCILE_SCHEMA },
))
if (learnDocs.length) updaters.push(() => agent(
  updaterBrief(['docs/learn/**.md (NOT docs/learn/examples/)'], learnDocs,
    `Tutorials teach: where a page hand-rolls what the component now does, update the prose AND its embedded code to the component — unless the page's point is to teach the hand-rolled mechanism, in which case add a pointer to the component instead.`),
  { label: 'update:learn-docs', phase: 'Reconcile', schema: RECONCILE_SCHEMA },
))
if (specDocs.length) updaters.push(() => agent(
  updaterBrief([`docs/specs/*.md except ${spec.specPath}`], specDocs,
    `Specs are decision records: do NOT rewrite history. Append/adjust their "## Executed" sections (or add a dated follow-up note) so they no longer under- or over-claim; never alter the recorded decisions themselves.`),
  { label: 'update:spec-executed', phase: 'Reconcile', schema: RECONCILE_SCHEMA },
))
// One agent per adoption DIRECTORY (multiple findings in one dir go to
// the same agent — two agents must never own the same write set).
const adoptByDir = {}
for (const site of adopters) {
  const dir = site.path.replace(/\/[^/]*\.[a-z]+$/, '')
  ;(adoptByDir[dir] = adoptByDir[dir] || []).push(site)
}
for (const [dir, sites] of Object.entries(adoptByDir)) {
  updaters.push(() => agent(
    updaterBrief([dir + '/'], sites,
      `Adopt the component the way kanbandemo adopted Tabs: the hand-rolled equivalent is DELETED in favor of the component (plain rm for dead files — never git rm), markup/bindings rewritten to the new element, behavior preserved. Build this site's binary to /tmp and smoke it under a pty (script -qec with an explicit stty size) before reporting. If on re-reading you judge this site contract surface that must NOT migrate (the cmd/reader precedent), skip with the reason.`),
    { label: `adopt:${dir}`, phase: 'Reconcile', schema: RECONCILE_SCHEMA, effort: 'high' },
  ))
}
if (otherDocs.length) updaters.push(() => agent(
  updaterBrief(['exactly the paths in your assigned findings'], otherDocs,
    `These fell outside the named doc sets — fix each in place.`),
  { label: 'update:misc-docs', phase: 'Reconcile', schema: RECONCILE_SCHEMA },
))

const reconciled = (await parallel(updaters)).filter(Boolean)

// docs-and-demos: audit the regen workflow's assumptions, and let the
// user decide whether to pay for a full regen now.
const regen = await agent(
  `You own ${REPO}/.claude/workflows/docs-and-demos.js (and nothing else) in this reconciliation. The new "${interview.name}" component landed; adopters changed: ${JSON.stringify(reconciled.flatMap(r => r.changed))}.
1. Read the script. Its recorder briefs hard-code demo choreography, key maps, expected strings, and the prebuilt-binary list — if this change invalidates any assumption (a demo's keys changed, a demo was migrated to the component, expected on-screen strings changed), update the script so the next run records the truth. scriptUpdated=true only if you edited it.
2. Ask the user (AskUserQuestion): run the full gooey-docs-and-demos regen now? Be honest about cost (~12 agents, ~700k tokens, ~12 min) and about what is stale without it (GIFs showing pre-component UI, regenerated docs). If they say yes: prep the scratch dir exactly as .claude/workflows/README.md specifies (go build each demo to /tmp/gooey-recordings — binaries to /tmp is the rule anyway) and set runRegen=true. If no, runRegen=false and note what stays stale.
${INTERACTION_RULES}${GIT_RULES}`,
  { label: 'regen:gate', phase: 'Reconcile', schema: REGEN_SCHEMA, agentType: 'general-purpose' },
)
let regenResult = null
if (regen?.runRegen) {
  log('user approved the docs-and-demos regen — running it nested')
  regenResult = await workflow('gooey-docs-and-demos', { repo: REPO, scratch: '/tmp/gooey-recordings' })
}

phase('Verify')
let verify = null
for (let attempt = 1; attempt <= 3; attempt++) {
  verify = await agent(
    `Verification pass ${attempt} for the "${interview.name}" component work in ${REPO}. ${attempt > 1 ? `Previous problems (fix them in the files this workflow touched, then re-check; report anything needing a human): ${JSON.stringify(verify?.problems)}` : ''}
Touched so far — component: ${JSON.stringify(build.files)}; spec: ${spec.specPath}; reconciled: ${JSON.stringify(reconciled.map(r => ({ owned: r.owned, changed: r.changed })))}
Run and report each:
1. Root module: go vet ./... && go test ./... (NEVER go build ./... at the root — it drops executables next to tracked ones).
2. Every nested module: cd mcp && go test -race ./...; cd grpc && go test -race ./...; every other dir with its own go.mod (discover: command find ${REPO} -name go.mod -not -path '*/.claude/*') gets go vet + go test — INCLUDING every examples/ and docs/learn/examples/ module: each must vet.
3. gofmt -l over every touched .go file — must be empty.
4. Damage pins: run the component's tests verbosely and confirm each count the spec promises.
5. Markdown: every relative link and image path in every touched doc resolves to a real file; every file a touched doc links to is either tracked (git ls-files --error-unmatch) or present on the staging list you are about to produce — a link into nowhere fails.
6. Append "## Executed" to ${spec.specPath} in house style (model: the executed specs' sections): what shipped, API surface, the verification evidence you just gathered, divergences from the plan. Also flip its Status line to executed. This is the ONE file Reconcile was told not to touch — it is yours.
7. Staging list: run git status --porcelain -uall NOW and derive stagingList (explicit file paths belonging to this work — component, tests, spec, reconciled docs, adopters, workflow script if edited${regenResult ? ', regenerated docs/GIFs' : ''}) and stray (untracked junk that must NOT be staged: binaries, .cast files, scratch logs). Every path individually — never a directory.
green=true only if ALL of 1-5 pass.${INVARIANTS}${GIT_RULES}`,
    { label: `verify:pass-${attempt}`, phase: 'Verify', schema: VERIFY_SCHEMA, effort: 'high' },
  )
  if (!verify) return { aborted: `verify pass ${attempt} skipped/died`, interview, design, spec, epic, build }
  if (verify.green) break
  log(`verify pass ${attempt} found problems: ${verify.problems.join('; ')}`)
}

return {
  component: interview.name,
  spec: spec.specPath,
  epic: epic?.epic ? { number: epic.epic, issues: epic.issues } : epic?.aborted || 'not filed',
  designArtifact: design.artifactUrl,
  build: { files: build.files, invariantChecklist: build.invariantChecklist },
  reconciled: reconciled.map(r => ({ changed: r.changed, skipped: r.skipped })),
  docsAndDemos: { scriptUpdated: regen?.scriptUpdated ?? false, regenRan: !!regenResult, regen: regenResult || undefined },
  verification: { green: verify.green, checks: verify.checks, problems: verify.problems },
  staging: { files: verify.stagingList, stray: verify.stray },
  note: 'Nothing is committed. The coordinator stages the explicit paths above and commits; epic issues (if filed) are already live on GitHub.',
}

function harnessNote(d) {
  return `harness dir ${d.harnessDir}, prototype files ${JSON.stringify(d.prototypeFiles)}, last screen: ${d.screenText.slice(0, 1500)}`
}
