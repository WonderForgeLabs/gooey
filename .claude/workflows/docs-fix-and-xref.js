export const meta = {
  name: 'docs-fix-and-xref',
  description: 'Apply confirmed docs-validation-sweep findings and issue/PR cross-references, one editor per doc cluster',
  whenToUse: 'The write half of a docs audit: after docs-validation-sweep has produced verified findings, the coordinator has filed tracking issues for the code-level ones, and the per-unit findings JSON has been written to disk. Editors fix the docs and weave in the cross-reference links; code-level defects get a breadcrumb to their tracking issue instead of a silent rewrite.',
  phases: [{ title: 'Edit', detail: 'one editor per doc cluster, disjoint files' }],
}

// Sequenced AFTER issue filing on purpose: the whole point of the
// breadcrumbs is that a doc describing a known-broken behavior links the
// issue that tracks the fix, and the numbers only exist once the
// coordinator has filed them. The flow is:
//
//   1. docs-validation-sweep  → verified findings + xref candidates
//   2. coordinator: group confirmed code-bug/functional-gap findings by
//      area, file one issue per area (epic + children only if the volume
//      earns it), note which findings are already tracked by open PRs
//   3. coordinator: write each sweep unit to <unitsDir>/<key>.json
//   4. this workflow, with args:
//        unitsDir  (required) directory of per-unit findings JSON
//        index     (optional) the same issue/PR index the sweep used,
//                  for editors to sanity-check xref targets against
//        issueMap  (optional) free text mapping finding topics to the
//                  issue/PR links filed in step 2 — pasted verbatim into
//                  every editor brief, so keep it short and specific
//   5. coordinator: review the diff, run the repo's verify loop, commit
//
// Editors run in ONE checkout concurrently, which is safe here for the
// same reason gooey-new-component needs worktrees elsewhere: ownership.
// Each cluster's file set is disjoint by construction, and the briefs ban
// git mutation, so there is no shared path for last-writer-wins to fork.
// If you re-cluster, keep the sets disjoint or move to worktrees.
//
// This map must mirror docs-validation-sweep's UNITS (same keys, same
// files) — the sweep's Coverage phase is the staleness check for both.

const UNITS_DIR = args && args.unitsDir
if (!UNITS_DIR) throw new Error('docs-fix-and-xref: args.unitsDir is required')
const INDEX = args && args.index
const ISSUE_MAP = (args && args.issueMap) || 'No tracking-issue map was provided this run: for code-bug/functional-gap findings, state actual behavior without a tracking link and list the finding in your report as needing one.'

const REPORT_SCHEMA = {
  type: 'object',
  required: ['changes', 'skipped'],
  properties: {
    changes: { type: 'array', items: { type: 'object', required: ['doc', 'summary'], properties: { doc: { type: 'string' }, summary: { type: 'string' } } } },
    skipped: { type: 'array', items: { type: 'object', required: ['item', 'reason'], properties: { item: { type: 'string' }, reason: { type: 'string' } } } },
    tests_run: { type: 'string' },
  },
}

const COMMON = `
You are applying documentation fixes in the gooey repo (this session's
working directory), on the branch already checked out. Your input is a JSON
file of VERIFIED audit findings and cross-reference proposals for the doc
files you own. Edit ONLY the markdown files you own, listed below. Never
touch Go source, never mutate git (no add/commit/push) — the coordinator
commits.

STEP 1 — read your findings file. Apply every finding with verdict
"confirmed" (skip "refuted"). Honor corrected_reality/verdict_note where
present — the verifier's evidence supersedes the finder's. Rules by category:
- doc-inaccuracy / stale-reference: surgically rewrite the passage so it
  matches reality. Preserve the doc's voice (terse, lowercase-leaning,
  em-dash heavy). Smallest edit that makes it true; don't restructure.
- missing-doc: add CONCISE coverage — a sentence, a short paragraph, or a
  table row. No new pages, no long sections.
- code-bug / functional-gap: the code is wrong, not the doc's intent. Do
  NOT silently rewrite the doc to bless the bug. State today's actual
  behavior and add an inline tracking link per this run's map:
${ISSUE_MAP}
  Example phrasing: "…does not yet cross the control boundary (tracked in
  [#NNN](https://github.com/OWNER/REPO/issues/NNN))."
- If a fix requires renumbering a file:line anchor, open the code file and
  derive the CURRENT correct line number — never guess.

STEP 2 — apply your xrefs list: inline cross-reference links from doc
passages to GitHub issues/PRs. Judgment applies${INDEX ? `: verify each target
against ${INDEX} (number + title must match what the xref claims)` : ''}; skip any
that look wrong, redundant, or noisy. GitHub does NOT auto-link #N inside
repo markdown, so always write explicit links:
  [#96](https://github.com/WonderForgeLabs/gooey/issues/96)
  [PR #143](https://github.com/WonderForgeLabs/gooey/pull/143)
Style: inline, short anchor text, at the point where the reader benefits —
"the Popup primitive ([PR #143](…))", "tracked in [#67](…)". For decision
records (docs/specs/*) with 3+ links, prefer a short "## Related work"
section at the end of the spec (epic, child issues, landing PRs — a
breadcrumb trail), plus at most a couple of inline links where they earn
their place. Do not duplicate a link the doc already has.

STEP 3 — re-read your final diffs (git diff -- <your files>) for coherence:
no broken markdown, no contradictions left half-edited.

Return via the structured-output tool: per-doc change summaries, skipped
items with reasons, and tests_run ('' if none).`

const UNITS = [
  { key: 'readme', model: 'opus', files: 'README.md and docs/getting-started.md' },
  { key: 'architecture', model: 'opus', files: 'docs/architecture.md' },
  { key: 'claudemd', model: 'opus', files: 'CLAUDE.md', extra: `
EXTRA CARE — CLAUDE.md is pinned by tests. Before editing, read
claudemd_test.go and ciworkflow_test.go to learn exactly what is derived:
the verify-loop shell block and the -race case arm are compared
character-for-character against ci.yml — do NOT touch those code blocks.
File:line anchors in prose are yours to fix: derive each current line from
the code. After editing, run: go test -run 'CLAUDEMD|CIWorkflow' .
from the repo root and iterate until green; report the result in tests_run.` },
  { key: 'markup-ref', model: 'opus', files: 'docs/markup-reference.md' },
  { key: 'demos-index', model: 'sonnet', files: 'docs/demos.md and docs/learn/index.md' },
  { key: 'tut-1-3', model: 'sonnet', files: 'docs/learn/01-first-app.md, 02-layout.md, 03-binding-and-state.md' },
  { key: 'tut-4-6', model: 'sonnet', files: 'docs/learn/04-input-commands.md, 05-usercontrols.md, 06-custom-components.md' },
  { key: 'tut-7-9', model: 'sonnet', files: 'docs/learn/07-app-chrome.md, 08-remote-control.md, 09-temporal.md' },
  { key: 'concepts', model: 'opus', files: 'every file under docs/learn/concepts/' },
  { key: 'howto-a', model: 'sonnet', files: 'docs/learn/howto/: howto-async, howto-companions, howto-custom-draw, howto-embed-release, howto-format, howto-forms, howto-handlers (.md)' },
  { key: 'howto-b', model: 'sonnet', files: 'docs/learn/howto/: howto-hot-reload, howto-images, howto-keybindings, howto-lists, howto-mouse, howto-popup, howto-testing (.md)' },
  { key: 'specs-a', model: 'sonnet', files: 'docs/specs/2026-08-10-{activity-islands,adornments,bindable-visibility,browser-branches,colorpicker-pixel,companions,container-backgrounds,datatemplates,exec-pack,format-constructors,fs-pack,grpc-contract,image-formats}.md' },
  { key: 'specs-b', model: 'sonnet', files: 'docs/specs/2026-08-10-{input-2,markup-companions,markup-declared-properties,mcp-server,pack-distribution,package-reorg,pipeline-grammar-v2,popup,reader-design,remote-handlers-design,rendering-2,root-package-facade,runtime-signals,styles-and-resources}.md' },
  { key: 'specs-c', model: 'sonnet', files: 'docs/specs/2026-08-10-{tabs,temporal-visibility-stdlib,toolkit-wave1,toolkit-wave2,tty-read-lifecycle,validation-core,workflow-driven-development}.md and every docs/specs/ file dated after 2026-08-10' },
]

phase('Edit')
log(`fanning out ${UNITS.length} doc editors with disjoint file ownership`)

const reports = await parallel(UNITS.map(u => () =>
  agent(
    `${COMMON}\n\nFILES YOU OWN: ${u.files}\nYOUR FINDINGS FILE: ${UNITS_DIR}/${u.key}.json${u.extra || ''}`,
    { label: `edit:${u.key}`, phase: 'Edit', schema: REPORT_SCHEMA, model: u.model }
  ).then(r => ({ unit: u.key, ...(r || {}) }))
))

const done = reports.filter(Boolean)
log(`${done.length}/${UNITS.length} editors reported`)
return done
