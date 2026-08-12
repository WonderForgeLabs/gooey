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
    { title: 'Reconcile', detail: 'worktree-isolated updaters, then collect; docs-and-demos' },
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

// Every write-set decision in Reconcile is a string comparison between
// two paths produced by two different agents, and agents type paths in
// whatever shape reads well: `docs/x.md`, `./docs/x.md`, the absolute
// path, a trailing slash. An unnormalized `!==` there does not fail
// loudly — it hands one file to two owners, or excludes nothing. So
// normalize to repo-relative once, at every boundary where an
// agent-produced path enters the script.
const norm = p => {
  let s = String(p ?? '').trim()
  if (s === REPO) return '.'
  if (s.startsWith(REPO + '/')) s = s.slice(REPO.length + 1)
  while (s.startsWith('./')) s = s.slice(2)
  s = s.replace(/\/+$/, '')
  return s || '.'
}

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
- UI-goroutine confinement: properties, tree, and composer are Dispatcher-confined; anything async marshals back through the Dispatcher. Evaluation may READ but never Set.
- Package boundary: contract in root gooey, implementation in gooey/components, markup imports components — components imports root and NEVER the reverse. The package is "components", never "widgets".
- Subscription corollary: a Collapsed node never evaluates Render and so never subscribes — state that must reopen a hidden surface cannot be watched from inside it (the overlay idiom is a zero-rect Visible surface, per the Popup spec).
- Markup contract: the builder lives in markup/, NOT beside the component — a case in buildComponent (markup/markup.go), factored to a named builder in a topic file when non-trivial (model: buildTabs in markup/toolkit.go), using the helpers there (literalOrBound, boundProp[T], bindStyle...). LOAD ERRORS for every malformed use: anything statically checkable fails at load, "accepted but silently ignored" is a refused failure mode house-wide. Error-contract tests in markup/<name>_test.go modeled on markup/tabs_test.go's TestTabsMarkupLoadErrors (shared buildFails helper).
- Damage-count tests pin repaint behavior: idle, hover, focus move, state change — exact paint counts. Pins through built markup live in markup/<name>_test.go (model: markup/tabs_test.go, bound switch paints exactly 3), component-level pins in components/<name>_test.go; the counter is Composer.Frame's second return, or app.PaintedLastFrame().
- docs/specs/2026-08-10-*.md are the contracts for their subsystems — read the ones adjacent to what you are building before writing code.`

// The build-run-inspect harness is this workflow's core mechanic: the
// component does not exist yet, so every design round compiles a scratch
// app hosting the in-progress prototype and drives it over MCP.
const HARNESS = `
BUILD-RUN-INSPECT HARNESS (the core mechanic — every design round goes through it):
- Scratch module OUTSIDE the repo at /tmp/gooey-proto-<component>/: own go.mod (module scratch/proto) requiring github.com/WonderForgeLabs/gooey and github.com/WonderForgeLabs/gooey/mcp, with replace directives at ${REPO} and ${REPO}/mcp. mcp/ is a nested module — anything importing it needs its own module (see the header comment in ${REPO}/examples/kanbandemo/go.mod).
- The prototype component lives IN THE SCRATCH MODULE (a local package), not in the repo: implement it against the real gooey interfaces (Component/Base, FocusState, HoverState, *prop.Property[T]) so the eventual move into components/ is a copy, not a rewrite. Register a scratch markup builder for it if the markup shape is under design; hosting it from Go composition is fine for early rounds.
- Host app modeled on ${REPO}/mcp/cmd/mcpdemo/main.go: a .gooey page exercising the prototype in realistic surroundings (neighbors, a Grid, focusable siblings), loaded via markup.Page(os.DirFS(dir), "name.gooey", ctx, also...) so file edits hot-reload (~300ms polling; name Include/UserControl files in also... or their edits won't reload), viewmodel of prop handles, one mcp.Serve(app, mcp.Options{Addr: "127.0.0.1:<port>", Context: ctx}) call — pass the markup Context or the name-addressed tools (list_values, set_value, invoke_command) see nothing. Pick a free port (not 7777); an empty Addr means an ephemeral port readable from srv.Addr().
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
      `Read-only exploration in ${REPO}. The component-author's idiom brief for building "${interview.name}": read component.go, base.go, components/ (pick the 2 closest components to this one and read them fully, plus components/tabs.go as the canonical modern example), how FocusState/HoverState/boundProp work, how a built-in element's builder is wired (the buildComponent switch in markup/markup.go, topic-file builders like buildTabs in markup/toolkit.go, the literalOrBound/boundProp helpers) and what its load-error contract looks like (markup/tabs_test.go's TestTabsMarkupLoadErrors), and what damage-count tests assert on each side (markup/tabs_test.go and components/tabs_test.go). Return a compact idiom brief with exact names and file:line pointers a builder can trust.${GIT_RULES}`,
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
// The one path every later phase compares against. Normalized here so
// the "nobody but Verify touches the new record" exclusion is a real
// exclusion and not a coin flip on how the spec agent typed it.
const specPath = norm(spec.specPath)

phase('Epic')
// The spec becomes tracked work: gooey-epic-decompose has its own user
// gate before filing anything, so calling it unconditionally is safe.
const epic = await workflow('gooey-epic-decompose', {
  repo: REPO,
  doc: specPath,
  context: `Called from gooey-new-component for the approved "${interview.name}" component. The build/reconcile/verify work happens in this same workflow run — decompose so each child issue maps to a spec section (API, markup contract, damage pins, adoption sites, docs), and note in the epic body that the epic tracks the work of THIS run plus any follow-ups.`,
})
if (epic?.aborted) log(`epic decomposition did not file: ${epic.aborted} — continuing; work is untracked`)

phase('Build')
const build = await agent(
  `Build the approved "${interview.name}" component for real in ${REPO}. The design is user-approved and the decision record at ${specPath} is the contract — implement THAT, do not redesign.
DIRECTION: ${JSON.stringify(interview)}
APPROVED DESIGN: ${JSON.stringify({ designSummary: design.designSummary, decisions: design.decisions })}
PROTOTYPE (start from it — it is the approved behavior; harden, don't rewrite): ${harnessNote(design)}
GROUNDING: ${grounding}
Deliverables, per the spec:
- components/<name>.go (+ siblings as the idiom brief dictates), moved/adapted from the prototype.
- The markup builder in markup/ (a buildComponent case, factored to a topic file like markup/toolkit.go's buildTabs when non-trivial), and a LOAD ERROR for every malformed use the spec's markup contract lists — with tests mirroring markup/tabs_test.go's TestTabsMarkupLoadErrors.
- Damage-count pins asserting the exact counts the spec promises (idle, hover, focus, state change, the money interaction): through-markup pins in markup/<name>_test.go, component-level pins in components/<name>_test.go.
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
    `Read-only survey in ${REPO}. The new "${interview.name}" component just landed in the working tree (files: ${JSON.stringify(build.files)}; spec ${specPath}). Find every DOC whose claims are now stale — under- OR over-claiming: docs/markup-reference.md (missing element entry), docs/learn/** tutorials/how-tos/concepts (statements like "gooey has no X", lists of components, embedded code samples), README.md (capability matrix rows, status claims), docs/architecture.md, and every docs/specs/*.md whose "## Executed"/"Not here" sections this change touches (a spec that said "no X exists" or deferred X now under-claims). Read the actual claims; kind=doc-stale; \`what\` quotes the stale sentence. Use command grep for sweeps. Do NOT edit anything.${GIT_RULES}`,
    { label: 'survey:stale-docs', phase: 'Reconcile', schema: SURVEY_SCHEMA },
  ),
  () => agent(
    `Read-only survey in ${REPO}. Find every piece of SHIPPED CODE that should adopt the new "${interview.name}" component: cmd/ demos, examples/**, docs/learn/examples/**, and code embedded in docs pages, wherever a hand-rolled equivalent now exists. The explore phase found these candidates — re-verify each in the CURRENT tree (other sessions write concurrently) and finish the sweep: ${grounding.slice(0, 4000)}
The Tabs precedent is the bar: kanbandemo's hand-rolled switcher was deleted in the same PR that shipped Tabs — but cmd/reader's panes were contract surface and left alone. Judge each site: adopt (kind=adopt) or leave (omit, note why in \`what\` of a doc-stale finding only if a doc claims otherwise). Do NOT edit anything.${GIT_RULES}`,
    { label: 'survey:adopters', phase: 'Reconcile', schema: SURVEY_SCHEMA },
  ),
])
const findings = survey.filter(Boolean).flatMap(s => s.findings).map(f => ({ ...f, path: norm(f.path) }))
// The new record belongs to Verify, which appends its ## Executed — and
// the stale-docs brief invites a finding on it in so many words ("every
// docs/specs/*.md whose ## Executed / Not here sections this change
// touches"). Drop it here, at the source: excluding it only from the
// specDocs bucket lets it fall straight through into otherDocs, and the
// misc-docs updater would then write the record from scratch in a tree
// that does not contain it — a stub the collection step copies over the
// real decision record.
const ownRecord = findings.filter(f => f.path === specPath)
if (ownRecord.length) log(`${ownRecord.length} survey finding(s) on the new record ${specPath} dropped — Verify owns that file`)
const staleDocs = findings.filter(f => f.kind === 'doc-stale' && f.path !== specPath)
const adoptFindings = findings.filter(f => f.kind === 'adopt' && f.path !== specPath)
log(`reconcile survey: ${staleDocs.length} stale doc claims, ${adoptFindings.length} adoption sites`)

// Disjoint write sets — each updater owns an explicit set and may touch
// nothing else, so they cannot collide. The new spec's own file belongs
// to Verify (it appends ## Executed); nobody here touches it.
// ONE PREDICATE PER BUCKET, used for all three jobs it has to do: which
// findings land in the bucket, what the updater DECLARES it owns, and
// whether an adopt finding on that path folds into the same updater.
//
// They used to be written out three times, and the copies had already
// drifted: the bucket filter took any `docs/architecture*` or `README*`
// while owns() took three exact filenames, and learnDocs bucketed with no
// extension check while owns() required `.md`. A path caught by the loose
// half and missed by the strict half — `docs/architecture-notes.md` is a
// real one — got assigned to the core updater as a finding while being
// carved OUT of every updater's write set, so it could only be fixed by an
// agent disobeying its own instructions. That is finding 3 of #180
// reopened one layer down, and it reopens again the moment these are
// separate expressions. Deriving all three from the same function is what
// makes the divergence unrepresentable rather than merely fixed.
const coreDocsPred = p => p.startsWith('docs/markup-reference') || p.startsWith('docs/architecture') || p.startsWith('README')
const learnDocsPred = p => p.startsWith('docs/learn/') && !p.startsWith('docs/learn/examples/')
const specDocsPred = p => p.startsWith('docs/specs/') && p !== specPath
const coreDocs = staleDocs.filter(f => coreDocsPred(f.path))
const learnDocs = staleDocs.filter(f => learnDocsPred(f.path))
const specDocs = staleDocs.filter(f => specDocsPred(f.path))
const otherDocs = staleDocs.filter(f => !coreDocs.includes(f) && !learnDocs.includes(f) && !specDocs.includes(f))

// Disjointness has to hold ACROSS the doc sets and the adoption sets,
// not only within each. The adopters survey is asked in so many words
// for "code embedded in docs pages", so an adopt finding on a file a
// doc bucket already owns is an expected result, not an exotic one —
// and handing one path to two isolated worktrees is exactly what the
// collection step STOPs on. So: what each doc updater DECLARES it owns,
// as a predicate, and the rule that a path has exactly one owner.
const docAgents = [
  { assigned: coreDocs, owns: coreDocsPred },
  { assigned: learnDocs, owns: learnDocsPred },
  { assigned: specDocs, owns: specDocsPred },
  { assigned: otherDocs, owns: p => otherDocs.some(f => f.path === p) },
].filter(a => a.assigned.length)

// An adoption site that lands on a doc-owned file goes TO that doc
// updater rather than to an adoption agent of its own: it keeps the
// work (the doc agent gets both jobs on that page) without ever giving
// the file two owners.
const adopters = []
for (const site of adoptFindings) {
  const owner = docAgents.find(a => a.owns(site.path))
  if (owner) {
    owner.assigned.push(site)
    log(`adoption site ${site.path} folded into the doc updater that already owns it — one path, one owner`)
  } else {
    adopters.push(site)
  }
}

// Reconcile is the only phase with genuinely concurrent writers, so it
// is the only one that pays for worktree isolation. Each updater gets
// its own worktree (the runtime creates it and auto-removes it if
// nothing changed); the coordinator collects the diffs afterwards. Two
// guards, not one: disjoint write sets so no two agents own a path, and
// separate trees so a stray write cannot land on someone else's file.
const ISOLATION_RULES = `
YOU ARE IN YOUR OWN GIT WORKTREE — not the shared checkout:
- Run \`pwd\` and \`git rev-parse --show-toplevel\` FIRST and work from that root. Paths in your write set are relative to it. Never cd to the shared checkout at ${REPO} to make an edit; reading from it is fine, writing to it defeats the isolation and is how duplicate-file forks happen.
- Your edits stay uncommitted in your worktree — the coordinator collects them. Do NOT commit, and do NOT create branches. Report changed[] as paths relative to the repo root so they are meaningful after collection.
- Verify before you report: run \`git status --porcelain -uall\` in YOUR worktree and confirm every changed path is inside your write set or is one of the seeded paths below, and that nothing stray came along (\`-uall\` descends into untracked directories; plain porcelain stops at the directory and would hide a file inside it). The seeded paths WILL show up dirty there — that is expected, and they still never go in changed[].
- Never create a directory under .claude/worktrees/ yourself. An unregistered directory there is invisible to BOTH \`git worktree list\` and \`git status\`, and has silently orphaned real work in this repo before.`

// A worktree is branched from a COMMIT, and this workflow never commits
// — so the component the Build phase just wrote is absent from every
// updater's tree. Reading it from the shared checkout is enough to
// DESCRIBE it, but not to compile against it: example modules `replace`
// gooey with `../../`, which resolves to the updater's own worktree
// root, and docs/learn/examples/** compiles against that same root
// module. An adopter that cannot resolve the component cannot build,
// never mind smoke, the site it is adopting. So each updater seeds its
// own tree with the built files first. Seeded paths belong to nobody's
// write set and are never collected back — they already exist in the
// shared checkout, which is where the coordinator will stage them from.
const SEED_FILES = [...new Set([...build.files.map(norm), specPath])]
const SEED_RULES = `
SEED YOUR WORKTREE BEFORE YOU READ A FINDING OR EDIT ANYTHING:
- The component and its decision record were written into the SHARED checkout and are still uncommitted, so your worktree — branched from a commit — does not have them. Copy them in yourself: for each path P below, \`mkdir -p $(dirname P)\` then \`cp ${REPO}/P P\` relative to YOUR worktree root. Paths: ${JSON.stringify(SEED_FILES)}
- Re-derive that list, do not trust it blindly: run \`git -C ${REPO} status --porcelain -uall\` and bring across anything else under components/ or markup/ that belongs to this component. A half-seeded tree compiles into confusing errors that look like your own mistake.
- Seeded files are NOT in your write set: do not edit them, do not delete them, and never list them in changed[]. They are here so your tree resolves the component, nothing more.
- Prove the seed took before you edit: \`go vet ./components/... ./markup/...\` from your worktree root must resolve the component (\`go build ./...\` at the root stays banned — it drops executables next to tracked ones). If it does not resolve, STOP and report that instead of editing around it: a green report from a tree that cannot see the component is worthless.`

const updaterBrief = (owned, assigned, extra) => `Reconciliation updater for ${REPO}. The new "${interview.name}" component landed (spec: ${specPath}; files: ${JSON.stringify(build.files)}; design summary: ${design.designSummary}).
YOUR WRITE SET — you may edit ONLY these paths, other agents own everything else, and the survey findings assigned to you are: ${JSON.stringify(assigned)}
Owned: ${JSON.stringify(owned)}
For each finding: re-read the file NOW (concurrent sessions; the finding's quote may have moved), judge it, and fix stale claims to tell the truth about the component as built — under-claims and over-claims both. A finding whose kind is "adopt" means that file also hand-rolls what the component now does: rewrite the code, not just the prose around it, to the same bar an adoption agent is held to (a reader must be able to paste it and have it run). ${extra}
List any assigned finding you judged wrong under skipped, with the reason. changed[] must stay inside your write set.${SEED_RULES}${ISOLATION_RULES}${GIT_RULES}`

const updaters = []
// The write set is the canonical targets UNION every path actually
// assigned to this updater. Listing only the canonical three was the other
// half of the same bug: the bucket predicate can hand this agent
// `docs/architecture-notes.md`, and an agent told to fix a finding on a
// file its write set forbids has no legal move. Deduped so the common case
// still reads as exactly the three names.
if (coreDocs.length) updaters.push(() => agent(
  updaterBrief([...new Set(['docs/markup-reference.md', 'docs/architecture.md', 'README.md', ...coreDocs.map(f => f.path)])], coreDocs,
    `markup-reference gets the component's full element entry (attributes, binding, load errors) matching the existing entries' shape; README's capability matrix/status rows must match what the spec says shipped.`),
  { label: 'update:core-docs', phase: 'Reconcile', schema: RECONCILE_SCHEMA, isolation: 'worktree' },
))
if (learnDocs.length) updaters.push(() => agent(
  updaterBrief(['docs/learn/**.md (NOT docs/learn/examples/)'], learnDocs,
    `Tutorials teach: where a page hand-rolls what the component now does, update the prose AND its embedded code to the component — unless the page's point is to teach the hand-rolled mechanism, in which case add a pointer to the component instead.`),
  { label: 'update:learn-docs', phase: 'Reconcile', schema: RECONCILE_SCHEMA, isolation: 'worktree' },
))
if (specDocs.length) updaters.push(() => agent(
  updaterBrief([`docs/specs/*.md except ${specPath}`], specDocs,
    `Specs are decision records: do NOT rewrite history. Append/adjust their "## Executed" sections (or add a dated follow-up note) so they no longer under- or over-claim; never alter the recorded decisions themselves.`),
  { label: 'update:spec-executed', phase: 'Reconcile', schema: RECONCILE_SCHEMA, isolation: 'worktree' },
))
// One agent per adoption DIRECTORY (multiple findings in one dir go to
// the same agent — two agents must never own the same write set).
const dirOf = p => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '.')
const adoptByDir = {}
for (const site of adopters) {
  const d = dirOf(site.path)
  ;(adoptByDir[d] = adoptByDir[d] || []).push(site)
}
// Fold a nested directory into its shallowest ancestor. Two write sets
// where one contains the other are NOT disjoint — examples/kanbandemo
// and examples/kanbandemo/panel would hand two isolated agents
// overlapping ownership, and the collection step cannot see that: it
// compares identical paths, not containment. Shallowest-first ordering
// guarantees an ancestor is promoted to a root before its descendants
// are tested against it.
//
// '.' is a PEER bucket, not an ancestor: its write set is the repo-root
// files only (that is what its brief says it owns). Treating it as an
// ancestor of everything folded every other adoption directory into it
// on the first root-level finding, leaving one agent holding findings
// outside the write set its own brief forbids it to leave — silently
// dropping every adoption but the root's.
const contains = (a, b) => a === b || (a !== '.' && b.startsWith(a + '/'))
const depth = d => (d === '.' ? 0 : d.split('/').length)
const roots = []
for (const d of Object.keys(adoptByDir).sort((a, b) => depth(a) - depth(b))) {
  const owner = roots.find(r => contains(r, d))
  if (owner) {
    adoptByDir[owner].push(...adoptByDir[d])
    delete adoptByDir[d]
    log(`adoption dir ${d} folded into ${owner} — nested write sets are not disjoint`)
  } else {
    roots.push(d)
  }
}
// An adoption write set is a whole DIRECTORY, so it can swallow a file
// a doc updater declared even when no single finding names it twice
// (docs/learn/examples/07-x/ owning a README.md that misc-docs owns).
// Carve every declared doc path that falls inside the directory back
// out of it, by name, in the write set the agent is handed.
const reserved = [
  { label: specPath, under: specPath },
  ...docAgents.flatMap(a => a.assigned.map(f => ({ label: f.path, under: f.path }))),
  ...(coreDocs.length ? ['docs/markup-reference.md', 'docs/architecture.md', 'README.md'].map(p => ({ label: p, under: p })) : []),
  ...(learnDocs.length ? [{ label: 'docs/learn/**.md (except docs/learn/examples/)', under: 'docs/learn' }] : []),
  ...(specDocs.length ? [{ label: 'docs/specs/*.md', under: 'docs/specs' }] : []),
]
const carvedFor = dir => {
  const inside = p => (dir === '.' ? !p.includes('/') : p === dir || p.startsWith(dir + '/'))
  return [...new Set(reserved.filter(e => inside(e.under)).map(e => e.label))]
}
for (const [dir, sites] of Object.entries(adoptByDir)) {
  const carved = carvedFor(dir)
  if (carved.length) log(`adoption dir ${dir} carved around doc-owned paths: ${carved.join(', ')}`)
  updaters.push(() => agent(
    updaterBrief([
      dir === '.' ? 'the repo root (files directly in it only)' : dir + '/',
      ...(carved.length ? [`EXCEPT these, which another updater (or Verify) owns — read them if you like, never edit them: ${carved.join(', ')}`] : []),
    ], sites,
      `Adopt the component the way kanbandemo adopted Tabs: the hand-rolled equivalent is DELETED in favor of the component (plain rm for dead files — never git rm), markup/bindings rewritten to the new element, behavior preserved. Build this site's binary to /tmp and smoke it under a pty (script -qec with an explicit stty size) before reporting. If on re-reading you judge this site contract surface that must NOT migrate (the cmd/reader precedent), skip with the reason.`),
    { label: `adopt:${dir}`, phase: 'Reconcile', schema: RECONCILE_SCHEMA, effort: 'high', isolation: 'worktree' },
  ))
}
if (otherDocs.length) updaters.push(() => agent(
  updaterBrief(['exactly the paths in your assigned findings'], otherDocs,
    `These fell outside the named doc sets — fix each in place.`),
  { label: 'update:misc-docs', phase: 'Reconcile', schema: RECONCILE_SCHEMA, isolation: 'worktree' },
))

const reconciled = (await parallel(updaters)).filter(Boolean)

// Collection: the updaters worked in separate worktrees, so their edits
// are not in the shared checkout yet. One agent brings them back — the
// write sets are disjoint by construction, so this is a copy, not a
// merge, and any path claimed by two worktrees is a bug worth stopping
// on rather than silently resolving.
//
// TWO checks, not one, and the second is the one that bites: a whole-
// file copy from a worktree carries that worktree's whole BASE COMMIT
// with it. The shared checkout is routinely ahead of that base — the
// coordinator's own feature branch, another session's uncommitted edits
// — so an unchecked copy silently reverts every line the updater never
// saw, with no diff, no conflict, and no report. Comparing worktrees to
// each other cannot catch it; only comparing each file to its
// destination can.
let collection = null
if (reconciled.some(r => r.changed?.length)) {
  collection = await agent(
    `Collect the reconciliation edits into the shared checkout at ${REPO}. Each updater ran in its own git worktree and left its changes uncommitted there; the write sets were assigned disjoint, so this is a copy with two guards, not a merge.
The updaters and what each reports changing: ${JSON.stringify(reconciled.map(r => ({ owned: r.owned, changed: r.changed })))}
Files the updaters were told to SEED into their worktrees from the shared checkout — these are already in ${REPO} and must NOT be collected back, whatever a worktree's git status says about them: ${JSON.stringify(SEED_FILES)}
Steps:
1. Enumerate the worktrees: \`git worktree list\` from ${REPO}. Match each to an updater by the changed paths it reports. Separately, check each entry under ${REPO}/.claude/worktrees/ for a \`.git\` file or directory: one without it is not a worktree at all and git cannot see it via \`git worktree list\` OR \`git status\`. Report such a directory; do not delete it, and do not expect the directory listing and \`git worktree list\` to match as sets — the list also carries the main checkout and worktrees registered elsewhere.
2. In each worktree run \`git status --porcelain -uall\` yourself — trust it over the agent's self-report — and collect the actual changed/added/deleted paths, minus the seeded paths above.
3. Collision check (worktree vs worktree) BEFORE copying: if two worktrees changed the same path, STOP and report it as a collision with both versions' diffs. Do not pick a winner — disjoint write sets were the contract, and a violation means the survey mis-assigned ownership.
4. Freshness check (worktree vs DESTINATION) BEFORE copying, per file, no exceptions. The updater edited its worktree's base commit; ${REPO} may have moved on since. For each path P from step 2, in that worktree:
   - P tracked in the worktree: \`git -C <worktree> show HEAD:P | diff - ${REPO}/P\`. Empty diff -> the destination is exactly what the updater started from, safe to copy. NON-EMPTY -> STALE: do NOT copy. Report P with both diffs (destination-vs-base, and the updater's own \`git -C <worktree> diff -- P\`) so a human can merge the two intents.
   - P new in the worktree (absent from its HEAD): ${REPO}/P must not exist. If it does, same stale case — report, never overwrite.
   - P deleted by an adopter: \`git -C <worktree> show HEAD:P | diff - ${REPO}/P\` must be empty before you \`rm ${REPO}/P\`. Non-empty -> report, do not delete.
5. Copy each file that passed BOTH checks from its worktree into ${REPO} at the same relative path (plain \`cp\`; plain \`rm\` for a cleared deletion). Never \`git mv\`/\`git rm\`, never \`git add\`, never commit.
6. Verify by diffing: for every collected path, the file in ${REPO} must now match the worktree's version.
Report: collected paths, any collisions, any STALE paths you refused to copy (these need a human — say so plainly, they are not a footnote), any \`.git\`-less directory under .claude/worktrees/, and anything else you could not collect.${GIT_RULES}`,
    { label: 'collect:worktrees', phase: 'Reconcile', effort: 'high' },
  )
}

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
Touched so far — component: ${JSON.stringify(build.files)}; spec: ${specPath}; reconciled: ${JSON.stringify(reconciled.map(r => ({ owned: r.owned, changed: r.changed })))}
Run and report each:
1. Root module: go vet ./... && go test ./... (NEVER go build ./... at the root — it drops executables next to tracked ones).
2. Every nested module: cd mcp && go test -race ./...; cd grpc && go test -race ./...; every other dir with its own go.mod (discover: command find ${REPO} -name go.mod -not -path '*/.claude/*') gets go vet + go test — INCLUDING every examples/ and docs/learn/examples/ module: each must vet.
3. gofmt -l over every touched .go file — must be empty.
4. Damage pins: run the component's tests verbosely and confirm each count the spec promises.
5. Markdown: every relative link and image path in every touched doc resolves to a real file; every file a touched doc links to is either tracked (git ls-files --error-unmatch) or present on the staging list you are about to produce — a link into nowhere fails.
6. Append "## Executed" to ${specPath} in house style (model: the executed specs' sections): what shipped, API surface, the verification evidence you just gathered, divergences from the plan. Also flip its Status line to executed. This is the ONE file Reconcile was told not to touch — it is yours.
7. Worktree hygiene (a NOTE, never a green gate): the Reconcile updaters ran in isolated worktrees and the runtime leaves behind every one that changed something — so entries under ${REPO}/.claude/worktrees/ are the EXPECTED outcome of the phase you just ran, not a failure. Do NOT compare that directory to \`git worktree list\` as sets: the list also carries the main checkout and any worktree registered outside that directory, other sessions' worktrees appear in both, and \`ls -a\` adds \`.\` and \`..\` — the two can never match. What IS worth catching: a directory there with no \`.git\` file or directory inside it (\`ls -a ${REPO}/.claude/worktrees/*/.git\`). That one is not a worktree at all, git cannot see it via \`git worktree list\` OR \`git status\`, and it has silently stranded real work in this repo before. Report each such directory under checks as a note; do not delete anything yourself, and do not let it change green.
8. Staging list: run git status --porcelain -uall NOW and derive stagingList (explicit file paths belonging to this work — component, tests, spec, reconciled docs collected from the worktrees, adopters, workflow script if edited${regenResult ? ', regenerated docs/GIFs' : ''}) and stray (untracked junk that must NOT be staged: binaries, .cast files, scratch logs). Every path individually — never a directory. \`-uall\` is load-bearing: it descends into untracked directories, where plain porcelain stops at the directory name and hides the files inside.
green=true only if ALL of checks 1-5 pass. (6 and 8 are actions, 7 is a note — none of the three is a pass/fail check.)${INVARIANTS}${GIT_RULES}`,
    { label: `verify:pass-${attempt}`, phase: 'Verify', schema: VERIFY_SCHEMA, effort: 'high' },
  )
  if (!verify) return { aborted: `verify pass ${attempt} skipped/died`, interview, design, spec, epic, build }
  if (verify.green) break
  log(`verify pass ${attempt} found problems: ${verify.problems.join('; ')}`)
}
// Red after three passes returns a FAILED shape, the way gooey-new-demo
// does. The success shape ends with "the coordinator stages the explicit
// paths above and commits" — handing that to a coordinator alongside a
// buried `green: false` is how a red build gets committed under live
// issues that already point at it. No stagingList, no commit note.
if (!verify.green) {
  return {
    failed: 'verification still red after 3 passes — nothing here is safe to stage',
    problems: verify.problems,
    checks: verify.checks,
    component: interview.name,
    spec: specPath,
    epic: epic?.epic ? { number: epic.epic, issues: epic.issues } : epic?.aborted || 'not filed',
    build: { files: build.files, invariantChecklist: build.invariantChecklist },
    reconciled: reconciled.map(r => ({ changed: r.changed, skipped: r.skipped })),
    collection: collection || 'no worktree edits to collect',
    // REPORTED ON THE RED PATH TOO, and this is the path where it matters
    // most. The success shape carries docsAndDemos; the failure shape used
    // to drop it, so a run that regenerated GIFs and docs and THEN went red
    // left those regenerated files uncommitted in the tree with nothing in
    // the result saying they existed. The human triaging a red run is
    // exactly the reader who needs to know what is sitting in their working
    // tree — omitting it here hid output precisely when it was least
    // expected and hardest to notice.
    docsAndDemos: { scriptUpdated: regen?.scriptUpdated ?? false, regenRan: !!regenResult, regen: regenResult || undefined },
    note: 'The working tree holds the component, the spec, the collected reconciliation edits, and any regenerated docs/demos — all uncommitted and all red. Epic issues (if filed) are already live on GitHub and now point at unfinished work — fix or say so there.',
  }
}

return {
  component: interview.name,
  spec: specPath,
  epic: epic?.epic ? { number: epic.epic, issues: epic.issues } : epic?.aborted || 'not filed',
  designArtifact: design.artifactUrl,
  build: { files: build.files, invariantChecklist: build.invariantChecklist },
  reconciled: reconciled.map(r => ({ changed: r.changed, skipped: r.skipped })),
  collection: collection || 'no worktree edits to collect',
  docsAndDemos: { scriptUpdated: regen?.scriptUpdated ?? false, regenRan: !!regenResult, regen: regenResult || undefined },
  verification: { green: verify.green, checks: verify.checks, problems: verify.problems },
  staging: { files: verify.stagingList, stray: verify.stray },
  note: 'Nothing is committed. The coordinator stages the explicit paths above and commits; epic issues (if filed) are already live on GitHub.',
}

function harnessNote(d) {
  return `harness dir ${d.harnessDir}, prototype files ${JSON.stringify(d.prototypeFiles)}, last screen: ${d.screenText.slice(0, 1500)}`
}
