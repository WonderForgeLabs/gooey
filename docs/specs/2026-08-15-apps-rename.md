# apps/, and the demo-suffix scrub

The showcase programs moved from `examples/` to `apps/`, and the `demo`
suffix came off the command directories. This record exists for two
reasons: to say why, and to be **the one lookup** that resolves an old
path in any older document.

Tracked as epic [#238](https://github.com/WonderForgeLabs/gooey/issues/238),
which stays open — two of its three questions are still unanswered (see
[Deliberately not done](#deliberately-not-done)).

## Why the old names were wrong

`examples/kanbandemo` is a Kanban board that is also an MCP server, with a
live traffic log and a Python Temporal worker as a companion. `examples/gitui`
is a git client. `examples/wysiwyg` is a markup editor that builds its own
UI over the control plane. Calling them examples — and suffixing them
`demo` — advertised them as toys, which is a claim about the framework as
much as about them: a framework with only demos is a framework nobody has
built anything in.

`cmd/` already means "runnable program" in Go. `cmd/colordemo` therefore
said "demo" twice and named its subject once.

## The decision

**1. `apps/` for the top-level set.** On 2026-08-12 the word was canvassed
across the active sessions; three of four peers replied and all three
independently said `apps/`. The alternative on the table was `usecases/`,
which would have made the "built by use cases" methodology legible in the
tree. It was rejected on 2026-08-15: the methodology claim belongs in one
README sentence, not in a directory name every import path has to carry.

**2. Under `cmd/`, the suffix is DROPPED, not replaced.** `cmd/colordemo`
→ `cmd/colors`, never `cmd/colorapp`. Respelling would keep the defect —
the directory would still be named after its status rather than its
subject — and would cost the same churn to do it.

Two names could not be reached by dropping alone, and both were decided
by naming the program for what it does:

- `cmd/demo` has an **empty stem**. Its own first doc line reads "the
  pixel-plane demo", so it is `cmd/pixels`.
- `cmd/settingsdemo` → `cmd/settings` is **build-breaking** — see
  [Traps](#traps) — so it is `cmd/prefs`. The synonym split is
  deliberate: the binary is `prefs`, the package it exercises is
  `settings/`. Do not "fix" it back.

The same rule reached two nested-module commands carrying the identical
defect (`mcp/cmd/mcpdemo`, `paint/cmd/paintdemo`). Nothing outside their
own module imports a main package, so this cost nothing beyond the move.

**3. `docs/learn/examples/` is untouched.** It is the one place the word
"examples" is honest — small finished programs, one per tutorial — and its
paths are woven into tutorial prose that reads as instructions.

**4. `handlers/temporal/workers/temporalworker` is untouched.** "Worker"
is not a status suffix; it is an accurate name for a Temporal worker.

**5. Existing specs are NOT rewritten.** Nineteen records under
`docs/specs/` name the old paths. Each is dated, and a spec dated
2026-08-10 describing `examples/kanbandemo` is telling the truth about the
tree on that date. Rewriting it would make the record claim a path that
did not exist when the decision was made — the same class of error as
doctrine that has gone stale while still presenting itself as current
(the failure `CLAUDE.md`'s "A red suite is yours" section was written
about). The mapping table below is the fix instead: one lookup resolves
any old path in any older document, and the older documents stay honest.

## Mapping

Every rename in one place. Where a `.gooey` page, a GIF or an on-disk
artifact was named after its directory, it moved with it.

### Top-level applications

| Old | New | Notes |
|---|---|---|
| `examples/dynamic-activities` | `apps/dynamic-activities` | module path `…/gooey/apps/dynamic-activities` |
| `examples/gitui` | `apps/gitui` | module path `…/gooey/apps/gitui` |
| `examples/kanbandemo` | `apps/kanban` | module path `…/gooey/apps/kanban` |
| `examples/temporal-worker` | `apps/temporal-worker` | Python; not a Go module |
| `examples/wysiwyg` | `apps/wysiwyg` | module path `…/gooey/apps/wysiwyg` |

The `replace` directives in those four `go.mod` files are unchanged: the
directory depth is the same, so `../../` still resolves.

### Commands in the root module

| Old | New | Notes |
|---|---|---|
| `cmd/cardsdemo` | `cmd/cards` | |
| `cmd/colordemo` | `cmd/colors` | `colordemo.gooey` → `colors.gooey` |
| `cmd/demo` | `cmd/pixels` | empty stem; named for its subject |
| `cmd/propdemo` | `cmd/props` | `propdemo.gooey` → `props.gooey` |
| `cmd/settingsdemo` | `cmd/prefs` | `cmd/settings` collides with `settings/` — see Traps. `settingsdemo.gooey` → `prefs.gooey`; the demo's own settings file `settingsdemo.json` → `prefs.json` |
| `cmd/statedemo` | `cmd/state` | `statedemo.gooey` → `state.gooey` |
| `cmd/toolkitdemo` | `cmd/toolkit` | |
| `cmd/typeaheaddemo` | `cmd/typeahead` | `typeaheaddemo.gooey` → `typeahead.gooey` |

Unchanged under `cmd/`, because they never carried the suffix:
`browser`, `finder`, `logview`, `markuplog`, `probe`, `reader`, `sysmon`.

### Commands in nested modules

| Old | New | Notes |
|---|---|---|
| `mcp/cmd/mcpdemo` | `mcp/cmd/server` | `mcpdemo.gooey` → `server.gooey`. `mcp/cmd/mcp` was not available — it would collide with the module directory the same way `cmd/settings` does |
| `paint/cmd/paintdemo` | `paint/cmd/plates` | "plate" is the word the demo's own pages already use. `paintdemo.gooey` → `plates.gooey` |

### Recorded GIFs under `docs/media/demos/`

| Old | New |
|---|---|
| `cardsdemo.gif` | `cards.gif` |
| `colordemo.gif` | `colors.gif` |
| `demo.gif` | `pixels.gif` |
| `mcpdemo.gif` | `server.gif` |
| `paintdemo.gif` | `plates.gif` |
| `propdemo.gif` | `props.gif` |
| `statedemo.gif` | `state.gif` |
| `toolkitdemo.gif` | `toolkit.gif` |

### Runtime identifiers that carried a renamed name

Not paths, but they read as one after the rename, and every producer and
consumer of each is in this repo:

| Old | New | Where |
|---|---|---|
| task queue `kanbandemo-dynamic-ui` | `kanban-dynamic-ui` | `apps/kanban` `-worker-task-queue` default, and the `TEMPORAL_TASK_QUEUE=` line in the docs that drive it |
| MCP/gRPC server name `gooey-kanbandemo` | `gooey-kanban` | `apps/kanban` |
| MCP server name `gooey-mcpdemo` | `gooey-mcp-server` | `mcp/cmd/server` |
| companion log `kanbandemo-worker.log` | `kanban-worker.log` | `apps/kanban` |
| `cmd/browser` recording prefix `example-` | `app-` | `roots` in `cmd/browser/main.go`; recordings live in the gitignored `recordings/` |

## Traps

### A `cmd/<name>` matching a sibling package directory produces no binary

This is why `cmd/settingsdemo` did not become `cmd/settings`. The repo has
a `settings/` package at the root, and `go build ./cmd/settings` would
write its executable to `./settings`. Three distinct failures, and the
middle one is silent:

```
$ go build ./cmd/foo                 # a foo/ package directory exists
go: build output "foo" already exists and is a directory
$ echo $?
1

$ go build ./...                     # the SAME collision, multi-package
$ echo $?
0                                    # ...and no binary was written

$ go build -o foo ./cmd/foo          # -o accepts a DIRECTORY
$ ls foo/
foo.go  foo                          # the executable lands INSIDE the source package
```

Reproduced in a throwaway module of four files — `go.mod`, `foo/foo.go`
(`package foo`), `cmd/foo/main.go` (`package main`) — which is enough to
show all three. Removing `foo/` and re-running `go build ./...` writes
`./foo` as expected, which is what proves the exit-0 case was the
collision and not a build that never had output.

`go build ./...` is what CI and the Verify loop run. A rename into this
shape therefore ships a command that **cannot be built and reports
success**, and the third form quietly drops a multi-megabyte binary into a
source directory where `git add -A` will find it.

Check any new `cmd/<name>` against `ls` of the module root before
committing to the name.

### `.gitignore`'s binary list must be generated

`go build ./...` writes one executable per main package into the current
directory — so a main package's binary lands at the root of **its own
module**, not at the repo root. The list was hand-maintained and had
drifted badly: **18 entries covering 16 of the repo's 38 main packages**,
with two of those entries (`/mcpdemo`, `/temporaldemo`) naming repo-root
paths that no build has ever produced. All ten `docs/learn/examples/*`
binaries and `toolkitdemo` were unprotected. Restricted to the root module
alone the ratio was 14 of 25 — and #238 itself carried a third pair of
numbers ("15 entries against 24 packages"), which is the point: **three
stale counts about one file.** Derive both sides from `go list`.

It is now **38 of 38**, generated, with the generating command in a
comment above the block. A substring edit cannot maintain it — seven of
the original sixteen root-level entries contained no "demo" at all.

### A demo GIF's filename must equal its DIRECTORY name

`gifFor` (`cmd/browser/gifplay.go:505`) tries three locations in order:
`recordings/<prefix><name>.gif` in the launch tree, then
`docs/media/demos/<name>.gif` in the source tree, then `<name>.gif` at
the source root. The prefix applies only to the first — the checked-in
lookup is keyed on the **bare directory name**. And its `ok` return is
discarded at the call site (`main.go:223`), so a miss leaves an empty
path and the info pane renders nothing: no error, no log line.

That is why `mcp/cmd/mcpdemo`'s GIF became `server.gif` and not the more
descriptive `mcp-server.gif` — the latter would have been invisible to the
launcher forever. Markdown image links in `README.md` and `docs/` fail
just as quietly on GitHub. Any rename touching a demo name has to move
its GIF in the same commit, to a filename equal to the new directory
name; the table above is complete for that reason.

### `cmd/browser` self-lists directory names

`roots` in `cmd/browser/main.go` names `cmd`, `handlers/temporal/cmd`,
`mcp/cmd`, `docs/learn/examples` and `apps` as literal paths, deliberately
rather than by discovery, because the ORDER is the presentation. A
top-level directory rename that misses this entry removes a whole group
from the launcher with no failure anywhere. `watch.go` iterates the same
list, so its fingerprinting follows automatically.

## Deliberately not done

- **`shared/grpc` and `shared/mcp`.** Epic question 2 (is the import-path
  churn worth it now, or should it wait for a natural module-path break?)
  is unanswered. It is the same class of churn as this rename and was
  originally sequenced into the same window; it is not blocked by
  anything here.
- **Deleting `apps/../cmd/typeaheaddemo`.** Epic question 3 asks whether
  the type-ahead probe should be deleted rather than renamed, since it has
  already produced its findings. Unanswered, so it was renamed
  (`cmd/typeahead`) and kept. Deleting it later costs one commit.
- **`grpc/cmd/grpcdemo` and `handlers/temporal/cmd/temporaldemo`.** Both
  carry the suffix. They were left because each sits beside sibling
  commands (`temporalops`, `wizardui`) whose naming wants deciding as a
  set, not one at a time.

## Verification

`go vet ./...` and `go test ./...` green on the root module, plus
`go test -race ./...` on the root module (a gap CI leaves). Every nested
module vetted and tested through `CLAUDE.md`'s discovery loop: **19
`go.mod` found, 18 nested modules, all passing** — the same count as
before the rename, which is what shows no module was orphaned by a move.
All 25 root main packages built to a scratch directory, plus the four
`apps/` modules (two of which CI does not build at all) and both renamed
nested commands.

`TestCIWorkflowDiscoversEveryNestedModule` and
`TestCLAUDEMDVerifyLoopReachesEveryNestedModule` are the mechanical pins
on that count, and both are in the root suite.
