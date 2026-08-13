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

Two guards keep concurrent agents from clobbering each other, and they
are not the same guard. Every agent brief bans mutating git outright —
no `add`/`commit`/`push`, plain `mv`/`rm` only — which protects the
*index*. Separately, the one phase with genuinely parallel writers (the
`gooey-new-component` reconciliation fan-out) runs each agent in its own
git worktree with a disjoint write set, which protects the *working
tree*: two agents editing one path in one checkout is a silent
last-writer-wins fork, and no amount of index discipline prevents it.
A worktree is branched from a commit and these workflows never commit,
so each updater seeds its tree with the uncommitted build first —
otherwise an adopter cannot compile the component it is adopting. A
collection step copies the edits back, stopping on any collision and
refusing any file the shared checkout has moved on from since that
worktree branched, rather than picking a winner. Sequential
single-writer phases stay in the main checkout, where isolation would
add merge-back cost for no concurrency benefit.

**Requires** (the three interactive workflows): the `superpowers`
plugin (`superpowers:brainstorming`), `frontend-design`
(`frontend-design:frontend-design`), the `artifact-design` and
`artifact-diagramming` skills, and — for `gooey-epic-decompose`, plus
`gooey-new-component` which calls it — `project-ops`
(`project-ops:manage-dependencies`) with a configured
`.claude/project-ops.yaml` and an authenticated `gh`. Their agents
invoke these by name through the Skill tool; if a name does not resolve
in the environment you launch from, the phase fails with an opaque skill
error rather than a signpost, so check they are installed first.

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
the point: parallel agents, each in **its own git worktree** with a
disjoint write set, update every stale doc claim (`docs/markup-reference.md`, `docs/learn/**`, README matrix,
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

## peer-canvass

Before dispatching work in a repo where several sessions are active:
audit worktrees, branches, dirty paths and open PRs empirically, then
reconcile that against what peers *said* they were doing.

```
Workflow({ name: 'peer-canvass' })
Workflow({ name: 'peer-canvass', args: { replies: '...peer summaries...' } })
```

Workflow agents cannot `SendMessage`, so the coordinator asks peers
directly and passes the answers in as `args.replies`. The highest-value
field is `reconciliation` — where the two sources **disagree**. A peer
saying "I'm not touching X" while X is dirty in their worktree, or a
branch reported as ahead that is 88 commits behind, is exactly what a
conflict check is for.

Collision avoidance is the smaller half. `whoToAsk` names, per area, the
peer who has read those files most recently — the cheapest design review
available, and the one most likely to know the thing you are about to
build already exists. Message peers when stuck, when a design has two
defensible shapes, when you want someone to argue with, or when an
inherited claim is load-bearing enough to check.

Not every exchange needs an outcome. "Nothing of mine touches that —
done here, call back if it changes" is a complete reply. Ending a thread
that way is cheaper for both sides than keeping it alive hunting for a
deliverable.

Read-only; 3 agents.

## select-work

Pick ONE project off the board by value-per-risk, decide whether it
needs a decision record, and shape the PR stack.

```
Workflow({ name: 'select-work' })
Workflow({ name: 'select-work', args: { conflicts: {...}, exclude: [130, 206] } })
```

Pass `peer-canvass`'s map as `args.conflicts`, or it will happily choose
work that collides. Every candidate returns with its score and reasoning,
not just the winner, so the ranking can be argued with rather than only
the result.

Two outputs earn their place beyond the choice itself. `ciWork` says
which lint/freshness/manifest checks the change must update — and when
the answer is *none*, flags it, because a change nothing verifies needs
a human reader. `doNotTouch` names paths that would break someone else,
with the consequence: a file that is another PR's entire diff belongs
there.

The decision-record verdict is deliberately conservative. A record is a
separate first PR, docs only, green and approved before any code — but
only when there is a real decision with more than one defensible answer.
A record that restates a verified fact is a document that decides
nothing.

Read-only; 3 agents.

## pr-babysit

One turn of the babysit cycle: assess a PR (or a stack), classify every
failure as infra or real, and decide what to fix, what to wait on, and
how long to sleep.

```
Workflow({ name: 'pr-babysit', args: { prs: [209, 210] } })
Workflow({ name: 'pr-babysit', args: { prs: [209], attempt: 3 } })
```

Open PRs as **drafts** early — a review on a draft costs an edit, a
review on a finished PR costs a rework — then loop this until every PR
is DONE. DONE means three things, not one: code checks green, a verdict
actually **rendered**, and every finding fixed or explicitly declined.

The two traps it encodes are this repo's, learned the expensive way. A
green `pr-review` is not a review: the run can abort and still exit 0,
complete and post nothing, or *overwrite* a finished review — the
comment is sticky and edited in place. Require a rendered verdict in the
body, never "no unchecked boxes" (a just-started review has none
either). And a re-run hides the deciding attempt, so read
`/actions/runs/<id>/attempts/1/jobs` rather than the latest.

`attempt` drives the backoff: real failures get a short sleep because
there is work to do, infra failures back off progressively up to **8
hours**. These faults recover on their own timescale and hammering them
burns runner capacity other sessions are queued behind.

Assesses and recommends; never pushes, never merges. Anything that is a
decision rather than a task — a merge, dropping a severable PR, a
finding worth disputing — comes back in `escalate`.

Read-only; 1 agent per PR, plus 1.
