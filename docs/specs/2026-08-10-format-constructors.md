# Format helpers as computed-property constructors (issue #159)

**Status:** executed.

**Date:** 2026-08-10

## What was asked for

Elan's framing, closing the stdlib expansion (4/4): "format helpers —
shouldn't we have a concept for doing this kind of stuff? how does xaml
do? we should be wrapping humanize or whatever conversions/sizes/
relative times as computed-property constructors."

So: a concept, not a grab-bag. The concept is that **a formatter is an
ordinary computed property built at wiring time** — `format.Bytes(size)`
returns a `*prop.Property[string]`, and from there the graph does
everything: laziness, dependency recording, per-reader damage. No new
mechanism enters the framework; the package is pure computation over
handles the framework already has.

## How XAML does it, and why gooey does it differently

XAML has two answers, both attached to the *binding*:

- **`IValueConverter`** — an interface (`Convert`/`ConvertBack`)
  instantiated as a resource and named on the binding
  (`Converter={StaticResource BytesConverter}`). Dispatch is by
  interface at binding-evaluation time; the converter is invisible to
  the dependency system except as a pass-through.
- **`Binding.StringFormat`** — a .NET format string applied after the
  binding resolves (`StringFormat={}{0:N1} KB`).

gooey's near-term equivalent is neither an interface nor a binding
attribute — it is a **constructor**: viewmodel code calls
`format.Bytes(p)` and hands the resulting string property to a `Text`
like any other. The differences are deliberate:

- **No interface, no dispatch.** An `IValueConverter` exists because
  XAML must invoke arbitrary conversions from markup through a
  type-erased binding engine. gooey's Go-side wiring has no such gap to
  bridge: a conversion is a function, a reactive conversion is
  `prop.NewComputed` over that function, and the no-reflection rule
  never comes under pressure.
- **The graph is the change notification.** WPF re-runs a converter
  because binding machinery pushes a new value through it. Here the
  computed re-evaluates because its source went dirty — same lazy
  semantics as every other property, damage counted the same way.
- **`ConvertBack` does not exist.** These are display formatters;
  two-way editing is component code (TextBox), per the existing
  bindings doctrine in the README.
- **`StringFormat`'s role** is covered by `format.Sprintf(verb, p)`,
  the escape hatch when a fmt verb is the whole requirement.

The markup-side answer — a converter *stage* in binding expressions —
is #99, below.

## One implementation, two surfaces (the #99 mapping)

Every formatter exists twice, by design:

| Plain function (this PR)          | Constructor (this PR)      | #99 stage (future) |
|-----------------------------------|----------------------------|--------------------|
| `FormatBytes(int64) string`       | `Bytes[T Integer](p)`      | `{{.Size \| bytes}}`   |
| `FormatBytesSI(int64) string`     | `BytesSI[T Integer](p)`    | `{{.Size \| bytes-si}}` |
| `FormatCount(int64) string`       | `Count[T Integer](p)`      | `{{.Stars \| count}}`  |
| `FormatPercent(float64, int)`     | `Percent(p, digits)`       | `{{.Frac \| percent 1}}` |
| `FormatDurationShort(Duration)`   | `DurationShort(p)`         | `{{.Elapsed \| duration}}` |
| `FormatRelTime(t, now)`           | `RelTime(p, clock)`        | `{{.Created \| reltime}}` |

The plain functions are the shared kernel. The pipeline-grammar-v2
record ([2026-08-10-pipeline-grammar-v2.md](2026-08-10-pipeline-grammar-v2.md))
already reserved the space: binding converter stages are *ordered value
transforms* sharing the tail parser and the reserved-word set with
handler clauses, and stage names outside the reserved five are exactly
where converters land. When #99 builds its registry, each registered
stage name resolves to one of these functions — the stage does
`FormatBytes` on the read path with dependency recording flowing
through the converted read, and Go-side and markup-side formatting can
never drift because there is one implementation to drift from.

Two conventions are fixed here so both surfaces inherit them:

- **Percent takes a fraction**, `0.423 → "42.3%"`, matching the
  pipeline grammar's canonical progress value (float64 in [0,1]): one
  source can feed a `ProgressBar` and a `Percent` label and mean the
  same thing to both.
- **Bytes defaults to IEC** ("1.5 KiB"). A TUI lives beside `free -h`,
  `df -h`, and htop, which are binary; SI is the named variant, not
  the default.

## Distribution decision: in-repo, root module, zero dependencies

Per the pack-distribution doctrine record
([2026-08-10-pack-distribution.md](2026-08-10-pack-distribution.md)):
`format` is a **component/format library, not a pack** — pure
computation over the property graph, importing the package is the whole
story, and there is no capability to grant, so there is no registration
gate. That absence is the classifying test; none was invented.

Root-module placement binds it to the root dependency budget
(`golang.org/x/*` only). `dustin/go-humanize` is tiny and pure but
third-party, and a format package earns a place in every viewmodel only
if importing it costs nothing — so the humanize *semantics* are
implemented in-repo (each formatter is a few dozen lines of stdlib) and
the API compatibility is recorded instead of the dependency:
`FormatBytes` matches `humanize.IBytes` output, `FormatBytesSI` matches
`humanize.Bytes` (both extended to negative counts, which humanize's
uint64 surface cannot express). If the outputs ever need to track
humanize exactly, the table tests are the contract to update.

## The RelTime drift problem, solved honestly

Relative time is the one formatter whose output changes while its
inputs hold still: a pure computed over `created` alone would render
"3 minutes ago" once and freeze, because nothing in a lazy graph
re-evaluates for wall-clock passage. Three designs were on the table:

- **A goroutine inside the constructor** — rejected. Ticking is
  Startable discipline: lifetime belongs to the composition
  (`components.Timer`) or the app (`app.Every`), never to a value
  constructor that has no Close and no owner.
- **Framework-magic re-evaluation** — rejected; there is no such
  mechanism and inventing one for a formatter would breach the lazy
  graph's core rule (only Set invalidates).
- **The clock as a property** — chosen. `RelTime(t, clock)` takes the
  "now" as a second handle; the app ticks it:

  ```go
  now := prop.NewSource(time.Now())
  app.Every(30*time.Second, func() { now.Set(time.Now()) })
  label := format.RelTime(created, now)
  ```

  or declares a `<Timer>` whose Tick sets the source — same pattern,
  composition-owned lifetime, hot-reload safe. One clock property
  serves every RelTime in the app.

`clock` may be nil, with documented semantics: the computed falls back
to `time.Now()` *at evaluation time*, so the string is exact whenever
the source changes and static in between — right for a "last updated"
label beside live data, wrong for a static timestamp.

Damage note, stated rather than hidden: `prop.Set` never compares, so
every clock tick repaints every RelTime reader even when the text is
unchanged ("3 minutes ago" holds for many ticks). Pick the tick
interval to match the coarsest granularity displayed; the per-tick cost
is the ordinary damage guarantee (exactly the readers), asserted by
`TestRelTimeClockTickRepaintsReader`.

## Executed

### API (`format/`, root module, imports `prop` + stdlib only)

```go
// Plain functions — the #99-shared kernel. Pure, no clock reads.
func FormatBytes(n int64) string                    // IEC: "1.5 KiB" (≙ humanize.IBytes)
func FormatBytesSI(n int64) string                  // SI: "1.5 kB"  (≙ humanize.Bytes)
func FormatCount(n int64) string                    // "999", "12.3k", "1.2M", "3.4B"
func FormatPercent(f float64, digits int) string    // FRACTION in: 0.423 → "42.3%"
func FormatDurationShort(d time.Duration) string    // "320ms", "1.5s", "1m30s", "1h32m", "3d4h"
func FormatRelTime(t, now time.Time) string         // "just now", "42 seconds ago", "in 2 hours"

// Constructors — prop.NewComputed over the functions above.
type Integer interface { ~int | ~int8 | ~int16 | ~int32 | ~int64 }

func Bytes[T Integer](p *prop.Property[T]) *prop.Property[string]
func BytesSI[T Integer](p *prop.Property[T]) *prop.Property[string]
func Count[T Integer](p *prop.Property[T]) *prop.Property[string]
func Percent(p *prop.Property[float64], digits int) *prop.Property[string]
func DurationShort(p *prop.Property[time.Duration]) *prop.Property[string]
func RelTime(t, clock *prop.Property[time.Time]) *prop.Property[string] // clock nil = eval-time now

// Escape hatches.
func Sprintf[T any](verb string, p *prop.Property[T]) *prop.Property[string]
func Func[T any](p *prop.Property[T], f func(T) string) *prop.Property[string]
```

Formatting rules fixed by the table tests: byte values under 10 of a
unit get one decimal, otherwise none (humanize's rule); counts carry
one decimal with ".0" trimmed and promote on rounding (999950 is "1M",
never "1000.0k"), clamping at T; durations show at most two units and
drop a zero minor unit ("3m", not "3m0s"); negative values format as
sign plus magnitude everywhere, including the `math.MinInt64`
negation-overflow edges.

### Tests

`format/format_test.go` — table tests per formatter: zeros, negatives,
`MaxInt64`/`MinInt64`, IEC/SI unit walks, the humanize doc examples
(82854982 → "79 MiB" / "83 MB"), count rounding-promotion boundaries
(999949/999950), sub-second durations, the singular/plural and
future/past ladders.

`format/graph_test.go` (package `format_test`, so `components` is a
test-only import) — the graph contract:

- `TestBytesSetRepaintsOnlyItsReader` — damage-count style: Set on the
  source paints exactly 1 component; clean frame paints 0.
- `TestUnreadFormatterNeverEvaluates` — laziness: Evals()==0 until the
  first Get.
- `TestRelTimeClockPairing` — clock advance alone re-derives; source
  stays live.
- `TestRelTimeClockTickRepaintsReader` — one tick, one repaint.
- `TestRelTimeNilClockFreezesUntilSourceChanges` — the documented
  nil-clock limit, asserted.
- `TestSprintfAndFunc` — the escape hatches track their sources.

### Docs

- `docs/learn/howto/howto-format.md` — the how-to (constructor usage,
  the RelTime clock pattern, the escape hatches), indexed from
  `docs/learn/index.md`.
- README bindings row: points Go-side formatting at `format/` and
  markup converters at #99.

### Demo touch: considered, declined

`cmd/sysmon` was the natural customer, but its two hand-rolled strings
(`"%d / %d MB"`, the fixed-width proc rows) are multi-value layouts no
single-property formatter reproduces identically — converting would
change rendered output, and the demo's frames are contract surface for
its ItemsView migration. The how-to carries the runnable usage instead.
First natural in-repo customer: the Temporal ops dashboard (#142 phase
2) formats activity latencies and payload sizes.

## Explicitly out

- **The #99 converter stages and registry** — the kernel functions are
  ready; the stage grammar, registry, and reserved-word enforcement are
  #99's pass, on the tail parser pipeline-grammar-v2 specifies.
- **ConvertBack / two-way formatting** — display only; editing is
  component code.
- **Locale-aware output** — English unit names and "ago"/"in" are
  fixed; localization is #102's string-table territory and these
  functions would become its lookup keys, not its mechanism.
- **Multi-property formatters** (`"%d / %d MB"`) — compose with an
  ordinary `prop.NewComputed` reading both sources; a `Func2` waits for
  a second real customer.
- **A `humanize` dependency** — recorded above; semantics in-repo.

## Invariants touched

No reflection (generic constraints and typed handles only). Lazy graph
(constructors are plain `NewComputed`; laziness asserted by test).
Damage (Set repaints exactly the readers; damage-count tests in the
package). UI confinement (the package never starts a goroutine; the
RelTime clock pattern routes ticking through Timer/app.Every, which
already own it). Startable discipline (explicitly kept out of the
constructor). Root dependency budget (stdlib + `prop` only, per the
pack-distribution record).
