# gooey

A XAML-like TUI framework for Go: retained visual tree, lazy
dependency-property graph, XML markup with Go-template bindings,
damage-tracked rendering.

`README.md` is the tour and `docs/architecture.md` is the grounded
walkthrough; `docs/specs/` holds the decision records, `docs/learn/` the
tutorials and how-tos. This file carries only what those do not: the
rules whose violation is *silent*, and the commands that catch it.

## One workspace, one vendor directory

`go.work` lists **every** module in the tree and `vendor/` at the repo root
holds their dependencies **once**, merged. Both are committed, and together
they mean the whole tree builds with the network off and with an empty
module cache — which is the property to re-check, because it is the one that
silently stops being true.

Two consequences that are easy to trip over:

- **`go.work` changes resolution, not just downloads.** Every nested module
  now resolves its siblings to the checkout beside it rather than to a
  published version. That is what makes a core API change break
  `apps/gitui` in your working tree instead of at the next tag — a feature
  here, but it means `go test` in a nested module is no longer testing that
  module against what it *requires*. That require is read for the first time
  OUTSIDE this repo, by somebody's `go get`, so
  `TestNestedModulesRequireAResolvableCoreVersion` reads it in here instead: a
  nested module must require core at a published commit, never the `v0.0.0`
  placeholder, which no proxy can serve. The `replace … => ../..` beside it
  does not save you — a replace in a *dependency's* go.mod is ignored by
  whoever depends on it, and applies only here, where the workspace has
  already made it redundant.
- **`GOPROXY=off` does not prove vendoring works.** It blocks downloads
  while the local module cache still satisfies everything, so a tree that
  would fail on a clean machine passes for you. The discriminating checks
  are where a package actually resolves, and a build against a cache that
  is not there:

```sh
go list -f '{{.Dir}}' google.golang.org/grpc   # must print a path under vendor/
GOPROXY=off GOMODCACHE=$(mktemp -d) go build ./...
```

`go work vendor` regenerates the directory after any dependency change; it
strips the vendored modules' own `go.mod` files, which is why the module
discovery below still finds exactly the tree's own modules and needs no
`vendor` prune.

**That pairing is now CHECKED rather than remembered.**
`.github/workflows/vendor-freshness.yml` runs `go work vendor` on every PR
touching a `go.mod`, `go.sum`, `go.work`, `go.work.sum` or `vendor/`, and on
every push to main, and fails if running it changed anything — which is the
invariant itself rather than a proxy for it. Until it existed the rule was
convention, and Dependabot broke it three times in one morning
([#449](https://github.com/WonderForgeLabs/gooey/issues/449)–[#451](https://github.com/WonderForgeLabs/gooey/issues/451)):
it edits a require and never vendors, so `go` refused with `inconsistent
vendoring` in whichever job ran a Go command first — for all three, a
*protobuf* step. `vendor-autofix.yml` repairs Dependabot's PRs in place.
`TestVendorWorkflowsCoverTheRootModule` pins the one blind spot those
filters can silently acquire: `**/go.mod` does not match the ROOT `go.mod`,
whose stale `vendor/` breaks every other module's build.

**`tool` directives go in `tools/`, never in a module somebody imports.** A
`tool` directive records the tool's whole dependency graph as `// indirect`
requires of the go.mod holding it, and MVS hands those to every consumer of
that module — buf in the root go.mod obliged anyone importing gooey to buf,
Docker's CLI, quic-go, cel-go and ~90 more, and forced upgrades of whatever
they shared with them. `tools/` is in `go.work`, so it still vendors into the
one root `vendor/` and `go tool` still resolves offline; it just has no
importable packages. `TestToolDirectivesStayOffTheImportableModules` keeps it
that way, and `tools/doc.go` carries the reasoning.

**How many modules are vendored is deliberately not written here** — derive
it, for the reason the Verify section gives about counts in prose. And
`vendor/modules.txt` will mislead you if you count it the obvious way:
`grep -c '^# '` counts **stanzas**, not modules, and a workspace inflates
that twice over. Each of the tree's own modules gets a self-referencing
`=> ./path` stanza, and a module required at two versions gets one stanza
each. Counting stanzas overstated the real figure by more than half in this
change's own PR description, and a reviewer caught it:

```sh
# third-party modules actually vendored
grep '^# ' vendor/modules.txt | awk '{print $2}' |
  grep -v '^github.com/WonderForgeLabs/gooey' | sort -u | wc -l
```

## Verify

```sh
go vet ./...     # whole-repo compile check — use this, not `go build`
go test ./...
```

`./...` stops at the module boundary, and every nested module it skips has
to be run on its own. **Discover them — never enumerate them.** A written
list of module names is stale the first time someone adds one, and the
failure is silent: the loop still exits 0, so you report a verified tree
having never compiled the new module. That is exactly how this file came to
name eight modules across two loops and still skip seven `packs/temporal-*`.
`ci.yml` ran the same risk from the other end and lost twice — `paint/` and
every `apps/*` module went unbuilt behind a wall of green — so it now
builds its job matrix from **this exact command** ([PR #261](https://github.com/WonderForgeLabs/gooey/pull/261)),
and `TestCIWorkflowAndCLAUDEMDShareOneDiscovery` fails if the two ever
differ by a character.

```sh
# Every nested module — the same discovery ci.yml uses to build its
# matrix. CI packs the result into one leg PER TIER rather than one per
# module; the discovery below is the half the two files share, and
# TestCIWorkflowAndCLAUDEMDShareOneDiscovery pins that, not the packing.
#
# The `case` below IS the -race tier, character for character the one in
# ci.yml, and TestCIWorkflowRaceTierMatchesCLAUDEMD fails if they drift —
# so read the arm, not a sentence restating it. The rule it encodes: a
# module races when its tests exist to prove nothing touches the property
# graph off the UI goroutine, i.e. when it serves somebody else's
# goroutine. Everything else gets the plain run.
#
# The one place this loop is deliberately WIDER than CI: CI vets
# `apps/*` without running their suites, and this runs them. So a
# green CI does not mean an example's own tests passed; a green loop
# here does.
#
# Pruning dot-directories at EVERY depth is load-bearing, and filtering
# with `-not -path './.*'` is not enough — that only anchors at the top.
# The two offenders are both UNTRACKED, so neither exists in a fresh
# clone and you cannot check this from the repo alone: .claude/worktrees/
# holds whole checkouts, and apps/kanban/worker/.venv (gitignored;
# appears once you run that example) vendors two go.mod files of
# Temporal's own. That asymmetry is the point — a top-anchored filter
# passes in CI and walks into someone else's tree on your machine.
#
# `-mindepth 1` is the load-bearing half of that: it is what lets a
# depth-1 dot-directory be pruned at all, AND it keeps `-name` off the
# starting point. Drop it and `-name '.*'` matches `.` itself, prunes the
# whole walk, and prints nothing while still exiting 0. `.?*` is only
# belt-and-braces for whoever removes `-mindepth 1` later — with it
# present the two patterns find the same set.
#
# Piped into `while read` rather than `for m in $(…)`: unquoted command
# substitution word-splits AND glob-expands, so one directory with a
# space or a `*` in its name would quietly mis-split the list.
#
# Failures go to a FILE, not to `echo FAIL`. The pipe puts the loop body
# in a subshell, so a counter set in there dies with it — which is how
# the previous version of this loop came to print `FAIL handlers/exec`
# somewhere in ten thousand lines of go output and still exit 0. Reading
# the last line is the check; the loop cannot make it for you.
#
# `mktemp`, NOT a fixed name like /tmp/gooey-verify-fails, and this is
# not fussiness — do not simplify it back. This repo routinely has five
# to fifteen agents live at once in their own worktrees, all following
# THIS file, and /tmp is shared by every one of them. On a fixed name
# two loops interleave: one truncates while another appends, so you
# report `FAILED: handlers/exec` for a module you built cleanly, or you
# read the file in the window after someone else's truncate and print
# "all nested modules green" over a real failure. A check that can
# quietly report the wrong answer is the defect this whole section
# exists to remove, so it may not be reintroduced by the loop itself.
#
# `< <(find …)` would let a counter survive and drop the file entirely,
# but it is a bashism; this block stays POSIX (`sh -n` clean) because
# people paste it into whatever /bin/sh they have.
fails=$(mktemp "${TMPDIR:-/tmp}/gooey-verify-fails.XXXXXX")
find . -mindepth 1 -name '.?*' -prune -o -name go.mod -print | sort |
while IFS= read -r mod; do
  m=${mod%/go.mod}; m=${m#./}
  [ "$m" = "." ] && continue   # the root module — covered by the block above
  case "$m" in
    handlers/*|packs/*|mcp|grpc) race=-race ;;
    *)                           race= ;;
  esac
  ( cd "$m" && go vet ./... && go test $race ./... ) || echo "$m" >> "$fails"
done
if [ -s "$fails" ]; then
  echo "FAILED: $(tr '\n' ' ' < "$fails")"   # kept at $fails for inspection
else
  echo "all nested modules green"; rm -f "$fails"
fi
```

**How many modules that is, is deliberately not written here.** A number in
prose is a sample taken once: this sentence used to say 15 (`490cbfe`) and
was still saying it three modules later, until a stack-landing commit
happened to notice (`1c119ff`). That is an enumerated list wearing a
smaller hat. `TestCLAUDEMDVerifyLoopReachesEveryNestedModule`
([PR #210](https://github.com/WonderForgeLabs/gooey/pull/210)) runs the
command above and compares it against a walk of the tree, and
`TestCIWorkflowDiscoversEveryNestedModule` does the same for `ci.yml`, so
the count is derived on every run of the root suite. Want the number? Run
the `find` — and if it walks noticeably fewer than the tree looks like it
holds, suspect the loop before you trust the green.

The `-race` where CI applies it is not a nicety: what those tests prove is
that no RPC, tool body, activity goroutine, or child-process callback
touches the property graph off the UI goroutine, and without the detector
that assertion is only half made. `.github/workflows/ci.yml` is the
authority on what CI runs.

One gap CI leaves you to cover by hand, and one it no longer does. CI now
discovers every module, so a core API change that breaks `apps/gitui` or
any other consumer is caught by name — in the failing leg's `::error::`
annotations and step summary, which list every module that failed, not in
the check name. Legs are packed one per TIER (`vet`, `test`, `race`), so
the check that goes red is the tier's — `race (N modules)` — and the
module is named inside it, not by it. (The N is rendered from discovery
at run time; it is deliberately not written down here, for the reason
the Verify section gives about counts in prose.) Examples are **vetted, not tested** there, so their own suites
(`apps/wysiwyg` has a dozen) run only in the loop above. The root
module's tests still do not run under `-race` in CI.

CI schedules on the org's self-hosted vSphere pools, not hosted runners —
which is why the packing matters. Those pools are shared with production
workloads and do not autoscale, so a leg per module meant 25 pods per
push. `.github/workflows/ci.yml` carries the per-job tier reasoning. The secondary
guard is narrower than it reads, too: `TestCLAUDEMDNamesNoDeletedModule`
checks only namespaces in its own `moduleNamespaces` list, which predates
the `apps/` move and does not include it — so an app module named here and
then renamed leaves this paragraph pointing at nothing, and nothing goes
red (tracked in [#316](https://github.com/WonderForgeLabs/gooey/issues/316)).

## Invariants

Each of these is checkable. Say so explicitly in your report when a change
touches one.

**No reflection in core.** Bindings resolve to typed `*prop.Property[T]`
handles at build time (lvalue semantics, not value), through registries and
type switches. The only `"reflect"` imports in the repo are generated
protobuf under `grpc/gen/` and one `*_test.go` per activity pack — eight
today, one under each `packs/temporal-*`, none of them core. Check with
`git grep -l '"reflect"'` rather than trusting this sentence's arithmetic.
This is what keeps a future `gooey gen` able to compile markup ahead of
time — the [`gooey gen` epic (#59)](https://github.com/WonderForgeLabs/gooey/issues/59)
is what that future is — so "just use reflection here" is a design change,
not a shortcut.

**The `Get` call site decides subscribe-vs-read.** `prop.node.recordRead`
(`prop/prop.go:33`) records an edge only when a computed is on `evalStack`.
Inside an evaluating node — a paint node's `Render`, a validator, a style
computed — `Get` subscribes. Anywhere else — `Measure`/`Arrange`, an event
handler, a Composer sweep — the identical call is a plain read. Layout runs
deliberately outside any evaluation context (`composer.go:651`, in
`Composer.Frame`), which is why `MeasureChild` can sync `Layout.Visibility`
from a bound source without creating a dependency; the Composer arms a
separate observer for that (`Composer.armVisibility`, `composer.go:418`).

**Every component's `Render` is its own paint node.** `Composer.build`
(`composer.go:349`) wraps each `Render` in a `prop.NewComputed`
(`composer.go:380`), so reading a property while painting *is* the damage
declaration — there is no `AffectsRender` and no `InvalidateVisual`. A
change repaints exactly the components that read it.

**Containers paint only their own chrome, through `MeasureChild` /
`ArrangeChild`.** The interface is `Container { ChildComponents() []Component }`
(`component.go:38`) — the framework walks children, never the container.
Parents never call `child.Measure`/`child.Arrange`; `MeasureChild`
(`layout.go:283`) and `ArrangeChild` (`layout.go:340`) apply the
margin/size/align/visibility sandwich, and skipping them silently drops all
four. A component calling `Base.Arrange(b)` on *itself* is fine and common.
A cycle no longer kills the process, and the fix is bigger than the issue
that asked for it. [#216](https://github.com/WonderForgeLabs/gooey/issues/216)
asked for a depth cap on `MeasureChild`; capping that alone would have left
the very crash it was filed for, because `Composer.build` runs BEFORE layout
exists and dies on the **heap**, with no fatal error and no trace. Seven
walks over `ChildComponents()` recurse in this package and all seven are
bounded now — Compose and Focus by identity (they already key a map by
component), Measure/Arrange/HitTest/Focusable/Render by depth against
`MaxLayoutDepth` (512, which is 73x the deepest tree this repo has ever laid
out). A control that includes itself is a **load** error naming the loop.
Nothing panics: read the report with `Composer.LayoutFault()` /
`App.LayoutFault()`.

Four walks OUTSIDE this package are still unbounded — `components/adorn.go`,
`components/buttonbar.go`, `control/markup.go`, `control/snapshot.go`. They
are safe only because a cyclic tree can no longer *become* a live
composition, and would still hang on one handed to them directly. That these
were eleven sites of one missing idea — the framework has no single
"walk the children" seam — is
[#375](https://github.com/WonderForgeLabs/gooey/issues/375); the decision
record is `docs/specs/2026-08-23-layout-cycle-bounds.md`.

Pre-clearing is the subtle half, and it is no longer a two-case rule
(`composer.go:383-420`; the design record is the container-backgrounds and
z-order epic [#26](https://github.com/WonderForgeLabs/gooey/issues/26),
landed in [PR #88](https://github.com/WonderForgeLabs/gooey/pull/88)):

- leaves pre-clear their bounds — to **the nearest ancestor's background**
  (`clearStyle`), not the terminal default, so a Text in a colored panel
  does not punch a hole;
- a **chrome-only container pre-clears nothing**, because its bounds
  enclose children whose own clean nodes will not repaint;
- a **hidden** container and a container with a declared `HasBackground`
  handle *do* fill their bounds, and are marked `covered` — which makes the
  z-ordered pass force their subtree to repaint above them in the same
  frame.

**Z-order is document order in TWO layers, and "declare it last" was never
the second one.** `Composer.orderPaint` walks the tree in depth-first
pre-order and lifts every `gooey.Overlay` subtree to the end; `c.nodes`
stays the structure, `c.paint` is the answer to what is in front of what.
The layer exists because forcing runs FORWARD ONLY — the paint loop can
force a node later than a painter beneath it and has no way to reach one
earlier — so "the surface is the last of its owner's children" bought
being above the owner's *other* children and nothing else. Anything
declared after the OWNER painted over an open popup and stayed there
([#430](https://github.com/WonderForgeLabs/gooey/issues/430): a `MenuBar`
on the designer canvas beside a `Gauge`, an `ItemsView` and a `Border` —
the dropdown painted on the frame that opened it and the next repaint
erased it). The lift is global rather than within the overlay's parent,
because an owner three containers deep still drops its menu over a dock
that is a sibling of its great-grandparent. `Overlay` is a MARKER with an
empty method, not a predicate: a paint node's position is structural, and
a bool would need the observer-and-re-sync machinery `Frozen` has. The
four unbounded `ChildComponents` walks outside this package
([#375](https://github.com/WonderForgeLabs/gooey/issues/375)) do not know
about layers and never needed to — none of them paints.

**Markup is two tiers behind one `fs.FS` seam.** `Include` = markup-only
control, no code-behind; without `<x:Property>` declarations its attributes
*become* the child context, with them they are type-checked against the
declared surface. `UserControl` = code-behind setup that **extends** a
declared surface; a setup defining a name the markup already declared is a
load error, because declarations own the public surface — the contract the
`x:Property` epic [#7](https://github.com/WonderForgeLabs/gooey/issues/7)
specified and [PR #84](https://github.com/WonderForgeLabs/gooey/pull/84)
landed. Everything resolvable — arity, argument types, unknown functions,
undeclared xmlns prefixes — must fail at **load time**, never as a surprise
on click. The `fs.FS` seam is what makes `os.DirFS` + watcher (dev) and
`embed.FS` (release) the same code path.

**One ordered `input.Event` stream.** Keys and SGR mouse reports arrive
interleaved on one wire and stay on one channel (`input/mouse.go:52`),
because two channels could reorder them. `FocusManager.Dispatch`
(`input.go:701`) routes a key in phases, and it **tunnels before it
bubbles**: every `PreviewKeyHandler` from the root *down* to the focused
component is offered the event first, and the first that takes it ends
dispatch. Then the bubble, focused → ancestors, **three steps per level**
— the node's scoped `KeyBinding`s, then its attachment handlers
(`attachedKey`, type-ahead and friends), then its own `HandleKey`. That
middle step's position is load-bearing and silently breakable: swapping it
past `HandleKey` still compiles and still passes most tests, and only
`TestAttachmentKeysPrecedeHost` notices. After the bubble the mnemonics get
the leftovers, in tree order; only then do tab/shift+tab and an unclaimed
arrow fall through to focus navigation (`FocusDir`, `input.go:813`).
`DispatchMouse` (`mouse.go:169`) bubbles the same way from the
captor-or-hit component. KeyBindings are scoped by their host component, so
one only fires while the focused chain passes through it. Focus and hover
are ordinary source properties (`FocusState`, `input.go:155`; `HoverState`,
`mouse.go:42`) — that is the whole reason moving focus repaints exactly two
components. Capture, `CanExecute` and the tunneling phase all came from
input round 2 ([#31](https://github.com/WonderForgeLabs/gooey/issues/31),
landed in [PR #86](https://github.com/WonderForgeLabs/gooey/pull/86); the
tunnel is [#34](https://github.com/WonderForgeLabs/gooey/issues/34)).

**UI-goroutine confinement, via the Dispatcher.** Properties are unlocked
by design, so nothing off the main loop may `Get` or `Set`. Async work
posts a closure (`Dispatcher.Post`, safe anywhere) and the loop runs it
(`Drain`, UI goroutine only) — see `App.Run`'s select at `app.go:477`. A
`Startable` gets `post` as the *only* legal route to the graph, and nothing
in the framework will catch a violation.

**No `Screen` teardown may leave a goroutine reading the terminal.**
`Screen.Restore` (`term/term.go:238`) restores modes, closes the tty, then
**joins** the decoder while draining its channel, bounded by
`term.DecoderTimeout`, with `Screen.DecoderLeaked` as the tripwire.

**Heavy dependencies live in nested modules.** The rule is about what an
SDK drags in, not about the count: a dependency that pulls a client library,
a protocol stack or a transitive graph belongs in a nested module, and the
default answer to "this needs an SDK" is still a new nested module — see
`docs/specs/2026-08-10-pack-distribution.md`, and the doctrine's
issue/net-audit trail at [#160](https://github.com/WonderForgeLabs/gooey/issues/160)
/ [PR #162](https://github.com/WonderForgeLabs/gooey/pull/162).

What the rule is *not* is a ban on adding anything. This section used to
read "exactly two direct requirements … adding a third is a doctrine
change", and that framing made a small library sound like the same decision
as vendoring a cloud SDK. It is not, and treating it as one bought a
hand-rolled line-scanner where a parser belonged (`lfscheckout_test.go`,
which now uses `gopkg.in/yaml.v3` to read the workflows).

Judge a dependency by **what it compiles into**, not by the `require`
count — those are different questions and the module graph will not
distinguish them for you. `go mod graph` shows yaml.v3 pulling
`gopkg.in/check.v1`, which looks like a transitive dependency and is not
one: it is yaml's own *test* dep, so it is in the graph and in nothing that
builds. The check that answers the real question is:

```sh
go list -deps ./...     # what the non-test build actually compiles
```

A test-only dependency appears in neither that list nor a consumer's binary.

Read the current set with `go list -m all` rather than trusting a number
written here; a count in prose is a sample taken once, and this file has
already been wrong about one.

## Traps

**`go build ./...` writes 25 executables into the repo root**, one per main
package under `cmd/` and `docs/learn/examples/`, named after its directory —
and each nested module does the same in its own root, for 38 in all.
`.gitignore` now covers every one, but only because the block is
**generated**: the hand-maintained version it replaced listed 18 entries
covering 16 of the 38, two of them naming repo-root paths no build has ever
produced. Regenerate it (the command is in a comment above the block) after
adding, renaming or removing any main package; a substring edit cannot
maintain it. Use `go vet ./...` for a whole-repo check, or
`go build -o /tmp/gooey-bin/ ./...` when you actually want binaries; `-o`
accepts a directory for a multi-package build and leaves the tree clean.

**A `cmd/<name>` that matches a sibling package directory builds nothing,
silently.** `cmd/settingsdemo` could not become `cmd/settings` because
`settings/` exists: `go build ./cmd/settings` errors with `build output
"settings" already exists and is a directory`, `go build ./...` **exits 0
and writes no binary at all**, and `go build -o settings ./cmd/settings`
treats the name as an output *directory* and drops the executable inside the
source package. That is why the binary is `cmd/prefs` while the package it
exercises is `settings` — see
`docs/specs/2026-08-15-apps-rename.md`, which also carries the old→new
path map for every pre-2026-08-15 spec. The move itself is the
repo-restructure epic
([#238](https://github.com/WonderForgeLabs/gooey/issues/238)), whose `apps/`
relocation and demo-suffix scrub landed in
[PR #268](https://github.com/WonderForgeLabs/gooey/pull/268).

**`prop.Set` does not compare values** (`prop/prop.go:101`). Setting a
property to what it already holds still invalidates every dependent and
still costs a repaint. Guard at the call site if you need idempotence.

**Dependencies are recorded by the `Get` that actually runs.** A `Get`
behind an early `return`, or on the short-circuit side of `&&`/`||`,
drops out of the dependency set on the frames where it does not execute,
and the component goes deaf to that property — no error, no panic, just a
stale cell. Hoist `Get`s above early returns and OR the results afterward.
Damage-count tests catch this; nothing else does.

**Every width is a COLUMN count, and `len([]rune(s))` is not one.** A
`Measure`'s W, a popup's self-sizing width, a menu title's span, a paint
cursor's advance — all cells. A CJK character or an emoji is one rune and
TWO cells, so a rune count asks layout for a box narrower than its own
text. Clipping ([#357](https://github.com/WonderForgeLabs/gooey/issues/357),
landed in [PR #409](https://github.com/WonderForgeLabs/gooey/pull/409);
`Composer.build` brackets every `Render` with `Cells.Clip(bounds)`) does
not save you here and reading it as a safety net is the trap: it stops
the overflow reaching a NEIGHBOUR's cells, which is a different problem.
The glyph is still lost, silently, inside your own rect — and the
sentence this paragraph replaced said "nothing clips at the frame level",
which stopped being true on 2026-08-29 and would send a reader looking
for the missing characters somewhere on screen. Measure with `render.StringWidth`, clip with
`render.ClipCols` (which stops BEFORE a glyph that would overrun, so it
may return a column short — half a glyph is not drawable), and write
through `Buffer.SetString`, which lays the `render.Continuation` marker in
the second column. `Buffer.Set` in a per-rune loop does not, and a cursor
advanced one-per-rune writes the next glyph into a cell the previous one
already covers.

This is the invariant whose violation is loudest in consequence and
quietest in the suite: ~35 sites counted runes across
[#358](https://github.com/WonderForgeLabs/gooey/issues/358) with
everything green, because every fixture in the repo was ASCII, *and*
because six packages' `row(b, y)` test helpers rendered the continuation
marker as a literal rune — so no fixture could hold a wide glyph and be
asserted on. Read a row back with `render.RowText`. To pin one of these,
use two strings of the same COLUMN width and different rune counts
(`"世界"` against `"abcd"`) and assert they measure alike; an ASCII
fixture agrees with itself under either rule and passes against the bug.

**A damage-count assertion is the only pin for a repaint claim.**
`Composer.Frame()` returns `(*Frame, int)` where the int is how many
components repainted; `App.PaintedLastFrame()` is the same number on the
runtime, and `Composer.Damage()` the rects. A bounds assertion or a "the
cell says X" assertion passes just as well when the entire tree repainted,
so it proves nothing about damage. Dozens of test files already assert exact
counts — `components/input_test.go`, `components/mouse_test.go`,
`components/background_test.go`, `components/tabs_test.go`,
`components/itemsview_test.go`, `components/validation_test.go`,
`./dynamic_test.go`, `format/graph_test.go`, `grpc/e2e_linux_test.go` and
more. How many is a grep for `PaintedLastFrame()` and for the second return
of `Composer.Frame()` away — not a number worth writing here, for the
reason the Verify section gives. They are contract tests, not a convenience
— [#30](https://github.com/WonderForgeLabs/gooey/issues/30) is where that
became the rule: if your change moves a number, that *is* the change, and it
needs justifying rather than updating.

**Event fields are `gooey.Action`, an interface** (`input.go:36`), not
`func()`. A bare `func(){}` literal does not assign — wrap it in
`gooey.Command(...)` or `gooey.NewCommand(...).When(canProp)`. Never test
one with `!= nil`; use `gooey.CanExecute(a)` (`input.go:49`), which is
nil-tolerant and also consults `CanExecute()`. A disabled binding keeps
bubbling rather than being consumed (`input.go:714` — the `CanExecute`
conjunct simply falls through to the next binding, then the next
ancestor).

**A `Startable`'s stop func must close AND join.** `close(done)` alone lets
a tick that already won its select post *after* `Close`, and lifetime tests
flake. Joining is what makes stop a barrier: Close ⇒ no further posts, ever.
The idiom now lives in `startable.go`, not hand-rolled per component:
`gooey.Every` (`startable.go:42`) owns it for periodic ticks — Timer,
Spinner, and ProgressBar all delegate to it (`components/timer.go:55`,
`spinner.go:113`, `progressbar.go:96`) rather than writing their own
`done`/`stopped` channels. `gooey.Delays` (`startable.go:80`) owns the same
contract for a group of one-shot delays that stop together — Tooltip and
ToastHost embed it (`components/tooltip.go:65`, `toast.go:47`) for
per-hover shows and per-toast dismissals, where the count in flight is
unbounded and a single ticker doesn't fit. It was written out by hand in
seven controls until
[PR #281](https://github.com/WonderForgeLabs/gooey/pull/281) collapsed
them, and `App.Every` shipped the signal-no-join defect in the runtime
itself until [PR #282](https://github.com/WonderForgeLabs/gooey/pull/282)
delegated it too (`app.go:376`). A `Startable` that still hand-rolls its
own `done`/`stopped` pair is a claim that neither shape fits —
`Companion.Start` (`components/companion.go:133`) is the one legitimate
case, joining a subprocess `Wait()` rather than a ticker.

**Never call `Fd()` on the tty.** `os.File.Fd()` puts the file in blocking
mode and removes it from Go's netpoller; after that a pending `Read` is an
uninterruptible syscall that `Close` cannot cancel, and `SetReadDeadline`
fails with `ErrNoDeadline`. `Screen.control` (`term/term.go:96`) routes
every ioctl through `SyscallConn().Control` for exactly this reason. The
bug record, including the A/B evidence and the ordering subtlety that made
it look intermittent, is `docs/specs/2026-08-10-tty-read-lifecycle.md`.
`term`'s pty-backed lifecycle tests only compile on Linux.

**Headless verification is already written down.** Driving an app under
`script -qec` with `stty`, octal `printf` escapes (`sh` has no `\x`),
replaying the log through `render.Screen` instead of hunting for the last
`\x1b[H`, and the asciinema/`agg` recipe are all in
`docs/learn/howto/howto-testing.md`. Two consequences worth internalizing:
mouse input cannot be injected through a recording pty, so every feature
must stay keyboard-operable; and `agg` renders the cell plane only, so
captures need halfblock, never sixel or kitty.

## A red suite is yours

**There is no known-bad list here, and adding a static one is not allowed.**
Main is expected green. If a test fails, it is your problem until an **open
issue says otherwise** — and you establish that by reading the issue's
state, not by reading a sentence in this file.

This rule replaces a "Known-bad on main" section that named issues #182 and
#183 as pre-existing failures. Both were already fixed *in this file's own
ancestry* when it was written (`ba6a7ff`, `b5869ae` both precede `13ebe2b`),
so for its entire life the file told every reader to wave through a `-race`
failure in `handlers/temporal` and a SIGWINCH timing failure in the root
module — the two places a concurrency regression is most likely to land. It
also claimed CI runs `handlers/temporal` without `-race`; that had stopped
being true in `b5869ae`, the very fix it was citing. A stale dismissal is
worse than no list, because it spends the attention that would have caught
the bug (issue #207).

So the mechanism for any future entry has to be **derived or expiring, never
hand-maintained**:

- cite an issue number and require the reader to check that it is still
  **open** — a closed issue means the entry is dead and the failure is real;
- never assert "this failure is expected" in prose that outlives the fix;
- prefer `t.Skip` with the issue number in the skip message, so the claim
  lives next to the test and dies with it in the same commit.

If you believe a failure is pre-existing, confirm it against `origin/main`
in a clean checkout and report *that*, rather than inheriting the belief.
