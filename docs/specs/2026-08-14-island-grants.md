# Island grants: the subtree contract, enforced host-side

Landed in [PR #250](https://github.com/WonderForgeLabs/gooey/pull/250).

Until this change, gooey's island contract was a comment.

`examples/wysiwyg/remotemode.go` states it plainly — "The editor owns
ONE named element in the target and never writes outside it" — and
`examples/wysiwyg/main.go` enforces it hard enough that `-attach`
without `-island` is a startup error. Both of those are the CLIENT
deciding to behave. On the host side there was nothing: `control.Service`
resolved every name against the whole `markup.Context`, so any attached
session could patch any element, write any property, invoke any command,
read the whole screen and swap the entire page. The only host-side
security in the control plane was `checkLoopback` in `grpc/server.go`
and `mcp/mcp.go`, plus MCP's `originAllowed` DNS-rebinding guard — all
about *who can connect*, none about *what a connected client may do*.

The plugin argument in `docs/specs/2026-08-11-component-catalog-and-wysiwyg-builder.md`
said as much in its own trade table: Temporal plugins are "sandboxed by
construction", Attach plugins "hold the control plane". This document is
the second half of that row.

## The mechanism: registration is the grant

This is not a new security model. It is the one gooey already has.

`markup.Context.Components`, `.Handlers` and `.Rules` all work the same
way, and `markup.go` says so in the `Rules` doc comment: *"Registration
is the grant, like Components and Handlers."* The host registers, and
markup reaches exactly what the host chose to hand it. A grant extends
that to the control plane:

```go
grpc.Serve(app, grpc.Options{
    Context: ctx,
    Grant:   control.Island("Guest", "Guest"),
})
```

A service built with that grant reaches the live subtree rooted at
`<... Name="Guest">` and the value namespace `Guest`, and nothing else.

Three properties fall out of putting the grant on the SERVER rather than
on the call:

- **A guest cannot name a capability it was not handed.** There is no
  request field to widen, no token to forge, no handshake to
  misnegotiate. The grant is a struct field in code the host owns.
- **The address IS the capability.** One endpoint, one grant. Two guests
  with disjoint islands are two `Serve` calls on two loopback ports.
  That composes with v1's actual posture — loopback-only, no auth —
  instead of pretending to improve on it.
- **The default is unchanged.** A nil grant is the host's own endpoint:
  the whole app, exactly as every endpoint behaved before. A host
  serving itself does not opt in to owning its own app.

### What this is not

A grant is **scoping, not authentication**. It stops an attached guest
from exceeding its brief. It does nothing about something that can reach
the host's own unscoped port. Authentication is what a non-loopback bind
would need, and v1 refuses those outright.

### Rejected alternatives

| | why not |
|---|---|
| **Named grants selected per session** (host registers several, guest picks one by name) | nothing stops guest A selecting guest B's grant, so it needs a secret, which needs auth — that is the capability handshake, explicitly deferred |
| **A wrapper component around the island** | `childSlot` (`control/markup.go`) is a CLOSED six-arm type switch; anything interposed between a container and a named element makes that element unpatchable. This already killed the per-element decorator idea |
| **Enforcement in each transport** | MCP and gRPC would drift into two ideas of what an island is. The rule lives in `control.Service`, which both already call — "one path, one model" applies to the scope as much as to the verbs |
| **Caching the island's component pointer at attach** | a swap or a hot reload reassigns every `Name=`; a cached pointer keeps pointing at a detached subtree while the guest believes it owns the visible one. The island resolves per call |

## Per-verb scoping

Two enforcement shapes, and the difference matters. **Refusing** answers
a question the guest should not have asked. **Narrowing** changes the
world the guest is shown, so it cannot refuse-probe its way to a map of
what it cannot touch. Host/guest is the framing: the host may expose or
hide, and hiding is the stronger option wherever it is available.

| verb | scoped behaviour |
|---|---|
| `PatchMarkup(name, …)` | **refuse** unless `name` resolves inside the island's live subtree (root included — that is the address the editor uses). Also refused when the island IS the composition root, because that path degrades to a whole swap |
| `SetProperty(name, …)` | **refuse** unless `name` is in the granted value list |
| `InvokeCommand(name)` | **refuse** unless granted. Commands live in the same namespace as properties and are granted identically |
| `RegisterProperties` | **refuse** unless every new name falls under a granted prefix |
| `SetFocus(name)` | **refuse** unless inside the island |
| `SendKeys` | **refuse** unless the CURRENTLY FOCUSED component is inside the island. Keys are not name-addressed; they go where focus is |
| `SendPointer` | **refuse** unless `FocusManager.MouseTarget(ev)` — where dispatch would actually route it — is inside the island |
| `SwapMarkup` | **refuse**, always. No island grant can authorize it |
| `GetDeclaredSchema("")` | **refuse** — empty source means the host's own running document |
| `SnapshotTree` | **narrow**: rooted at the island |
| `ListValues` | **narrow**: granted values, and the `Name=` table filtered to the island |
| `GetProperty(name)` | **refuse** unless granted |
| `ScreenText(styled=false)` | **narrow**: cropped to the island's arranged rect |
| `ScreenText(styled=true)` | **narrow**: the island's cells copied into a fresh `render.Buffer` of the island's size and encoded by the ordinary one-shot `Flush`, so the stream is homed at 0,0 and does not betray where on the host's page the island sits |
| `ValidateMarkup` | pruned build context (below). A denial is an **error**, not a `valid=false` answer |
| `ListStyles` | **exposed**. Styles are already a host registration, and markup cannot be authored without them |
| `FrameDelta` property changes | **narrow** — the broadcaster diffs `Service.Values()`, so scoping the service scopes the deltas with no extra code |
| `FrameDelta` damage rects | **narrow** to rects intersecting the island. Consequence: a scoped session's `Repainted` counts repaints touching its island, not the app's total |
| `LifecycleEvent.Swapped` | **narrow**: the new name table, filtered to the island |
| `InputEcho` from the terminal | **suppressed**. Terminal input is the user typing, anywhere on the page; an island grant is not a grant to watch that |
| `InputEcho` of the guest's own injections | **kept** — that is confirmation of what it sent |

### `set_value`: bound-inside-the-island is NOT the rule

The tempting rule is "a guest may write any property its island binds".
It is wrong. An island commonly READS host state — a status line bound
to `{{.App.Status}}` — and being able to display a value is not
authority to write it. Read and write are separate capabilities; only
the value list confers the write.

### The binding surface is part of the grant

This is the half that makes the rest worth anything.

Refusing `SetProperty` on `.Host.Secret` while leaving the BINDING
surface open would enforce the spelling of an escalation, not the
escalation. A guest refused the write patches
`<TextBox Text="{{.Host.Secret}}"/>` into its own island and reads it
off the screen; or patches a `Button` bound to a host command and
presses it. So a scoped `scratchBuild` builds against a **pruned
`Values` map** — the grant's names and nothing else.

Two details are load-bearing:

- **`Values` is restored on every path, success included.** Unlike
  `Named` and `Declared`, a pruned `Values` left committed would
  silently narrow the host's own context, and the failure would surface
  somewhere else entirely.
- **The denial is classified by a typed error, not by string-matching
  and not by a second build.** "No value named X" is the shape of a typo,
  so `markup.resolve` now returns a typed `markup.UnresolvedError`
  carrying the path. A failed scoped build is a denial when `errors.As`
  finds one AND that path resolves against the host's full surface — one
  map lookup. The message says so without naming the value it hid,
  otherwise a refusal becomes an enumeration of the host's state.

  The first version classified **by experiment**: rebuild the same source
  against the full surface and see whether it succeeded. Correct answers,
  and it ran the document TWICE — a `<Companion>` in a guest's fragment
  would have launched two processes on the error path alone.
  `TestARefusedFragmentRunsItsLoadTimeSideEffectsOnce` is the pin, and it
  is RED against the double build.

  Note what does *not* depend on this: the classification is about the
  MESSAGE. If `errors.As` ever misses, the caller gets
  `INVALID_ARGUMENT` instead of `PERMISSION_DENIED` — the build still
  failed and the escalation is still blocked. Enforcement never rode on
  the wording.

What is deliberately NOT pruned: `Components`, `Handlers`, `Rules`,
`Styles`, `Includes`. Those are already host registrations, and
narrowing them per guest is the capability handshake — a later question
by explicit direction.

### `PERMISSION_DENIED` is distinct from `NOT_FOUND`

A guest reaching outside its island has usually named something that
really exists, and answering "no such name" is a lie a client reasonably
retries. But a name that does not exist stays `NOT_FOUND` even for a
scoped session: collapsing every miss into a denial makes a typo
indistinguishable from a boundary. The existence check runs first.

## The property this exists for

Two guests with disjoint islands drive one app concurrently without
interfering. `TestDisjointIslandsDriveOneAppConcurrently`
(`grpc/grant_test.go`) pins it with 40 rounds per side in parallel,
asserting three things: every legal op succeeds under the other side's
load; each island's final state is its own writer's last write; and the
count of cross-island refusals is exactly `rounds*2` per side. The third
clause is what keeps it from passing vacuously — a build where
enforcement did nothing would still satisfy the first two, because both
clients are also behaving.

## The honest exception: focus is singular

There is one keyboard. Two guests cannot both hold focus, so they cannot
both type at once — and no enforcement fixes that. What enforcement buys
is that the contention becomes a **visible refusal** ("focus is on a
`*components.TextBox` outside this session's island") instead of silent
cross-talk into the host's fields. Every other verb is genuinely
concurrent.

## A scope check must model dispatch, not paraphrase it

The pointer check was first written against `HitTest` plus a separate
`Captured()` test. That is wrong in **both** directions, and neither
failure is visible without a test that dispatches the event and asks who
received it:

- **Too permissive.** `HitTest` returns the deepest component on purpose,
  but a frozen subtree does not act, so dispatch routes to the frozen
  HOST. An island underneath a frozen shell it does not own would clear
  the check and then have its event delivered outside the grant. This
  stopped being hypothetical the day `preview.Pane` became a `Frozen`
  host (`docs/specs/2026-08-14-frozen-observed.md`).
- **Too strict.** While the pointer is captured, every event routes to
  the captor regardless of where it points — that is what makes a drag
  work outside the captor's bounds. Refusing on the hit alone refuses a
  guest's own drag at exactly the moment a drag matters.

The fix is `FocusManager.MouseTarget(ev)`: the routing decision itself,
exported, so the check *derives* from dispatch instead of restating it.
It also has to model the press asymmetry — a fresh press discards an
implicit capture, only a held one survives — which `DispatchMouse` gets
for free by setting the captor before it routes and a pre-dispatch query
does not.

`components/mousetarget_test.go` never asserts `MouseTarget` against a
hand-written expectation: it dispatches, watches which component's
counter moved, and compares. That is the only form of the test worth
having, and writing it surfaced a second thing — `MouseMove` does not
route through `HandleMouse` at all, so the first probe reported
"delivered to nil" for every motion event and would have passed
vacuously against a broken `MouseTarget`.

### The grant scopes an event's TARGET, never its bubble

An event delivered inside the island still **bubbles to ancestors
outside it** when nothing in the island consumes it — `DispatchMouse`
walks `m.parent` upward, and `FocusManager.Dispatch` does the same for
keys. That is the framework's event model, and suppressing it for scoped
sessions would change how the app behaves rather than what a guest may
reach. A host component that must not see a guest's stray events has to
consume them, or not be an ancestor.

It also means a test asserting "the host component received nothing" is
wrong, and `TestGrantRefusesAPointerRetargetedOutOfTheIsland` measures a
DELTA across the refusal instead: a refused call dispatches nothing at
all.

## Residual gaps

- **A guest can still infer host geometry** from its own island's bounds
  moving when the host's layout changes.
- **No per-guest component vocabulary.** A guest may patch in any
  registered `Component`, `Handler` or `Rule`. That is the capability
  handshake, not this.
- **Nothing rate-limits a guest.** A scoped session can still make the
  app repaint as fast as the bridge allows.
