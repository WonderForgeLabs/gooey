export const meta = {
  name: 'docs-validation-sweep',
  description: 'Validate every doc against the implementation; verify findings adversarially; collect issue/PR xref candidates',
  whenToUse: 'When the docs need a full truth audit: every checkable claim in README/CLAUDE.md/docs/** checked against the code, findings adversarially verified, and cross-reference links to issues/PRs proposed. Read-only: it edits nothing and files nothing — pair with docs-fix-and-xref for the write half.',
  phases: [
    { title: 'Validate', detail: 'one finder per doc cluster, docs vs code' },
    { title: 'Verify', detail: 'adversarial re-check of every finding' },
    { title: 'Coverage', detail: 'diff the cluster map against the real doc tree' },
  ],
}

// The shape, and why it is this shape:
//
// - One finder per DOC CLUSTER, not per claim: a claim is only checkable
//   with its surrounding doc in view, and the same code files answer for
//   a whole cluster, so the read amortizes.
// - Every finding flows straight into an adversarial verifier
//   (pipeline, no barrier — cluster A verifies while cluster B still
//   reads). The verifier is told to REFUTE and to default to refuted on
//   thin evidence. On the first run of this workflow the verifiers
//   killed 2 findings and corrected severities on several more; unverified
//   findings become GitHub issues, so false positives are issue noise.
// - Decision records under docs/specs/ get SWEEP MODE: they are
//   historical, so code drift is expected and only presented-as-current
//   falsehoods count. They are, however, the richest xref target — each
//   should trail back to its epic and landing PRs.
// - The cluster map below is an enumeration, and this repo's doctrine is
//   that enumerations rot. The Coverage phase is the derived check: it
//   diffs the union of assigned files against the real tree and reports
//   any doc no cluster owns, so a new page fails loudly instead of
//   escaping the audit.
//
// args:
//   index    (optional) path to a pre-built index of the repo's GitHub
//            issues and PRs ("#N STATE title" lines). The coordinator
//            builds it (mcp__github__list_issues / list_pull_requests)
//            and passes the path; without it the xref deliverable is
//            skipped, the audit still runs.
//
// Returns { summary, units: [{unit, findings: [... with .verdict], xrefs}] }.
// The coordinator then: files issues for confirmed code-bug /
// functional-gap findings (grouped by area), splits `units` into per-unit
// JSON files, and runs docs-fix-and-xref over them.
//
// Sizing: ~29 agents (14 finders + up to 14 verifiers + 1 coverage),
// beyond the default medium guideline — launch it deliberately.

const INDEX = args && args.index

const FINDINGS_SCHEMA = {
  type: 'object',
  required: ['findings', 'xrefs'],
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        required: ['doc', 'location', 'claim', 'reality', 'evidence', 'category', 'severity', 'area', 'suggested_fix'],
        properties: {
          doc: { type: 'string', description: 'repo-relative doc path' },
          location: { type: 'string', description: 'section heading and/or line range in the doc' },
          claim: { type: 'string', description: 'what the doc says, quoted or tightly paraphrased' },
          reality: { type: 'string', description: 'what the code actually does / what is actually true' },
          evidence: { type: 'string', description: 'file:line references and short quotes proving the reality' },
          category: { enum: ['doc-inaccuracy', 'code-bug', 'functional-gap', 'missing-doc', 'stale-reference'] },
          severity: { enum: ['high', 'medium', 'low'] },
          area: { type: 'string', description: 'one of: markup, layout, components, input, rendering, prop-graph, control-plane, handlers-packs, term-tty, apps-demos, docs-infra, ci' },
          suggested_fix: { type: 'string' },
          confidence: { enum: ['high', 'medium', 'low'] },
        },
      },
    },
    xrefs: {
      type: 'array',
      items: {
        type: 'object',
        required: ['doc', 'location', 'target', 'why'],
        properties: {
          doc: { type: 'string' },
          location: { type: 'string' },
          target: { type: 'string', description: 'e.g. "#216" for an issue, "PR #283" for a pull request' },
          why: { type: 'string' },
          anchor_text: { type: 'string', description: 'suggested inline phrasing, e.g. "tracked in #216"' },
        },
      },
    },
  },
}

const VERDICT_SCHEMA = {
  type: 'object',
  required: ['verdicts'],
  properties: {
    verdicts: {
      type: 'array',
      items: {
        type: 'object',
        required: ['index', 'verdict', 'note'],
        properties: {
          index: { type: 'number', description: '0-based index into the findings array you were given' },
          verdict: { enum: ['confirmed', 'refuted', 'uncertain'] },
          corrected_category: { enum: ['doc-inaccuracy', 'code-bug', 'functional-gap', 'missing-doc', 'stale-reference'] },
          corrected_severity: { enum: ['high', 'medium', 'low'] },
          corrected_reality: { type: 'string' },
          note: { type: 'string', description: 'the fresh evidence (file:line) that decided the verdict' },
        },
      },
    },
  },
}

const XREF_BRIEF = INDEX
  ? `\nSECOND DELIVERABLE — xrefs: read ${INDEX} (an index of this repo's GitHub
issues and PRs). Propose cross-reference links from your docs to issues/PRs
so readers see related and tracked work: a section describing a feature
links to the PR that landed it or the epic tracking its future; a
documented limitation links to the open issue tracking its removal. Only
high-confidence, genuinely useful links; 0 for a section is fine.`
  : `\nNo issue/PR index was provided this run: return an empty xrefs array.`

const COMMON = `
You are auditing documentation of the gooey repo (this session's working
directory) against its actual implementation. READ-ONLY: do not edit,
create, or delete any file; do not run go test or go build; reading code,
grep, and go doc are your tools.

For every CHECKABLE claim in your assigned docs, check it against the code:
file paths and file:line anchors, exported API names and signatures, markup
element/attribute names, component lists and counts, key bindings, command
invocations and flags, described runtime behavior (read the implementing
function), and described defaults. Chase the claim to its source file and
quote the deciding lines as evidence.

Report MATERIAL findings only — things that would mislead a reader or point
at a real defect. Not style, not tone. Classify:
- doc-inaccuracy: the code is sensible, the doc is wrong/stale.
- code-bug: the doc states the intended contract and the code violates it.
- functional-gap: the doc promises a capability that does not exist.
- missing-doc: a shipped, user-facing behavior the docs should cover and
  don't (only substantial ones).
- stale-reference: a path/name/anchor that moved.
Set confidence honestly; a verifier will try to refute every finding.
${XREF_BRIEF}

Return via the structured-output tool only. Empty arrays are valid results.`

const SPEC_MODE = `SPEC SWEEP MODE: these are decision records — historical documents. Do NOT
flag drift between a spec and today's code as ordinary inaccuracy; that is
expected. Flag ONLY: (a) a spec asserting something as CURRENT state that
was never true or is dangerously misleading with no superseding note (the
repo convention is an amendment note at the top); (b) a status/outcome
section that says shipped/decided when it was not, or vice versa; (c) file
paths a later rename moved, in specs that lack a pointer to the rename's
path map. For xrefs, specs are the richest target: each spec should link to
its epic/issues and the PR(s) that landed it — propose those generously
(still only where the mapping is certain).`

// The cluster map. Keys must stay unique; models: opus where the doc is
// claim-dense and load-bearing, sonnet elsewhere. The Coverage phase
// checks this map's completeness against the tree — extend it when docs
// grow a new area.
const UNITS = [
  { key: 'readme', model: 'opus', docs: 'README.md and docs/getting-started.md', extra: 'README makes feature/status claims and shows code samples — check each sample against current APIs. getting-started shows install/run commands — check paths exist.' },
  { key: 'architecture', model: 'opus', docs: 'docs/architecture.md', extra: 'The grounded walkthrough; it cites many file:line anchors — verify each still points at what the doc says. Walk it top to bottom.' },
  { key: 'claudemd', model: 'opus', docs: 'CLAUDE.md', extra: 'Partially pinned by tests — read claudemd_test.go and ciworkflow_test.go first to learn which claims are derived, and do not flag those. Verify the rest: every file:line anchor, every count, every named test, every command behavior claim.' },
  { key: 'markup-ref', model: 'opus', docs: 'docs/markup-reference.md', extra: 'Cross-check against the markup package: every documented element, attribute, x: construct, binding form, pipeline stage, and value namespace must exist with the documented semantics; also note substantial shipped markup features the reference omits.' },
  { key: 'demos-index', model: 'sonnet', docs: 'docs/demos.md and docs/learn/index.md', extra: 'Check every listed demo/app/example against cmd/, apps/, and docs/learn/examples/. Check media references point at files that exist. Check the learn index TOC against the real file set.' },
  { key: 'tut-1-3', model: 'sonnet', docs: 'docs/learn/01-first-app.md, 02-layout.md, 03-binding-and-state.md', extra: 'Tutorials carry full code listings — check each against current APIs and the matching example under docs/learn/examples/; flag listings that no longer compile as written.' },
  { key: 'tut-4-6', model: 'sonnet', docs: 'docs/learn/04-input-commands.md, 05-usercontrols.md, 06-custom-components.md', extra: 'Code listings vs current APIs (Action/Command semantics, x:Property rules, Container/MeasureChild contract).' },
  { key: 'tut-7-9', model: 'sonnet', docs: 'docs/learn/07-app-chrome.md, 08-remote-control.md, 09-temporal.md', extra: 'Chrome components as described; remote-control claims vs mcp/, grpc/, control/; temporal tutorial vs handlers/temporal and packs/.' },
  { key: 'concepts', model: 'opus', docs: 'every file under docs/learn/concepts/', extra: 'Distilled internals claims — hold them to architecture.md standard; verify against composer.go, prop/, input.go, mouse.go, app.go, markup/.' },
  { key: 'howto-a', model: 'sonnet', docs: 'docs/learn/howto/: howto-async, howto-companions, howto-custom-draw, howto-embed-release, howto-format, howto-forms, howto-handlers (.md)', extra: 'Check recipes against current APIs (Dispatcher.Post, Startable contract, Companion, format/ constructors, validation, handlers/).' },
  { key: 'howto-b', model: 'sonnet', docs: 'docs/learn/howto/: howto-hot-reload, howto-images, howto-keybindings, howto-lists, howto-mouse, howto-popup, howto-testing (.md)', extra: 'Check recipes against the fs.FS/watcher seam, imagefmt/ and Image tier rules, KeyBinding scoping, ItemsView, Popup, and the headless-testing recipe — verify commands and helper names exist.' },
  { key: 'specs-a', model: 'sonnet', docs: 'docs/specs/2026-08-10-{activity-islands,adornments,bindable-visibility,browser-branches,colorpicker-pixel,companions,container-backgrounds,datatemplates,exec-pack,format-constructors,fs-pack,grpc-contract,image-formats}.md', extra: SPEC_MODE },
  { key: 'specs-b', model: 'sonnet', docs: 'docs/specs/2026-08-10-{input-2,markup-companions,markup-declared-properties,mcp-server,pack-distribution,package-reorg,pipeline-grammar-v2,popup,reader-design,remote-handlers-design,rendering-2,root-package-facade,runtime-signals,styles-and-resources}.md', extra: SPEC_MODE },
  { key: 'specs-c', model: 'sonnet', docs: 'docs/specs/2026-08-10-{tabs,temporal-visibility-stdlib,toolkit-wave1,toolkit-wave2,tty-read-lifecycle,validation-core,workflow-driven-development}.md and every docs/specs/ file dated after 2026-08-10', extra: SPEC_MODE },
]

phase('Validate')
log(`fanning out ${UNITS.length} doc-cluster validators`)

const results = await pipeline(
  UNITS,
  u => agent(
    `${COMMON}\n\nYOUR ASSIGNED DOCS: ${u.docs}\n\nCLUSTER-SPECIFIC GUIDANCE: ${u.extra}`,
    { label: `validate:${u.key}`, phase: 'Validate', schema: FINDINGS_SCHEMA, model: u.model }
  ),
  (found, u) => {
    if (!found || !found.findings || found.findings.length === 0) {
      return { unit: u.key, findings: [], xrefs: (found && found.xrefs) || [] }
    }
    const list = found.findings.map((f, i) => `[${i}] ${JSON.stringify(f)}`).join('\n')
    return agent(
      `You are an adversarial verifier for the gooey repo (this session's
working directory). READ-ONLY (no edits, no go test/build). Below are
documentation-audit findings from another agent. For EACH finding,
independently re-derive the truth from the repo: open the doc at the cited
location, open the code, and decide.
- confirmed: the doc really says that, the reality really is that, category
  and severity are right. Cite your own file:line evidence in the note.
- refuted: the doc does not say that, or the code actually matches the doc,
  or the claim mischaracterizes; default to refuted when evidence is thin.
- uncertain: genuinely undecidable by reading (e.g. needs a live terminal).
If directionally right but off in detail, verdict confirmed with
corrected_reality / corrected_category / corrected_severity filled in.
Every index 0..${found.findings.length - 1} must get exactly one verdict.

FINDINGS:\n${list}`,
      { label: `verify:${u.key}`, phase: 'Verify', schema: VERDICT_SCHEMA, model: 'opus', effort: 'high' }
    ).then(v => ({ unit: u.key, findings: found.findings, xrefs: found.xrefs || [], verdicts: (v && v.verdicts) || null }))
  }
)

phase('Coverage')
const assigned = UNITS.map(u => `${u.key}: ${u.docs}`).join('\n')
const coverage = await agent(
  `In the gooey repo (this session's working directory), run:
  find docs -name '*.md' | sort
and compare the result — plus README.md and CLAUDE.md — against this
cluster map from a docs-audit workflow:\n\n${assigned}\n\nReturn the list of
markdown files that NO cluster covers (empty list if the map is complete),
and any cluster entry that names a file which no longer exists. Read-only.`,
  { label: 'coverage-check', phase: 'Coverage', model: 'haiku',
    schema: { type: 'object', required: ['uncovered', 'stale_entries'], properties: {
      uncovered: { type: 'array', items: { type: 'string' } },
      stale_entries: { type: 'array', items: { type: 'string' } } } } }
)
if (coverage && coverage.uncovered && coverage.uncovered.length) {
  log(`COVERAGE GAP — no cluster owns: ${coverage.uncovered.join(', ')}`)
}

const units = results.filter(Boolean)
let confirmed = 0, refuted = 0, uncertain = 0, unverified = 0, xrefs = 0
for (const u of units) {
  xrefs += (u.xrefs || []).length
  if (!u.findings.length) continue
  if (!u.verdicts) { unverified += u.findings.length; continue }
  for (const f of u.findings) f.verdict = 'unverified'
  for (const v of u.verdicts) {
    const f = u.findings[v.index]
    if (!f) continue
    f.verdict = v.verdict
    f.verdict_note = v.note
    if (v.corrected_category) f.category = v.corrected_category
    if (v.corrected_severity) f.severity = v.corrected_severity
    if (v.corrected_reality) f.reality = v.corrected_reality
    if (v.verdict === 'confirmed') confirmed++
    else if (v.verdict === 'refuted') refuted++
    else uncertain++
  }
  unverified += u.findings.filter(f => f.verdict === 'unverified').length
}
log(`verdicts: ${confirmed} confirmed, ${refuted} refuted, ${uncertain} uncertain, ${unverified} unverified; ${xrefs} xref candidates`)

return { summary: { confirmed, refuted, uncertain, unverified, xrefs }, coverage, units }
