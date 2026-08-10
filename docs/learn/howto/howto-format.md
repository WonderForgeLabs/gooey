# How to format values for display

You have a byte count, a duration, a timestamp, a fraction — and a
`Text` that wants a string. The `format` package turns one into the
other as a **computed property**: each constructor takes your typed
source handle and returns a `*prop.Property[string]` that the graph
keeps current.

```go
import "github.com/WonderForgeLabs/gooey/format"

size    := prop.NewSource(int64(0))
elapsed := prop.NewSource(time.Duration(0))
frac    := prop.NewSource(0.0)

&components.Text{Content: format.Bytes(size)}          // "1.5 KiB"
&components.Text{Content: format.DurationShort(elapsed)} // "1m30s"
&components.Text{Content: format.Percent(frac, 1)}       // "42.3%"
```

There is no wiring beyond that. `size.Set(n)` dirties the formatted
string, and the next frame repaints exactly the `Text` reading it —
the ordinary damage guarantee, nothing formatter-specific.

## The formatters

| Constructor | Input | Output |
|---|---|---|
| `format.Bytes(p)` | integer property | IEC sizes: "512 B", "1.5 KiB", "79 MiB" |
| `format.BytesSI(p)` | integer property | SI sizes: "1.5 kB", "83 MB" |
| `format.Count(p)` | integer property | compact counts: "999", "12.3k", "1.2M" |
| `format.Percent(p, digits)` | float64 **fraction** | "42.3%" — same 0..1 convention as `\| progress` |
| `format.DurationShort(p)` | duration property | "320ms", "45s", "1m30s", "1h32m", "3d4h" |
| `format.RelTime(p, clock)` | time property | "just now", "3 minutes ago", "in 2 hours" |

Each has a plain-function twin (`format.FormatBytes(int64) string`,
…) for use outside the graph — same implementation, so a log line and
a label can never disagree.

## Relative time needs a clock

"3 minutes ago" drifts: the source never changes, but the right string
does. A computed only re-evaluates when a dependency changes, so
`RelTime` takes the clock **as a property** and you tick it:

```go
now := prop.NewSource(time.Now())
app.Every(30*time.Second, func() { now.Set(time.Now()) })

label := format.RelTime(created, now)
```

One `now` source serves every `RelTime` in the app. Pick the interval
to match the coarsest granularity you show — each tick repaints the
labels reading the clock, even when the text comes out the same,
because `Set` never compares.

Passing `nil` for the clock is allowed: the string is then computed
against `time.Now()` only when the *source* changes — fine for a
"last updated" label beside data that updates anyway, wrong for a
static timestamp.

## When there is no ready-made formatter

Two escape hatches, in order of reach:

```go
format.Sprintf("0x%02x", code)            // one fmt verb is the whole need
format.Func(state, func(s State) string { // arbitrary one-value rendering
    return s.Label()
})
```

For a string over *several* properties, skip the package and write the
computed directly — that is all these constructors are:

```go
memLabel := prop.NewComputed(func() string {
    return fmt.Sprintf("%d / %d MB", used.Get(), total.Get())
})
```

Markup-side formatting (`{{.Size | bytes}}`) is planned as converter
stages over these same functions — issue #99; until then, formatting
lives in the viewmodel as above.
