export const meta = {
  name: 'gooey-new-demo',
  description: 'Interview-first, design-first pipeline for building a new gooey demo',
  whenToUse: 'When the user wants a new demo (cmd/<name> or examples/<name>) and the shape is not already decided. Interactive: it interviews the user, iterates on mockups and a live MCP-driven prototype until they approve, and only then writes real code. Not for mechanical tweaks to existing demos.',
  phases: [
    { title: 'Interview', detail: 'brainstorm with the user until a direction is picked' },
    { title: 'Explore', detail: 'capability map, prior art, spec contracts for the chosen area' },
    { title: 'Design', detail: 'mockups + live prototype harness, looped on user feedback' },
    { title: 'Build', detail: 'real demo, markup-first, damage-count tests' },
    { title: 'Verify', detail: 'all modules green, pty smoke, no stray binaries' },
    { title: 'Document', detail: 'demos.md entry, GIF, docs/learn pointer' },
  ],
}

// Design-first demo pipeline. This workflow is INTERACTIVE — its agents
// interview the user with AskUserQuestion and nothing is built until the
// user approves a design. Expect it to pause on questions; that is the
// point. Re-enterable: a rejection in Design loops back, "rethink the
// premise" re-runs the Interview.
//
//   Workflow({ name: 'gooey-new-demo' })
//   Workflow({ name: 'gooey-new-demo', args: { idea: 'a spreadsheet demo' } })
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
- When the user is choosing between layouts or visual directions, put side-by-side ASCII mockups in each option's \`preview\` field (previews render in a monospace box; single-select questions only).
- Never assume an answer the user has not given, and never mark your own work approved. If you cannot reach the user, return your open questions in the structured result instead of guessing.`

const INVARIANTS = `
FRAMEWORK INVARIANTS — violating one is a defect, not a style choice:
- No reflection in anything the framework or a demo compiles in.
- Property subscription happens at the Get call site: dependencies are recorded by the Get that actually runs. Hoist Gets above early returns; never short-circuit past one (\`a || b.Get()\` silently drops the b dependency when a is true). prop.Set never compares values — a same-value Set still notifies.
- Containers paint their own chrome; parents never pre-clear a child's cells.
- Anything Startable must close-and-join: close(done) alone lets one tick post after Close — pair it with <-stopped or lifetime tests flake.
- Event/command fields are gooey.Action, not bare func(); test presence with gooey.CanExecute, never != nil.
- Markup-first: the UI is declared in a .gooey file; Go holds the viewmodel and logic. Markup served for swap needs a key-complete Values map — a key whose value is "" must still exist or the page fails to load.
- UI-goroutine confinement: properties, tree, and composer are Dispatcher-confined. Anything async (timers, network, MCP) marshals back through the Dispatcher; never touch a property from another goroutine, and never Set during an evaluation.
- Load errors are house-wide policy: anything statically checkable in markup fails at load — "accepted but silently ignored" is a refused failure mode.
- Damage-count tests pin repaint behavior where it is non-trivial (models: markup/tabs_test.go for pins through built markup — a bound tab switch paints exactly 3 — and components/tabs_test.go; the counter is Composer.Frame's second return, or app.PaintedLastFrame()).
- docs/specs/2026-08-10-*.md are the contracts for their subsystems — read the ones that touch what you are building before writing code.`

// The live prototyping harness: a throwaway module hosting the
// in-progress UI, wired to the MCP control plane so agents (and the
// user, via pasted screen_text) iterate on the real thing, not a drawing.
const HARNESS = `
LIVE PROTOTYPING HARNESS (build once, reuse every round):
- Scratch module OUTSIDE the repo at /tmp/gooey-proto-<name>/: own go.mod (module scratch/proto) requiring github.com/WonderForgeLabs/gooey and github.com/WonderForgeLabs/gooey/mcp, with replace directives at ${REPO} and ${REPO}/mcp. mcp/ is a nested module — anything importing it needs its own module (see the header comment in ${REPO}/examples/kanbandemo/go.mod).
- Model main.go on ${REPO}/mcp/cmd/mcpdemo/main.go: load the page with markup.Page(os.DirFS(dir), "name.gooey", ctx, also...) — App.Run wires its watcher through the Dispatcher so file edits hot-reload (~300ms polling; the older markup.Watch built trees off the UI goroutine and is not the path). Name any Include/UserControl files in the also... variadic or edits to them will not reload. Viewmodel is prop.NewSource/NewComputed handles; then one mcp.Serve(app, mcp.Options{Addr: "127.0.0.1:<port>", Context: ctx}) call — pass the markup Context or the name-addressed tools (list_values, set_value, invoke_command) see nothing. Pick a free port (not 7777); an empty Addr means an ephemeral port readable from srv.Addr().
- A gooey app needs a pty: run it in the background under script -qec "stty cols 110 rows 32; /tmp/gooey-proto-<name>/proto" /dev/null — the stty MUST set a size or the pty is 0x0 and paints nothing.
- Drive it over MCP streamable HTTP at http://127.0.0.1:<port>/mcp. Plain curl JSON-RPC works: initialize, notifications/initialized (carry the Mcp-Session-Id header), then tools/call — read ${REPO}/mcp/e2e_linux_test.go for the exact wire sequence, and leave a drive.sh wrapper in the scratch dir so later rounds reuse it.
- The tools: screen_text (your screenshot — quote it in results and in AskUserQuestion previews), tree_snapshot, swap_markup / patch_markup / validate_markup (change the UI without restarting), send_keys, send_mouse, focus, set_value / list_values, invoke_command, list_styles, register_properties.
- Iterate on the real thing: edit the .gooey (hot reload) or swap_markup, then screen_text. Kill your app instance and its script wrapper before you exit — the next round rebuilds in seconds. Leave the scratch dir in place.`

const INTERVIEW_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['decided', 'name', 'purpose', 'audience', 'capabilities', 'compelling', 'layoutDirection', 'openQuestions'],
  properties: {
    decided: { type: 'boolean', description: 'true only if the user explicitly picked a direction' },
    name: { type: 'string', description: 'demo name (dir name under cmd/ or examples/)' },
    purpose: { type: 'string', description: 'what the demo proves, in the user\'s words' },
    audience: { type: 'string' },
    capabilities: { type: 'array', items: { type: 'string' }, description: 'framework capabilities it must exercise' },
    compelling: { type: 'string', description: 'what "compelling" means for this demo, per the user' },
    layoutDirection: { type: 'string', description: 'the layout/interaction direction the user picked, incl. the winning ASCII sketch' },
    openQuestions: { type: 'array', items: { type: 'string' } },
  },
}

const DESIGN_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['verdict', 'artifactUrl', 'harnessDir', 'markupFile', 'designSummary', 'screenText', 'userFeedback'],
  properties: {
    verdict: { type: 'string', enum: ['approved', 'revise', 'restart-interview'], description: 'the USER\'s verdict from AskUserQuestion, never your own' },
    artifactUrl: { type: 'string', description: 'published mockup artifact URL ("" if none)' },
    harnessDir: { type: 'string', description: 'scratch harness dir ("" if not yet built)' },
    markupFile: { type: 'string', description: 'path of the prototype .gooey inside the harness' },
    designSummary: { type: 'string', description: 'the design as it stands: layout, components, key map, what each beat shows' },
    screenText: { type: 'string', description: 'final screen_text snapshot of the prototype this round' },
    userFeedback: { type: 'string', description: 'verbatim gist of what the user asked to change ("" when approved)' },
  },
}

const BUILD_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['location', 'files', 'tests', 'notes'],
  properties: {
    location: { type: 'string', description: 'cmd/<name> or examples/<name>' },
    files: { type: 'array', items: { type: 'string' }, description: 'repo-relative paths written' },
    tests: { type: 'array', items: { type: 'string' }, description: 'test names added and what each pins' },
    notes: { type: 'string' },
  },
}

const VERIFY_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['green', 'checks', 'problems'],
  properties: {
    green: { type: 'boolean' },
    checks: { type: 'array', items: { type: 'string' }, description: 'each check run and its outcome' },
    problems: { type: 'array', items: { type: 'string' }, description: 'failures needing a fix pass' },
  },
}

const DOC_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['files', 'gif', 'learnPointer', 'handoff'],
  properties: {
    files: { type: 'array', items: { type: 'string' } },
    gif: { type: 'string', description: 'path of the recorded GIF, or "" if handed off' },
    learnPointer: { type: 'string', description: 'docs/learn file updated/added, or ""' },
    handoff: { type: 'string', description: 'what remains for gooey-docs-and-demos, or ""' },
  },
}

const STAGING_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['stagingList', 'stray'],
  properties: {
    stagingList: { type: 'array', items: { type: 'string' }, description: 'explicit file paths to stage, from git status --porcelain -uall' },
    stray: { type: 'array', items: { type: 'string' }, description: 'untracked junk that should NOT be staged (binaries, logs)' },
  },
}

// ---------------------------------------------------------------------------

phase('Interview')
let interview = await agent(
  `You are running the Interview phase of the gooey-new-demo workflow in ${REPO}: the user wants a new demo${IDEA ? ` — their seed idea: "${IDEA}"` : ''} and YOUR job is to land a decided direction with them, not to design or build anything.
FIRST invoke the brainstorming process skill: call the Skill tool with skill "superpowers:brainstorming" and follow it — it owns this phase's process.
Ground yourself before asking anything: skim ${REPO}/README.md, ${REPO}/docs/demos.md, and the cmd/ + examples/ dirs so your options are real and you never propose a demo that already exists.
Then interview the user (AskUserQuestion, multiple rounds as needed): what should this demo PROVE, who is it for, which framework capabilities must it exercise (properties/damage, markup+hot reload, input/focus/mouse, MCP control plane, graphics, Temporal...), and what does "compelling" mean for it. When the conversation narrows to layout/shape, present 2-4 concrete alternatives as side-by-side ASCII mockups in option previews and let the user pick.
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
  // Read-only grounding for the decided direction; re-runs if the user
  // later restarts the interview with a different direction.
  grounding = (await parallel([
    () => agent(
      `Read-only exploration in ${REPO}. Map the framework capabilities this demo direction needs: ${JSON.stringify(interview.capabilities)} (demo: ${interview.name} — ${interview.purpose}). For each: the real APIs (exact package/type/function names, verified by reading source), the components in components/ it would use, and the markup elements/attributes involved (docs/markup-reference.md, then verify against markup/markup.go). Return a compact API brief a builder can trust without re-deriving.${GIT_RULES}`,
      { label: 'explore:capabilities', phase: 'Explore' },
    ),
    () => agent(
      `Read-only exploration in ${REPO}. Prior art for a new demo "${interview.name}" (${interview.purpose}): read the closest existing demos in cmd/ and examples/ (main.go + .gooey), note the canonical host-loop boilerplate, how each wires markup + viewmodel + input, and which patterns this demo should copy vs avoid. Also: cmd/ demos live in the root module; anything importing gooey/mcp (or other heavy deps) must be its own module under examples/ like examples/kanbandemo — say which this demo needs. Return a compact brief with file:line pointers.${GIT_RULES}`,
      { label: 'explore:prior-art', phase: 'Explore' },
    ),
    () => agent(
      `Read-only exploration in ${REPO}/docs/specs/. Find the 2026-08-10-*.md specs whose contracts govern this demo direction (capabilities: ${JSON.stringify(interview.capabilities)}), and extract the binding rules a builder must honor — including damage-count expectations, markup load-error contracts, and input/focus scoping rules. Cite spec file per rule. Return a compact contract sheet.${GIT_RULES}`,
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
      `You are running Design round ${round} of the gooey-new-demo workflow in ${REPO}. Nothing ships from this phase — you are here to get a design the USER approves, using mockups and a live prototype.
THE DIRECTION (user-decided): ${JSON.stringify(interview)}
GROUNDING (from the explore agents; trust it, spot-check what you build on): ${grounding}
${feedback.length ? `PRIOR ROUNDS' USER FEEDBACK, oldest first — the current round exists because of the last entry, address it concretely: ${JSON.stringify(feedback)}` : 'This is the first round: produce the initial design.'}
${harnessState ? `EXISTING HARNESS from earlier rounds (reuse it): ${harnessState}` : ''}
Process:
1. Invoke the Skill tool with skill "frontend-design:frontend-design" for aesthetic direction (terminal UI: its thinking about hierarchy, density, and intentionality applies even in cells).
2. Mockups the user can open: load the artifact-design skill (Skill tool) BEFORE writing the page, then publish an Artifact showing the design — screen sketches, layout grid, styling options, the key map, the beat-by-beat story of what the demo shows. Load artifact-diagramming if you draw structural diagrams. Keep ONE artifact across rounds: write to the same file path, and if a previous round returned an artifactUrl, pass it as \`url\` so the link stays stable.
3. The live prototype IS the design artifact that matters most:${HARNESS}
   Reproduce the current design direction in the harness's .gooey + viewmodel, drive it (send_keys / set_value / invoke_command), and capture screen_text at each story beat.
4. Put the beats in front of the user: AskUserQuestion with the screen_text snapshots (or tight excerpts) as previews where a visual choice is being made, and finish with the gate question — approve this design / revise (say what) / rethink the premise (back to interview). Their answer is your verdict verbatim: approved | revise | restart-interview. NEVER return approved without the user having chosen it this round.
${INTERACTION_RULES}
${INVARIANTS}
${GIT_RULES}`,
      { label: `design:round-${round}`, phase: 'Design', schema: DESIGN_SCHEMA, agentType: 'general-purpose', effort: 'high' },
    )
    if (!r) return { aborted: `design round ${round} skipped/died`, interview, lastFeedback: feedback }
    harnessState = `dir=${r.harnessDir} markup=${r.markupFile} artifact=${r.artifactUrl}`
    if (r.verdict === 'approved') { design = r; break }
    if (r.verdict === 'restart-interview') {
      log(`user sent design back to the interview after round ${round}`)
      interviewRounds++
      interview = await agent(
        `Re-run the Interview phase of gooey-new-demo (round ${interviewRounds}) in ${REPO}. The user rejected the current premise during design. Prior direction: ${JSON.stringify(interview)}. Their feedback when rejecting: ${JSON.stringify(r.userFeedback)}. Invoke Skill "superpowers:brainstorming" again, then re-interview with AskUserQuestion — start from what the rejection revealed, not from zero. Same bar: decided=true only on an explicit user pick.${INTERACTION_RULES}${GIT_RULES}`,
        { label: `interview:round-${interviewRounds}`, phase: 'Interview', schema: INTERVIEW_SCHEMA, agentType: 'general-purpose' },
      )
      if (!interview) return { aborted: 'interview re-run skipped/died', lastDesignFeedback: r.userFeedback }
      break // out of the design loop; outer while re-explores under the new direction
    }
    feedback.push(r.userFeedback) // 'revise' — loop
  }
}

phase('Build')
const build = await agent(
  `Build the approved demo for real in ${REPO}. This is the ONLY phase that writes production code, and the design below is user-approved — implement it, do not redesign it.
DIRECTION: ${JSON.stringify(interview)}
APPROVED DESIGN: ${JSON.stringify(design)}
GROUNDING: ${grounding}
Rules:
- Location: cmd/${interview.name}/ in the root module; examples/${interview.name}/ with its OWN go.mod only if it imports gooey/mcp or other quarantined deps (copy the module-header rationale style from examples/kanbandemo/go.mod).
- Markup-first: the UI is a .gooey file (start from the harness prototype at ${design.markupFile} — it is the approved design), Go holds the viewmodel/logic. Follow the house host-loop idiom from the closest existing demo.
- A package-header comment in main.go saying what the demo proves and its key map (the docs agents read these).
- Damage-count tests where behavior is non-trivial, following markup/tabs_test.go's style (build from markup, mutate a bound source, assert the exact painted count); skip them only where the demo is a straight composition of already-pinned components.
- Build your binary to /tmp to smoke it, never into the repo.
${INVARIANTS}
${GIT_RULES}`,
  { label: 'build', phase: 'Build', schema: BUILD_SCHEMA, effort: 'high' },
)
if (!build) return { aborted: 'build agent skipped/died', interview, design }

phase('Verify')
let verify = null
for (let attempt = 1; attempt <= 3; attempt++) {
  verify = await agent(
    `Verification pass ${attempt} for the new demo at ${build.location} in ${REPO}. ${attempt > 1 ? `Previous problems (fix them yourself in the demo's own files, then re-check; never patch framework code to make a demo pass — report that instead): ${JSON.stringify(verify?.problems)}` : ''}
Run and report each:
1. Root module: go vet ./... && go test ./... (NEVER go build ./... at the root — it drops binaries next to tracked ones).
2. Every nested module green: cd mcp && go test -race ./...; cd grpc && go test -race ./...; and every other dir with its own go.mod (discover with: command find ${REPO} -name go.mod -not -path '*/.claude/*') gets go vet + go test.
3. gofmt -l on every file the build reported: ${JSON.stringify(build.files)} — must be empty.
4. pty smoke: build the demo to /tmp, run it under script -qec with an explicit stty size, drive its key map (printf OCTAL escapes only — \\011 tab, \\015 enter; never \\x hex), quit cleanly, then extract the FINAL frame by feeding the trimmed output log through render.Screen (the last \\x1b[H in the log is the first flush of the final frame, not the whole frame — trim to it and replay). Confirm the money beat from the approved design is on screen: ${design.designSummary}
5. No stray binaries or junk: git status --porcelain -uall — every untracked path must be an intended demo file; flag executables or logs.
green=true only if ALL pass.${INVARIANTS}${GIT_RULES}`,
    { label: `verify:pass-${attempt}`, phase: 'Verify', schema: VERIFY_SCHEMA, effort: 'high' },
  )
  if (!verify) return { aborted: `verify pass ${attempt} skipped/died`, interview, design, build }
  if (verify.green) break
  log(`verify pass ${attempt} found problems: ${verify.problems.join('; ')}`)
}
if (!verify.green) return { failed: 'verification still red after 3 passes', problems: verify.problems, interview, design, build }

phase('Document')
const doc = await agent(
  `Document the new demo ${build.location} in ${REPO} (verified green). The demo proves: ${interview.purpose}. Design summary: ${design.designSummary}
1. Add its section to docs/demos.md matching the existing sections' shape (GIF embed from docs/media/demos/, headline, narrated beats, run command, key map, which subsystem it exercises).
2. The GIF, via the house capture pipeline: asciinema rec --overwrite --cols W --rows H -c "<script>" out.cast, then agg --theme dracula --font-size 14 out.cast ${REPO}/docs/media/demos/${interview.name}.gif. Choreography scripts run under dash: printf OCTAL escapes only (\\011 tab, \\015 enter, \\033[B down; never \\x hex), keyboard-only, and the pattern is ( sleeps + printf keys ) | script -qec "stty cols W rows H; /tmp/<binary>" /dev/null. Build the binary to /tmp first. VERIFY the GIF: convert -coalesce to frames and Read a mid and late frame as images — an empty UI or missed keystroke is a failure; re-choreograph (up to 3 attempts). If the pipeline tools (asciinema/agg/convert) are missing, skip recording and say exactly what to hand to the gooey-docs-and-demos workflow instead.
3. If the demo teaches something no docs/learn/ page covers, ask the user (AskUserQuestion) whether they want a docs/learn pointer now — if yes, add the entry (follow docs/learn/index.md's structure); if no, note it in handoff.
${INTERACTION_RULES}${GIT_RULES}`,
  { label: 'document', phase: 'Document', schema: DOC_SCHEMA, agentType: 'general-purpose' },
)

const staging = await agent(
  `Final read-only sweep in ${REPO}: run git status --porcelain -uall NOW (not from memory) and derive the staging list for the demo work just completed (demo ${build.location}, docs ${JSON.stringify(doc?.files || [])}, GIF ${doc?.gif || 'none'}). stagingList = explicit file paths (never directories) that belong to this work; stray = untracked junk that must NOT be staged (binaries, .cast files, logs). Anything untracked that is neither, list under stray with a note.${GIT_RULES}`,
  { label: 'staging-list', phase: 'Document', schema: STAGING_SCHEMA },
)

return {
  demo: build.location,
  interview: { name: interview.name, purpose: interview.purpose, compelling: interview.compelling },
  designArtifact: design.artifactUrl,
  files: build.files,
  verification: verify.checks,
  docs: doc ? { files: doc.files, gif: doc.gif, learnPointer: doc.learnPointer, handoff: doc.handoff } : 'document agent skipped',
  staging: staging || 'staging agent skipped — coordinator must derive the list itself',
  note: 'Nothing is committed. The coordinator stages the explicit paths above and commits.',
}
