# Pipeline grammar v2 (design)

Design gate for epic #38, covering its four children: `| err` (#40),
`| progress` (#41), multiple `into` targets (#42), and a bounded
retry/timeout surface (#43). The v1 design and implementation record
live in `2026-08-10-remote-handlers-design.md`; its closing section
deferred exactly these four because they are all changes to the *same
grammar* and had to be designed once, together. This document is that
single pass. No code lands with it.

It also answers the shared-grammar question #99 raises: binding value
converters (`{{.Bytes | human}}`) must share pipe syntax with handler
pipelines so the two never diverge. The common core is specified here;
the converter stages themselves are #99's work.

## Where v1 stands

One stage. `{{ns:Func args… | into .Target}}` parses args up to the
first pipe, then stages; the only stage is `into`, taking exactly one
`.Path` that must resolve to a `*prop.Property[string]`. Failures are
delivered to the *same* target as an `"ERROR: …"` string — data and
status in one channel. Providers build their own goroutine, snapshot
argument values on the UI goroutine inside the Command, and call
`Target.Deliver`, which posts one `Set` through the Dispatcher.
Delivering to an absent target is a no-op (`wf:Signal` already uses
that: `into` is optional in the grammar; requiring it is per-function
provider validation).

Everything below preserves the load-bearing rules: no reflection
(typed handles, type-switches, the `propKinds` table), all property
writes through the Dispatcher on the UI goroutine, and every
resolvable error a load-time error naming what was wrong.

## The grammar

EBNF-ish. `lex` is unchanged: the token forms are `.Path`, `` `literal` ``,
bare word, and `|`; backtick literals stay the quoting form because XML
attributes spend both quote characters.

```
handler-expr  = "{{" prefix ":" fn { arg } { stage } "}}"
prefix        = ident-with-dashes
fn            = ident
arg           = path | literal
stage         = "|" stage-name { operand }
stage-name    = bare-word
operand       = path | literal | bare-word

; v2 stage vocabulary (each at most once, any order):
into-stage     = "|" "into" path { path }
err-stage      = "|" "err" path
progress-stage = "|" "progress" path
retry-stage    = "|" "retry" bare-int          ; 1 ≤ n ≤ provider cap
timeout-stage  = "|" "timeout" bare-duration   ; Go duration syntax, > 0
```

### Clauses, not transforms — and the #99 answer

Handler stages are **clauses on one call**, not sequential value
transforms: `retry` does not apply "after" `into`. Therefore stages may
appear **in any order**, and **each stage name at most once**.
Canonical order in documentation and examples is
`into err progress retry timeout`, but the parser does not require it —
order is style, duplication is an error.

Binding converter stages (#99) are the opposite: **ordered value
transforms**, where `{{.Bytes | human | pad 8}}` pipes left to right.
Both grammars share:

- the lexer and token forms, verbatim (`markup/expr.go`'s `lex`);
- the stage-segmentation rule: a `|` starts a stage, the first token
  after it must be a bare word (the stage name), operands run to the
  next pipe;
- one reserved-word set. The names `into`, `err`, `progress`, `retry`,
  `timeout` are **reserved in both grammars**: a converter may not
  register any of them, and a handler tail rejects any *non*-reserved
  bare stage name as unknown. That unknown-stage space is exactly where
  converter stages land later — `{{net:Get .Url | human | into .Size}}`
  is precluded by v2's stage table, not by the grammar shape, so #99
  can extend handler tails with transforms without a syntax change.

The implementation consequence: `parseHandlerExpr` splits into a
generic tail parser producing `[]stage{name, operands}` plus a
validation pass against a stage table. The binding-expression parser
(#99, future) reuses the tail parser and brings its own table.

### Stage semantics, one at a time

#### `into` — result targets (#42)

`| into .A .B …` — one or more `.Path` operands, mapped
**positionally** onto the function's declared results. Rules:

- A function declares an ordered result list (see the introspection
  contract below). `net:Get` declares `(body string, status int)`.
- **Prefix mapping**: a document may name *fewer* targets than the
  function produces — trailing results are dropped. It may never name
  *more*: that is a load error naming the provider, the function, the
  declared count, and the given count. This is what keeps every
  existing single-target document loading unchanged when `net:Get`
  grows a status result.
- **Typing** leans on the `propKinds` table. Result kinds are named in
  the markup type vocabulary (`string`, `int`, `bool`, `float`,
  `duration`, `color`). A target handle must be the property type of
  the declared kind, **or** `*prop.Property[string]`, in which case
  the framework renders the value to text (a status code into a
  string property is legal and displays "404"). Both checks are
  type-switches at load time; no reflection.
- **Atomicity**: all Sets for one completion — every `into` target and
  the `err` clear — ride in **one Dispatcher post**, so a multi-target
  result lands in one frame. Two properties that came from one fetch
  are never observably torn.
- Field extraction does **not** appear: `| into .A` names a context
  property, exactly as v1; there is no `.Result.Field` addressing in
  stage operands. Splitting a result into parts is the provider's job
  (that is what multiple declared results are). Projection syntax
  stays out (see Explicitly out).
- `into` remains optional in the grammar (fire-and-forget, as
  `wf:Signal` already allows). A function may require it at
  `NewCommand` time, as `net:Get` does today.

#### `err` — the error tail (#40)

`| err .Prop` — exactly one `.Path`, resolving to
`*prop.Property[string]`. Errors are delivered as text, not a
structured object: the terminal renders text, a typed error would need
per-provider type plumbing the no-reflection rule has no room for, and
a page that wants structure can bind visibility off "is it empty".

- **On failure**: the err target is Set to the error text. The `into`
  targets are left **unchanged** — the last good value is still valid
  data, and the common terminal pattern is stale data plus an error
  line. Zeroing would destroy information to signal a condition the
  err property already signals.
- **On success**: the err target is Set to `""`. A stale error must
  not outlive the retry that fixed it, and empty-means-healthy is what
  makes `err` usable as a visibility trigger without a converter.
- Both outcomes are **one Dispatcher post each**: failure posts
  {err←text}, success posts {into…←results, err←""}. One frame either
  way.
- **With retries** (#43): `err` fires only when the retry budget is
  exhausted — the final failure. Intermediate attempt failures are not
  delivered anywhere; they are the retry machinery's business.
- **Without an err tail**, behavior is exactly today's: the failure is
  rendered as an `"ERROR: …"` string into the `into` target (and a
  document with neither tail logs nothing and swallows the failure —
  that is v1's actual behavior for `wf:Signal` without `into`, and it
  stays). This legacy fallback is what keeps every existing document
  loading and behaving identically; the epic's "failures stop landing
  in `into` targets" is achieved by documents declaring `err`, not by
  breaking documents that don't. Whether a future strict mode makes
  `err` mandatory is left open (question 1 below).

#### `progress` — heartbeat piping (#41)

`| progress .Prop` — exactly one `.Path`. The canonical progress value
is a **float64 fraction in [0,1]**, clamped on delivery. The target
may be:

- `*prop.Property[float64]` — receives the fraction;
- `*prop.Property[int]` — receives `round(fraction*100)`, which is
  what `components.ProgressBar.Value` (0–100) binds.

Both are load-time type-switches; any other handle type is a load
error naming the two acceptable kinds. Provider-defined detail
payloads (strings like "32/97 files") are out of scope this pass.

Delivery discipline:

- On invoke, progress is Set to `0` synchronously (the Command runs on
  the UI goroutine — a fresh click restarts the bar).
- Each observation is one Dispatcher post. Sets are last-write-wins,
  so out-of-order or regressing observations (a retried attempt's
  heartbeats restart from zero) are correct by construction — noted,
  not defended against.
- On success the framework delivers a final `1.0` in the same post as
  the results, so a completed bar always reads full.
- On failure the progress property is left where it was; the err tail
  carries the outcome.
- Repaint cost is the ordinary damage guarantee: a progress Set
  repaints exactly the widgets reading that property.

**Capability is declared, not assumed.** A provider that cannot report
progress makes `| progress` a **load error**, never a silent no-op —
enforced by the framework via the introspection contract below, so no
provider can forget to reject it. `net` v2 declares no progress
capability (an HTTP GET has no useful progress signal at this layer);
`temporal:Activity` declares it.

**The Temporal mechanism** is a poll, because the SDK exposes no push
stream for standalone-activity heartbeats: the provider's job goroutine
ticks at a bounded interval (default 1s, host-tunable via a provider
`Option`), calls `ActivityHandle.Describe`, and if
`HasHeartbeatDetails()`, decodes the **first heartbeat detail** as a
number via `GetHeartbeatDetails(&f)`. Workers cooperate by calling
`activity.RecordHeartbeat(ctx, fraction)` with a numeric first detail.
A detail that does not decode as a number is ignored for progress —
that is runtime data from a remote worker, not markup, so it cannot be
a load error; the pipeline still completes normally. The poller is
internal to the provider's job and joins before the job returns.

#### `retry` and `timeout` — the bounded surface (#43)

`| retry N` — one bare-word operand, a positive integer: **at most N
additional attempts** after the first (so `retry 3` allows 4 attempts
total; the Temporal mapping is `MaximumAttempts = N+1`).
`| timeout D` — one bare-word operand in Go duration syntax
(`5s`, `1m30s`), strictly positive: a ceiling on the **total**
invocation including retries (Temporal: `ScheduleToCloseTimeout`).
Operands are literals only — these are load-time configuration, and a
bound operand would move validation to click time, against the house
rule.

**Who owns retries is a provider property, not a document choice.**
This is the double-retry hazard settled: each function declares its
retry ownership through introspection —

- **Framework-owned** (`net:Get`): the framework's invocation engine
  re-runs the provider's job up to the budget, with a per-invocation
  `context.Context` carrying the timeout. The provider contains no
  retry code at all.
- **Provider-owned** (`temporal:Activity`): the framework applies *no*
  retries and *no* local deadline race; the parsed budget rides in the
  `Call` and the provider maps it onto its native machinery
  (`RetryPolicy.MaximumAttempts`, `ScheduleToCloseTimeout`). Exactly
  one layer ever interprets the budget.

Markup needs no syntax for "defer to provider" — the same two stages
mean the same thing everywhere, and ownership is invisible to the
document. That is deliberate: an untrusted document must not get to
choose which layer retries.

**Bounds are host-side, and exceeding them is a load error, not a
clamp.** Each function declares caps (max retry count, max timeout)
through introspection; the host sets them at provider construction
(`Option`s), and registration remains the capability grant. A document
asking for `| timeout 10m` under a 60s cap fails to load naming the
cap — silent clamping would hide the divergence between what the
document asked for and what it got, and "strict and loud" wins. A
function whose caps are zero does not permit the stage at all (same
load error shape). Documents can therefore only **narrow** what the
host granted — a `retry 2` under Temporal's default unlimited-attempts
policy reduces it — never escalate. Connection configuration, task
queues, and credentials stay host-side, unchanged.

Defaults without the stages are exactly today's behavior: `net`'s
30s client timeout and no retries; Temporal's server-default retry
policy inside the provider's configured `ScheduleToCloseTimeout`.

At-least-once still applies: a Temporal activity invoked from markup
may run more than once per click regardless of `retry`, so handlers
should target idempotent activities, and `into` Sets are naturally
last-write-wins. The grammar reference ships with that paragraph.

## The provider contract, v2

v1 makes providers do four jobs: validate, snapshot, run a goroutine,
and deliver. v2 moves the goroutine and the delivery into the
framework, which is what makes every stage behave identically across
providers — the epic's "the grammar is specified once and every
provider implements the same stages" is achieved by providers *not*
implementing the stages.

### Introspection (load time)

```go
// FnInfo describes one provider function to the loader.
type FnInfo struct {
        // Results are the declared result kinds, in delivery order,
        // named in the markup type vocabulary ("string", "int", …).
        Results []string
        // Progress reports whether the function can drive | progress.
        Progress bool
        // Retries is who interprets | retry / | timeout.
        Retries RetryOwner // RetriesUnsupported | RetriesFramework | RetriesProvider
        // RetryCap / TimeoutCap bound the document surface; zero
        // forbids the stage.
        RetryCap   int
        TimeoutCap time.Duration
}

// Introspector is optional. A provider that does not implement it gets
// v1 semantics: one string result, no progress, no retry surface.
type Introspector interface {
        Describe(fn string) (FnInfo, bool)
}
```

The framework validates every stage against `FnInfo` **before**
calling `NewCommand`, so the errors are uniform across providers and a
provider cannot forget to reject a stage. The zero-value default keeps
every existing provider loading unchanged.

### Invocation (event time)

`NewCommand(c *Call) (gooey.Command, error)` keeps its signature. What
changes is how the returned Command is built: providers stop spawning
goroutines and calling `Target.Deliver`, and instead hand the
framework a **job**:

```go
// Report publishes a progress fraction; safe from any goroutine.
type Report func(fraction float64)

// Job is one attempt of the underlying work. It runs on a
// framework-owned goroutine; ctx carries the timeout budget; results
// must match the FnInfo declaration positionally.
type Job func(ctx context.Context, report Report) (results []any, err error)

// Run builds the Command for this call: per event it runs prepare on
// the UI goroutine (snapshot argument values there — that is the only
// legal place to touch properties), then drives the returned Job on
// one framework goroutine with this call's retry/timeout/delivery
// stages applied.
func (c *Call) Run(prepare func() Job) gooey.Command
```

`net:Get` becomes:

```go
src := c.Args[0]
return c.Run(func() markup.Job {
        u := src.String() // UI goroutine: snapshot
        return func(ctx context.Context, _ markup.Report) ([]any, error) {
                body, status, err := p.fetch(ctx, u)
                return []any{body, status}, err
        }
}), nil
```

The engine owns: the goroutine, the attempt loop (framework-owned
retries), the timeout context, progress reset/clamp/final-1.0, the
one-post-per-outcome delivery (into + err-clear together; err alone on
final failure), and the legacy no-`err` fallback. Provider-owned-retry
functions get the parsed budget via `Call` fields
(`Retry int`, `Timeout time.Duration`, zero = unset) and run a single
attempt. A job may use internal goroutines (Temporal's heartbeat
poller) but must join them before returning; `report` and nothing else
is safe to call from them.

`Target` and `Target.Deliver` are removed with the migration — all
three in-repo surfaces (`net:Get`, `temporal:Activity`, `wf:Signal`)
move in the PRs below. Pre-1.0, in-repo blast radius only; the break
is called out in release notes. `Call.Dispatcher` stays (WorkflowUI's
markup-swap path posts UI work that is not result delivery).

### Invariants check

- **UI confinement**: unchanged and strengthened — providers can no
  longer touch a property from the wrong goroutine because they no
  longer hold property handles for delivery at all; every write goes
  through the engine's Dispatcher posts. Argument snapshots still
  happen inside the Command on the UI goroutine.
- **No reflection**: result typing is the `propKinds` name table plus
  type-switches on `[]any` elements at delivery; target checks are
  type assertions at load.
- **Laziness/damage**: delivery is `Set` through the Dispatcher; a
  progress Set repaints exactly the readers of that property. The
  atomic multi-Set post means one frame per outcome — asserted by
  test, not implied.
- **Load-time strictness**: every rule above that can fail is a load
  error; the only runtime tolerances are remote-data ones (undecodable
  heartbeat details), which are not markup defects.
- **Capability grant**: unchanged in shape, extended in reach —
  registration grants the namespace; construction options bound what
  documents may ask of it; introspection is how the loader enforces
  both.

## Parse and load errors, enumerated

Tail-level (shared with #99's future binding tails):

1. trailing `|` with no stage;
2. stage position holds a non-bare token ("expected a pipeline stage
   after |, got path .X");
3. unknown stage name — message lists the v2 vocabulary and notes
   non-reserved names are reserved for converter stages;
4. duplicate stage ("more than one `| err` stage").

`into`: no operands; a non-path operand; more targets than declared
results (names provider, fn, counts); target handle not the declared
kind and not `*prop.Property[string]` (names both types, propKinds
style); unresolvable path (existing resolve error).

`err`: operand count ≠ 1; operand not a path; target not
`*prop.Property[string]`; unresolvable path.

`progress`: operand count ≠ 1; operand not a path; function does not
declare progress capability (names provider and fn); target not
`*prop.Property[float64]` or `*prop.Property[int]`; unresolvable path.

`retry`: operand count ≠ 1; operand not a bare integer; n < 1;
function declares no retry surface; n exceeds the host cap (names the
cap).

`timeout`: operand count ≠ 1; operand not a valid Go duration; d ≤ 0;
function declares no timeout surface; d exceeds the host cap (names
the cap).

Plus the v1 errors, unchanged: undeclared prefix, unregistered URI,
missing Dispatcher, bad args, provider `NewCommand` failures.

## Worked examples

```xml
<!-- err tail: stale body survives a failure; Err empties on success -->
<Button Content="fetch"
        Click="{{net:Get .Url | into .Body | err .Err}}"/>

<!-- multiple targets: body and status land in one frame -->
<Button Content="fetch"
        Click="{{net:Get .Url | into .Body .Status | err .Err}}"/>

<!-- single target under the two-result declaration: status dropped,
     exactly v1 behavior -->
<Button Content="fetch" Click="{{net:Get .Url | into .Body}}"/>

<!-- progress: a worker's heartbeats drive a ProgressBar (int 0-100) -->
<Button Content="rebuild"
        Click="{{temporal:Activity `RebuildIndex` .Query
                 | into .Results | err .Err | progress .Pct}}"/>
<ProgressBar Value="{{.Pct}}"/>

<!-- bounded retry/timeout; temporal owns the interpretation -->
<Button Content="sync"
        Click="{{temporal:Activity `Sync` .Src
                 | into .Report | err .Err | retry 3 | timeout 2m}}"/>

<!-- framework-owned retries on net; stage order is free -->
<Button Content="poll"
        Click="{{net:Get .Health | retry 2 | timeout 5s
                 | err .Err | into .HealthBody}}"/>

<!-- fire-and-forget with only an error channel -->
<Button Content="notify" Click="{{wf:Signal `approve` | err .Err}}"/>
```

And the load-error side, for the reference doc: `| into .A .B .C` on
two-result `net:Get`; `| progress .Pct` on `net:Get`; `| timeout 10m`
under a 60s cap; `| retry 0`; `| err .A .B`; `| frobnicate .X`.

## Wire/schema note

Nothing new serializes. Handler expressions ride inside the markup
document, so the served-markup path (`SwapMarkup`, workflow-served UI)
carries v2 pipelines with zero contract change, and the values-map
contract is untouched. What v2 *adds* is introspectability: the
`FnInfo` table means a loaded document's handler surface — which
namespaces, functions, result kinds, and budgets it needs — is
computable at load time. That is the natural input for a future
control-plane preflight ("will this document load against this host's
grants?") and for `gooey gen`'s typed surfaces, in the same way the
`<x:Property>` declarations block is already noted as a per-control
schema. Noted, not built, matching that spec's precedent.

## Implementation plan

PR-sized, in dependency order; each stays green at `go test ./...`
(plus `cd handlers/temporal && go test ./...` — nested module).

1. **#40 — engine + err tail.** Generalize `parseHandlerExpr` into
   the shared tail parser and stage table (order-free, duplicate
   rejection). Introduce the invocation engine (`Call.Run`, `Job`,
   one-post delivery, legacy fallback) with single string results.
   Migrate all three provider surfaces off `Target`; remove `Target`.
   Tests: `TestErrTailRoutesFailure`,
   `TestIntoUnchangedOnFailure`, `TestSuccessClearsErrTail`,
   `TestNoErrTailKeepsLegacyErrorString`,
   `TestOutcomeDeliversInOneFrame` (damage-count),
   `TestDuplicateStageIsLoadError`, `TestStageOrderIsFree`; the
   existing `net` httptest coverage extended, not replaced.
2. **#41 — introspection + progress.** `FnInfo`/`Introspector`,
   framework-side stage validation before `NewCommand`, `Report`
   plumbing, the Temporal Describe-poller, progress typing
   (float64/int). Tests: `TestProgressOnIncapableProviderIsLoadError`,
   `TestProgressTargetKindIsChecked`,
   `TestProgressResetsOnInvokeAndCompletesAtFull`,
   `TestProgressRepaintsOnlyReaders` (damage-count),
   `TestHeartbeatPollerDeliversFractions` (fake `activityStarter`
   seam).
3. **#42 — multiple results.** `[]any` results, `FnInfo.Results`
   kinds, prefix mapping, typed targets with the string-render
   fallback, `net:Get` grows `(body, status)`. Tests:
   `TestIntoArityIsLoadError` (names provider and counts),
   `TestTwoTargetsLandInOneFrame` (damage-count),
   `TestTypedStatusTarget`, `TestStringTargetRendersAnyKind`,
   `TestSingleTargetPrefixKeepsV1Behavior`.
4. **#43 — retry/timeout.** Stage parsing (bare-int, bare-duration),
   caps in `FnInfo` + host `Option`s, the framework attempt loop for
   `net`, the Temporal `RetryPolicy`/`ScheduleToCloseTimeout` mapping.
   Tests: `TestRetryExhaustionDeliversErrOnce`,
   `TestTimeoutOverCapIsLoadError`, `TestRetryZeroIsParseError`,
   `TestFrameworkRetriesNetWithinBudget` (httptest flaky-then-ok),
   `TestTemporalOwnsRetries` (assert one `ExecuteActivity` call
   carrying `MaximumAttempts`, no framework re-invocation),
   `TestNoStagesKeepsProviderDefaults`.
5. **Epic close-out.** `docs/learn/examples/` app driving a
   ProgressBar from Temporal heartbeats with a visible err tail,
   recorded as a GIF; pipeline grammar reference updated to v2; the
   remote-handlers spec's "Still open" section closed out; README
   status row.

## Explicitly out

- **Converter stages in handler tails** (`| human` before `into`) —
  the grammar reserves the space; #99 designs the stages.
- **Binding-side converters themselves** — #99, sharing the tail
  parser and the reserved-word set defined here.
- **Field projection / `.Result.Field` addressing** in stage operands
  — multiple declared results are the sanctioned decomposition.
- **Structured error objects** — `err` is text; typed error surfaces
  would fight the no-reflection rule for little terminal payoff.
- **Progress detail payloads** (strings, counts) — fraction only this
  pass.
- **Bound (non-literal) retry/timeout operands** — load-time config
  stays literal.
- **Cancellation surface** (`| cancel`, cancel-on-reinvoke policy) —
  the engine's per-invocation context makes it reachable later;
  nothing in v2 precludes it.
- **Dedup/conflict-policy surface in markup** (Temporal ID reuse) —
  stays host-side entirely.
- **Custom provider-defined stages** — the stage table is the
  framework's; providers extend via functions, not grammar.
- **Wire serialization of pipelines/FnInfo** — noted above as future
  control-plane preflight input, not built.

## Open questions (for Elan)

1. **Sunset for the legacy fallback?** With no `| err`, failures still
   land in `into` as `"ERROR: …"` (back-compat, per #40's acceptance).
   Keep indefinitely, or make a future strict mode require an err tail
   wherever `into` is declared?
2. **Reserved-word head-room**: reserve only the five v2 names, or
   also pre-reserve likely future clause names (`when`, `cancel`,
   `debounce`) so converters can never claim them?
3. **Provider-contract break**: removing `Target`/`Deliver` outright
   (pre-1.0, all known providers in-repo) vs. keeping a deprecated
   shim for one release — any out-of-tree providers to worry about?
