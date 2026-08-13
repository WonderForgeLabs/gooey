# Value namespaces: the pull half of the mechanism (executed)

Status: implemented 2026-08-12. Owner: `markup/values.go`,
`markup/scan.go`, `handlers/env`, `handlers/str` — all root module.
Builds on the pack distribution doctrine
(`2026-08-10-pack-distribution.md`) and the handler-namespace record
(`2026-08-10-remote-handlers-design.md`); reserves nothing that
pipeline grammar v2 (`2026-08-10-pipeline-grammar-v2.md`) claimed.

The prompting question was Elan's, and it is worth keeping verbatim
because the answer turned out to be "yes, and that is the problem":

> How do you invoke a function outside a handler? Does it just become
> sending a command?

## What was measured first

Before designing anything, six documents were loaded against a live
provider to find out what the framework actually did with a namespace
call in a value position. The result is the reason this record exists.

| document | before this change |
|---|---|
| `<Text>{{t:Get \`HOME\`}}</Text>` | **loads clean**, zero provider calls, paints the literal text `{{t:Get \`HOME\`}}` |
| `<Button Content="{{t:Get \`HOME\`}}"/>` | same |
| `<Text>home is {{t:Get \`HOME\`}}</Text>` | same |
| `<Text>{{zz:Get \`HOME\`}}</Text>` (undeclared prefix) | same — the capability check never runs |
| `<TextBox Text="{{t:Get \`HOME\`}}"/>` | load error: "is not a binding expression" |
| `<Text>{{.Nope}}</Text>` | load error: `"Nope" not found in context` |

So the answer to the question was: **yes, a function is only ever a
command** — and reaching for it any other way was not refused, it was
*swallowed*. `bindText` matched `{{.Path}}` with a regexp
(`markup.go:880`) and left every non-matching `{{…}}` as literal text.
An undeclared namespace prefix, an unregistered URI, a typo'd function
name and a deliberate decision all produced the same thing: the source
text, on the terminal, at run time.

That is a direct violation of the house rule — *everything resolvable
must fail at load time, never as a surprise later* — and it is the
third member of the silent-drop family, after unknown attributes (now
rejected) and leaf elements discarding children (still open).

The asymmetry is what made it invisible: attributes that *require* a
binding (`TextBox.Text`, `Visibility`, `Style`) go through
`BindingValue` and were always strict. Attributes that accept
*literal-or-binding* — element content, `Content`, `Title`, `Label`,
`Prompt`, eight call sites, all of them the ones a page author
actually writes in — funnel through `bindText` and were not.

## The decision

Two things, and they are not separable.

**1. `bindText` is strict.** A `{{…}}` in interpolated content is a
binding, a value-namespace call, or a load error. `markup/scan.go`
replaces the regexp with a scanner that respects backtick literals (so
``{{str:Replace .S `}}` `--`}}`` finds the *last* `}}`), and every
brace expression it cannot classify names what it could not resolve.

**2. Namespaces get a pull side.** A value expression resolves at build
time to a `*prop.Property[string]` and composes with literals and paths
in the same interpolation:

```xml
<Gooey xmlns:env="gooey.dev/handlers/env"
       xmlns:str="gooey.dev/handlers/str">
  <Text>hi {{str:Upper .User}}, on {{env:Get `TERM` `(unknown)`}}</Text>
```

There is **no new syntax**. The expression grammar (`markup/expr.go`)
is reused verbatim, including its backtick literals and its `.Path`
arguments; what changed is that a second *position* now accepts it.

### Push and pull are a property of the capability, not a feature gap

This is the conceptual finding, and it is what makes the split
principled rather than a convenience.

An **effect** — fetch a URL, run a workflow, spawn a process, play a
sound — is an event. It happens at a moment, it can fail, it wants a
target to deliver into. `| into .Body` is the right shape and
`Click="{{net:Get .Url | into .Body}}"` is the right position.

A **value** — the environment, the clock, an uppercased name — is not
an event. It has no moment and nothing to deliver into; it *is* the
binding. Forcing it through the push form means declaring a property,
declaring a button, and pressing the button before the page is
correct, which is not a workaround so much as a different program.

So the framework had exactly half a mechanism, and the half it had was
the wrong half for everything Elan named first (`System.Environment`,
string functions). `PlaySound()`, the other example, needed nothing —
it is an effect and the push form already fits it exactly.

## The provider contract

```go
// markup/values.go
type ValueProvider interface {
        NewValue(c *Call) (*prop.Property[string], error)
}

func RegisterValues(uri string, p ValueProvider)
func RegisteredValues() []string
```

`Call` is the *same* struct handler providers receive, with `Target`
left invalid — a provider can therefore tell the two positions apart
without a second type. `NewValue` runs at build time, once per
expression, so arity, argument types, unknown functions and (for `env`)
names outside the host's grant are load errors.

### Damage tracking is not implemented here; it is inherited

The load-bearing sentence of this whole record:

> A provider builds its handle with `prop.NewComputed`, so every
> `Arg.String()` it calls runs **inside an evaluation** — which is what
> makes that `Get` a subscription rather than a read.

`{{str:Upper .Name}}` therefore repaints exactly the components that
display it, when and only when `.Name` changes, and nothing in
`markup/values.go` participates. This falls straight out of the
existing rule that *the `Get` call site decides subscribe-vs-read*
(`prop/prop.go:33`). `bindText` already wrapped its parts in a
computed; a value handle is just another part.

Three damage-count assertions pin it rather than implying it:
`TestValueExpressionRepaintsOnlyItsReaders` (1),
`TestTwoValueExpressionsOverOneArgumentPaintTwice` (2), and
`TestSetUpdatesReadersAndPaintsOnlyThem` in the env pack (1 — the
static sibling does not repaint, and neither does the button, because
the Composer had already focused it).

The corollary is the trap the repo already documents, and it bites
providers harder than it bites pages: an argument read behind an early
return or a short-circuit drops out of the dependency set on the frames
where it does not run. `env:Get`'s optional fallback and
`str:Default` both hoist *both* reads above the branch, with a comment
saying why, and both have a test that fails if someone un-hoists them.

### Two registries, on purpose

`RegisterValues` and `RegisterHandlers` are separate maps keyed by the
same URI. A namespace may register on one, the other, or both, and
`handlers/env` is the one that does both:

```go
p := envhandlers.NewWritable("EDITOR")
markup.RegisterValues(envhandlers.URI, p)   // env:Get, env:Names
markup.RegisterHandlers(envhandlers.URI, p) // env:Set, env:Unset
```

The reason is that "may read the environment" and "may write the
environment" are different decisions, and a single registration would
force a host to grant both to get either.

The cost is a new way to be confused, so both crossovers get a
specific message rather than a generic "not registered":

```
markup: {{net:Get …}} is in a value position, but
"gooey.dev/handlers/net" is registered as a HANDLER namespace
(event-only): invoke it from an event attribute, as
Click="{{net:Get … | into .Target}}"
```

and, from the provider side, `env:Set` written in a `Text` says *"Set
is an effect, not a value"* while `env:Get` written on a `Click` says
*"Get is a value, not an effect"*. Those two are the mistakes a person
actually makes, because the natural move is to reach for the half that
exists.

## `env`'s grant is an itemized allowlist, and that is a security call

`handlers/fs` grants a subtree; `handlers/exec` grants named commands.
`handlers/env` follows exec, not fs, and the reason is that the
environment is not a uniform space. It is where a process keeps
`AWS_SECRET_ACCESS_KEY` next to `TERM`.

A page loaded from an untrusted `fs.FS`, in a host that also granted
`net:Get`, would otherwise be a two-element exfiltration path with no
app code:

```xml
<Text Name="s">{{env:Get `AWS_SECRET_ACCESS_KEY`}}</Text>
<Button Click="{{net:Get .Leak | into .Ignored}}"/>
```

So `New(names...)` grants exactly those names, an ungranted name is a
load error naming the grant, and there is deliberately **no**
"grant everything" constructor. A host that wants one can enumerate
`os.Environ()` itself and see what it is doing.

The variable name must be a backtick literal, never a bound path —
the same rule `sys:Run` applies to command names, for the same reason:
a runtime value cannot be checked against an allowlist at load time,
and load time is where this framework refuses things.

One pleasant consequence: because the grant is a *list* rather than a
predicate, it is **enumerable**, and `{{env:Names}}` renders it. That
is a real asymmetry with the handler side — a handler grant is a
registered provider and cannot be introspected per function — and it
is worth remembering the next time someone tries to probe capabilities
uniformly.

## Reactivity of `env`, stated plainly

`env:Get` hands out a **source property per name**, cached on the
provider, seeded from the real environment on first resolution.
`env:Set` writes the process environment *and* that source, so the
change travels the ordinary graph and repaints exactly its readers;
two bindings to one variable share one source.

Nothing polls. A variable changed by anything other than `env:Set` is
not noticed. For what a terminal UI actually reads — `USER`, `HOME`,
`TERM`, `SHELL` — that is correct and cheap. For something genuinely
live, a host-owned source property updated from a `Startable` is the
right shape, and this pack does not pretend otherwise.

## Markup-only versus Go, per capability

Elan asked for both versions of each capability and a recommendation.
The recommendations share one shape, so it is worth stating once:
**the Go version is better whenever the app knows at build time what it
needs; the markup version earns its place exactly when the page — not
the app — is the thing that decides.** That is served markup,
hot-reloaded markup, markup written by someone who cannot edit Go, and
markup from a source the host does not trust. The grant is always Go,
in every case, which is the capability model working rather than a gap.

**Environment.** Markup: `{{env:Get \`USER\`}}` plus one
`RegisterValues` line. Go: `ctx.Values["User"] =
prop.NewSource(os.Getenv("USER"))` and bind `{{.User}}` — one line,
typed, and the app can normalise or validate the value on the way in.
*Prefer Go* for a fixed set an app knows about. Reach for the namespace
when the page chooses.

**String functions.** Markup: `{{str:Upper .User}}`. Go:
`prop.NewComputed(func() string { return strings.ToUpper(user.Get()) })`
in the viewmodel. *Prefer Go* for anything the app owns — a computed is
more readable than a call chain the grammar cannot even nest, and it
can do arbitrary work. The exception is genuinely presentational
width work (`str:Pad`, `str:Truncate`): that is a layout decision, it
belongs to the page rather than the viewmodel, and the markup form is
the better practice there.

**`PlaySound()`.** Markup: `Click="{{sound:Play \`ding\`}}"` — a
handler provider whose `New` takes an allowlisted name→asset map, in
the `sys:Run` shape. Go: `ctx.Handlers["OnSave"] = gooey.Command(…)`.
*Prefer Go* unless sound selection is a page-authoring decision. **No
framework feature is missing for this one** — the push form already
fits it exactly. What it needs is a pack, and because audio needs a
real dependency it must be a **nested module** under the two-direct-
requirements rule, which is why it is not in this change.

## Invariants check

- **No reflection.** `ValueProvider` is a typed factory; arguments are
  `*prop.Property[string]` handles resolved through `resolveArg`'s
  type-switch; the result is a typed handle. `git grep -l '"reflect"'`
  is unchanged by this record.
- **The `Get` call site decides.** Untouched, and now doing more work:
  it is the entire reason value namespaces are damage-correct.
- **Every `Render` is its own paint node.** Untouched — a value handle
  is a part of the same `bindText` computed a path binding produces.
- **Load-time strictness.** Strengthened, and this is the change most
  likely to surprise: content that used to load now fails. Measured
  blast radius on the whole repo: **zero** — the full root suite and
  every nested module stayed green, and no document in the corpus
  contained an unresolvable `{{…}}`. There is no escape hatch for a
  literal `{{` in content today; see open questions.
- **UI-goroutine confinement.** `NewValue` runs at build time on the
  loading goroutine and returns a handle; nothing here spawns a
  goroutine. `env:Set`'s Command runs on the UI goroutine and Sets
  there, so the repaint lands in the same frame rather than the next —
  which is why it needs no Dispatcher post, unlike an async handler.
- **Capability grant.** Unchanged in shape, doubled in resolution: a
  namespace can now grant its read half without its write half.
- **Two direct requirements.** Unchanged; both packs are standard
  library only and live in the root module beside `net` and `fs`.

## Showing an app its own source — designed, not built

Elan's fourth question: *"also notably missing is the ability to just
show the code for the App."* It is missing, and the survey says why:

- `markup.Build` and `markup.Load` **discard `src` after parsing**
  (`markup.go:311`, `markup.go:264`). `Element` carries no source text,
  no byte offsets, no line numbers. `page` holds the `fs.FS` and the
  *name*, not the bytes.
- The control plane cannot serve it either. `control.Service.Doc
  func() []byte` exists (`control/control.go:66`) but is **input-only**
  — consumed internally by `DeclaredSchema`. No RPC response and no MCP
  tool returns markup text; every `source` field in `control.proto` is
  a *request* field.
- There is no viewer. `examples/wysiwyg` shows generated markup in a
  plain `components.Text` (`Style="dim"`, no scroll, hard-clipped), and
  the only per-span line renderer in the repo is
  `cmd/browser/markdown.go`, which is `package main`.

The design, in three layers, smallest first:

1. **A source seam.** `markup.Page` already holds `fsys` and `name`;
   exposing `Source() ([]byte, error)` (re-reading through the FS,
   exactly as `Context.includeElements` already does at
   `catalog.go:506`) is the whole mechanism. Re-reading beats retaining
   because it stays correct under the dev watcher, and it keeps `Build`
   allocation-free. `examples/wysiwyg` already has the two-line version
   of this — `pageSrc := func() []byte { … }` at `main.go:151` — wired
   to `grpc.Options.Doc` and never displayed.
2. **A value namespace over it**, which is why this belongs in *this*
   record rather than a separate one: `{{src:Page}}` and
   `{{src:File \`card.gooey\`}}`, granted with
   `markup.RegisterValues(srchandlers.URI, srchandlers.New(pageFS))`.
   The `fs.FS` is the grant, exactly as in `handlers/fs`, so an app
   shows its own source by granting its own page FS and nothing else.
   Note that this is safe under the new strictness *because* markup
   source arrives as runtime **data** in a property — `bindText` runs
   over the document at build time and never re-parses a value — so a
   `{{…}}` inside the displayed source is inert.
3. **A viewer component.** The real gap. `components.Text` clips and
   cannot scroll; `ItemsView.Scroll` is the only scrolling multi-line
   surface and has no markup attribute. A `<CodeView>` wants
   `ItemsView`'s scrolling plus `markdown.go`'s `mdSpan`/`mdLine`
   lifted out of `package main`, plus a line-number gutter
   (`cmd/finder`'s `previewPane` is the prior art). Syntax highlighting
   is a fourth thing and should not be conflated with it — the repo has
   no lexer of any kind today.

Layers 1 and 2 are small and mostly designed (issue #224). Layer 3 is a
component and belongs to the component pipeline, not here (issue #225).

## Explicitly out

- **Nesting.** ``{{str:Upper env:Get `USER`}}`` does not parse, and
  this record does not make it parse. The grammar is flat by decision
  (`markup/expr.go`), and the visible cost is real: `str:Default` and
  `env:Get`'s optional fallback are two ways to say one thing, because
  neither composes with the other. Recorded, not hidden — issue #223.
- **Pipe transforms in a value position.** ``{{.Bytes | human}}`` is
  issue #99's reserved space, specified in pipeline grammar v2. This
  record occupies the *call* form only and touches no stage name. When
  #99 lands, `str`'s functions are the obvious first converter stages
  and composition arrives with them.
- **Non-string value expressions.** `{{env:Get …}}` cannot appear in
  `Visibility=`, `Style=` or any typed attribute, because those resolve
  through `boundProp[T]`/`BindingValue` rather than `bindText`.
  Extending them means a kinded `NewValue`, against `propKinds` — issue
  #222.
- **`<x:Property Default="{{ns:Fn …}}">`.** A declaration's `Default`
  is coerced from a literal at parse time and never sees a context, so
  a value call there is not resolvable. Left alone.
- **A live environment.** No polling, no inotify; see above.
- **`| into` in a value position.** A load error naming the fix, rather
  than a tolerated no-op.
- **A `sound` pack.** Push-shaped, needs a real dependency, therefore a
  nested module. Issue #226, not built.

## Open questions (for Elan)

1. **An escape for a literal `{{`.** There is none, and nothing in the
   repo needs one today (measured). Add `{{{{` → `{{`, or a
   `<Text Literal="true">`, or leave it until something asks? Issue #227.
2. **Should the value registry be enumerable per function?** `env` can
   answer `{{env:Names}}` only because its grant is data. A general
   `Describe(fn)` on `ValueProvider` — the `Introspector` shape
   pipeline grammar v2 already designs for the handler side — would let
   a control plane preflight a document's value surface too. Worth
   unifying now, or after #41 builds `Introspector`?
3. **Does `str` survive #99?** If converter stages land, `str:Upper .X`
   and `.X | upper` are the same thing twice. Deprecate the call form
   then, keep both, or make the stages *be* this pack?

## Filed

| # | kind | what |
|---|---|---|
| [#221](https://github.com/WonderForgeLabs/gooey/issues/221) | bug | a namespace call in a value position was silently rendered as literal text (**fixed here**) |
| [#222](https://github.com/WonderForgeLabs/gooey/issues/222) | feature | value expressions in typed attributes (`Visibility`, `Style`, `Background`) |
| [#223](https://github.com/WonderForgeLabs/gooey/issues/223) | research | how value expressions compose — nesting vs #99's converter stages |
| [#224](https://github.com/WonderForgeLabs/gooey/issues/224) | feature | a source seam plus a `src:` value namespace, so an app can show its own markup |
| [#225](https://github.com/WonderForgeLabs/gooey/issues/225) | feature | `<CodeView>` — a scrolling, line-numbered, read-only text surface |
| [#226](https://github.com/WonderForgeLabs/gooey/issues/226) | feature | `handlers/sound` — `sound:Play` behind an allowlist, nested module |
| [#227](https://github.com/WonderForgeLabs/gooey/issues/227) | research | no escape for a literal `{{` in content after brace strictness |
