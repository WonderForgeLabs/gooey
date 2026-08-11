# Workflow-driven demo and component development

**Status:** executed (the workflows exist; they govern future demo/component work).

**Date:** 2026-08-10

## What shipped

Three interactive workflow scripts in `.claude/workflows/`, registered
by `meta.name` so `Workflow({ name: ... })` resolves them:

- `gooey-new-demo` — Interview → Explore → Design → Build → Verify →
  Document, for new demos.
- `gooey-new-component` — the same front half, then Spec → Epic → Build
  → Reconcile → Verify, for new `components/` citizens.
- `gooey-epic-decompose` — doc → project-ops epic with per-section child
  issues and doc backlinks; standalone, and invoked by
  `gooey-new-component` after its spec lands.

Plus their entries in `.claude/workflows/README.md`.

## The decision in one sentence

New demos and components start with an interview and a user-approved
live prototype, not with code — and a component is not "done" until
every doc claim and every hand-rolled equivalent in the repo has been
reconciled with its existence.

## The decisions

### Interview-first, and the workflow owns the gate

Both pipelines open by invoking the brainstorming process skill and then
interviewing the user with concrete options (ASCII layout previews, not
open prose questions). Nothing advances past Design until the user
explicitly approves — the scripts encode the gate as control flow
(`verdict: approved | revise | restart-interview`), so a rejection loops
Design and a premise rejection re-enters Interview. The alternative — a
fire-and-forget fan-out that guesses the design — is exactly what these
workflows exist to replace: guessed designs produced demos that proved
the wrong thing.

### The design artifact is a running prototype, not a drawing

Every design round builds a scratch module under `/tmp` (its own
`go.mod` with `replace` directives — `mcp/` is a nested module, so
anything importing it needs one) hosting the in-progress UI or
component, wired to `mcp.Serve` and markup hot reload, and drives it
over the MCP control plane (`swap_markup`, `send_keys`, `send_mouse`,
`screen_text`). `screen_text` captures are the iteration artifact the
user judges, alongside a published mockup artifact. For components the
harness is the core mechanic: the component does not exist yet, so
build-run-inspect against the real framework interfaces is the only
honest prototype.

### Spec before implementation, epic from the spec

A component gets its `docs/specs/` decision record after design
approval and before any implementation, in the house shape (decisions
with the alternatives they beat, markup contract with load errors,
damage pins, "Not here"), with `## Executed` appended only after Verify
proves the build out. The spec then feeds `gooey-epic-decompose`: an
epic plus child issues each tied to a spec section, the section carrying
a `Tracked: #NNN` backlink. Epic decomposition is its own workflow
rather than a phase because it is useful against any doc, and because
the user gate (approve the exact issue list before anything is filed)
deserves a single implementation.

### Reconciliation is a phase, not a follow-up

Shipping a component makes existing text false: docs that say "gooey
has no X", README matrix rows, other specs' `## Executed` sections,
and every hand-rolled equivalent in shipped code. `gooey-new-component`
surveys all of it read-only, then fans out updaters with **disjoint
write sets** stated in each brief (core docs / learn docs / other
specs / one agent per adoption site / the `docs-and-demos.js` script),
so concurrent agents cannot collide. The Tabs precedent is the bar:
kanbandemo's hand-rolled switcher was deleted in the same change that
shipped Tabs, while `cmd/reader`'s panes were judged contract surface
and left alone — the survey asks that question about every site.

### Agents never touch the git index

Every brief carries the same discipline block: no mutating git (plain
`mv`/`rm` only), no `git add` of any kind, binaries to `/tmp`,
`command grep` for gitignored paths, staging reported as explicit file
paths from `git status --porcelain -uall` (a collapsed untracked
directory hides its contents), and state re-read in the turn it is
asserted. The workflows return a staging list; the coordinator that
invoked them stages and commits.

## Not here

Automation triggers (nothing runs these workflows unattended — they are
interactive by design), a general-purpose "feature" workflow for
non-demo non-component work, and CI enforcement of the reconciliation
contract.
