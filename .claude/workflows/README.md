# Workflows

Multi-agent workflow scripts for this repo, run with the `Workflow`
tool. They are opt-in: nothing runs them automatically.

Two kinds live here. `docs-and-demos` is a fire-and-forget fan-out:
launch it and read the result. The other three are **interactive** —
their agents interview the user with `AskUserQuestion`, publish mockup
artifacts, and loop on feedback; expect them to pause on questions, and
run them only when the user is present. None of them ever commits: each
returns an explicit staging list of file paths and leaves the git index
to the coordinator that invoked it.

Which one:

- A new **demo** (something in `cmd/` or `examples/` that shows the
  framework off) → `gooey-new-demo`.
- A new **component** (something in `components/` with a markup builder,
  damage pins, and docs/adopters to reconcile) → `gooey-new-component`.
- A design doc that should become **tracked issues** →
  `gooey-epic-decompose` (also called automatically by
  `gooey-new-component` after its spec lands).
- The demos or docs are **stale** after a change → `gooey-docs-and-demos`.

## gooey-new-demo

Design-first pipeline for a new demo: Interview (brainstorming skill +
real questions with ASCII layout previews) → Explore (capability map,
prior art, spec contracts) → Design (frontend-design skill, a published
mockup artifact, and a live prototype — a scratch module under /tmp
wired to `mcp.Serve` and hot reload, driven over MCP with
`swap_markup`/`send_keys`/`screen_text`) → Build (only after the user
approves; markup-first, damage-count tests) → Verify (all modules, pty
smoke with final-frame extraction, no stray binaries) → Document
(`docs/demos.md` entry, GIF via the capture pipeline, optional
`docs/learn/` pointer).

```
Workflow({ name: 'gooey-new-demo' })
Workflow({ name: 'gooey-new-demo', args: { idea: 'a spreadsheet demo' } })
```

The Design loop re-enters on rejection: "revise" iterates, "rethink the
premise" goes back to the Interview. Cost varies with rounds — typically
8–15 agents.

## gooey-new-component

Everything gooey-new-demo does, plus the parts that make a component a
framework citizen. The prototype harness is the core mechanic (the
component doesn't exist yet, so every design round compiles a scratch
app hosting the in-progress component and drives it over MCP). After the
user approves: a `docs/specs/` decision record BEFORE implementation
(`## Executed` appended after, by Verify), a project-ops epic filed from
the spec via `gooey-epic-decompose`, the build in `components/` (markup
builder + load-error cases + damage pins + an explicit invariant
checklist in the result), and then the reconciliation fan-out that is
the point: parallel agents with disjoint write sets update every stale
doc claim (`docs/markup-reference.md`, `docs/learn/**`, README matrix,
`docs/architecture.md`, other specs' `## Executed`), adopt the component
wherever shipped code hand-rolls it (the Tabs/kanbandemo model — the
hand-rolled version is deleted in the same change), audit/update
`docs-and-demos.js` itself, and offer the user the full GIF/doc regen.

```
Workflow({ name: 'gooey-new-component' })
Workflow({ name: 'gooey-new-component', args: { idea: 'a DatePicker' } })
```

Run it top-level only (it nests `gooey-epic-decompose` and, user-gated,
`gooey-docs-and-demos`). Cost: 15–25 agents before the optional regen.

## gooey-epic-decompose

Standalone: turn any design doc into a project-ops epic. Plan (decompose
the doc into child issues each tied to a specific `##` section; the user
approves the exact list before anything is filed) → File (epic + child
issues in the house shape, explicit board add — the gooey board has no
auto-add — Status/Priority/Design Doc fields, dependency wiring) → Xref
(each doc section gets a `Tracked: #NNN` backlink; staging list
returned).

```
Workflow({ name: 'gooey-epic-decompose',
           args: { doc: 'docs/specs/2026-08-10-foo.md' } })
```

Optional `args`: `title` (epic title), `context` (framing from a calling
workflow). Cost: ~3 agents plus one gh-driven filing pass.

## docs-and-demos.js

Re-records every demo GIF and regenerates the documentation set
(`docs/architecture.md`, `docs/getting-started.md`,
`docs/markup-reference.md`, `docs/demos.md`, `README.md`), then
verifies the result — links resolve, documented identifiers exist in
the source, the getting-started program compiles, `go build ./...` and
`go test ./...` are green.

Run it after a change that alters what the demos look like or what the
docs claim: an input-system change, a new component, a renamed API.

Prepare the scratch directory first — the recording agents assume it,
and they use the prebuilt binaries deliberately, so that a tree being
edited elsewhere cannot break a recording mid-run:

```sh
S=/tmp/gooey-recordings
mkdir -p $S
for d in probe demo propdemo logview markuplog finder reader statedemo; do
  go build -o $S/$d ./cmd/$d
done
cp cmd/*/*.gooey $S/
```

Then:

```
Workflow({ scriptPath: '.claude/workflows/docs-and-demos.js' })
```

Optional `args`: `{ repo: '/path/to/gooey', scratch: '/tmp/gooey-rec' }`.

Requires `asciinema` 2.x, `agg`, and ImageMagick `convert` on PATH. The
recorders drive each demo keyboard-only through a pty (`script -qec`
with an explicit `stty` size) because a pty cannot synthesize mouse
input, and each verifies its own GIF by extracting frames before
reporting success.

Cost: 12 agents, roughly 700k output tokens and ~12 minutes wall clock
for a full run.
