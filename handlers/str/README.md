# str — a value pack

Pure string functions, in the binding position:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:str="gooey.dev/handlers/str">
  <Text>{{str:Upper .User}}</Text>
  <Text>{{str:Pad .Name `12`}}{{str:Truncate .Note `40`}}</Text>
</Gooey>
```

```go
markup.RegisterValues(strhandlers.URI, strhandlers.New())
```

Value registry only. There is no `RegisterHandlers` for this pack and
no writable variant: nothing here has an effect, so there is nothing to
grant beyond the pack's existence. Writing `{{str:Upper .X}}` on a
`Click` is a load error.

**Root module, on purpose** — the graph is `fmt`, `strconv` and
`strings`.

## Functions

Exported as constants, enumerated by `strhandlers.AllNames()`. The
error text for an unknown function derives from the same list, so the
inventory and the dispatch cannot drift (there is a test for that).

| function | shape | does |
|---|---|---|
| `str:Upper` | `{{str:Upper .S}}` | |
| `str:Lower` | `{{str:Lower .S}}` | |
| `str:Trim` | `{{str:Trim .S}}` | leading/trailing whitespace |
| `str:Replace` | ``{{str:Replace .S `old` `new`}}`` | all occurrences |
| `str:Join` | ``{{str:Join `, ` .A .B .C}}`` | separator first, then one or more values |
| `str:Default` | ``{{str:Default .S `(none)`}}`` | the fallback when the value is empty |
| `str:Pad` | ``{{str:Pad .S `12`}}`` | right-pads to a width in **runes**; never truncates |
| `str:Truncate` | ``{{str:Truncate .S `40`}}`` | cuts to a width in runes, spending the last one on `…` |

Widths are **backtick literals**, never bound paths: a width is
layout configuration, and the house rule (pipeline grammar v2, "Bound
operands") is that configuration resolves when the page loads. A
non-integer width, a width below 1, and every arity mistake are load
errors naming the function.

Rune counting rather than byte counting is deliberate — the terminal
counts cells, and `{{str:Pad `héllo` `7`}}` has to pad by two.

## Every function is a computed

Each function returns `prop.NewComputed` over its argument handles, so
the argument `Get`s run *inside* an evaluation, which is what makes
them subscriptions rather than reads. `{{str:Upper .User}}` repaints
exactly the components that display it, only when `.User` changes.
Nothing in this package tracks anything; the property graph does it
all.

The corollary is the trap the whole repo shares: an argument read
behind a branch drops out of the dependency set on the frames where the
branch does not run. Every function here reads all of its arguments
unconditionally *before* deciding anything, and `str:Default` is where
that is load-bearing rather than incidental — `TestDefaultStaysSubscribedToBothArguments`
is the pin.

## What is missing, and why

**Nesting.** ``{{str:Upper env:Get `USER`}}`` does not parse. The
expression grammar (`markup/expr.go`) is deliberately flat — no
nesting, no arithmetic — and this pack occupies the call form that
already exists rather than inventing syntax. The visible cost is the
overlap between `str:Default` here and `env:Get`'s optional fallback
argument: two ways to say the same thing, because neither can be
composed with the other.

**Pipe transforms.** ``{{.Bytes | human | pad 8}}`` is reserved for
binding converters under issue #99, specified in
`docs/specs/2026-08-10-pipeline-grammar-v2.md`. This pack does not
touch that space. When #99 lands, `str`'s functions are the obvious
first converter stages, and composition arrives with it.
