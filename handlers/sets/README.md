# sets — a value pack

Composition for the name-set attributes markup has grown, of which
`<Frozen Allow>` is the first:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:sets="gooey.dev/handlers/sets">
  <Frozen Active="{{.DesignMode}}"
          Allow="{{sets:Concat `Hover` `Nav` .Selected}}">
    <VStack>…the document being edited…</VStack>
  </Frozen>
</Gooey>
```

```go
markup.RegisterValues(sethandlers.URI, sethandlers.New())
```

Value registry only. There is no `RegisterHandlers` for this pack and no
writable variant: nothing here has an effect, so there is nothing to
grant beyond the pack's existence. Writing `{{sets:Concat …}}` on a
`Click` is a load error.

**Root module, on purpose** — the graph is `fmt`, `sort`, `strings`,
`unicode`, plus `gooey` and `markup`.

## A set is text

A set is names separated by spaces or commas. Nothing in this package
knows what a name means, which is what keeps it a `markup.ValueProvider`
returning a `*prop.Property[string]` — the same handle `{{.Path}}`
produces — and what lets literals and bound paths compose in one
interpolation with no new binding machinery.

A typo is therefore not a load error *here*. It is a load error where the
set is consumed (`<Frozen Allow="Clicks">` fails to load, naming the
vocabulary) or a fail-closed value at runtime (a bound `Allow` that will
not parse becomes `None`, and `components.Frozen.AllowError` says why).

Output is canonical: deduplicated, single-space separated, ordered by
`gooey.SortAllowNames` — the `Allow` vocabulary's own order, with names it
does not recognize sorted after it alphabetically. One spelling per set is
what keeps `components.Frozen`'s parse cache hitting on every routed
pointer event.

## Functions

Exported as constants, enumerated by `sethandlers.AllNames()`. The error
text for an unknown function derives from the same list, so the inventory
and the dispatch cannot drift (there is a test for that).

| function | shape | does |
|---|---|---|
| `sets:Concat` | ``{{sets:Concat `Hover` .Sel}}`` | union of one or more sets |
| `sets:Without` | ``{{sets:Without .Base `Start`}}`` | everything in the first set that no later one names |
| `sets:When` | ``{{sets:When .Design `Pointer` `Hover`}}`` | the union, or the empty set, according to a condition |
| `sets:Group` | ``{{sets:Group `Text`}}`` | expands one `gooey.Allow` group name to its primitives (`None` renders as itself — see below) |
| `sets:Has` | ``{{sets:Has .Allow `Hover`}}`` | `"true"` / `"false"` membership |

`sets:When`'s condition is false for `""`, `"false"`, `"0"`, `"off"` and
`"no"`, case-insensitively. That list exists because a bound bool renders
as `"false"` through `Arg.String`, and `"false"` is not empty — a
truthiness rule of "non-empty" would have made every `When` on a bool
permanently on.

## No nesting

The pipeline grammar has none, so ``{{sets:Concat sets:Group `Text` .X}}``
does not parse. `sets:Group` is a one-call convenience rather than a
composable stage, and the ordinary way to spell a group is to write its
name — `Text` is already a name `<Frozen Allow>` understands. See
`docs/specs/2026-08-10-pipeline-grammar-v2.md` and issue #99.

## Groups come from `gooey`

`sets:Group` and `GroupNames()` read `gooey.AllowGroups()`. That is the
one place this generic pack knows anything about a consumer, and it is a
derivation rather than a copy on purpose: a second table of expansions
would go stale, and the failure would be a page silently granted the
wrong permissions.

## `Group` and the two groups that are not unions

`sets:Group` is served straight out of `gooey.AllowGroups()`, so the
expansion cannot disagree with the constants. Three of the five groups
are plain unions of primitive names. The other two are not, and they go
opposite ways — the rule being that an expansion may WITHHOLD, but may
never GRANT MORE:

- **`All` expands** to its primitive names. `AllowAll` also contains the
  class of a key nobody classified, which is deliberately nameless, so
  the expansion parses back to a set without it. That is a narrowing,
  and narrowing is the safe direction. Rendering `All` as a single
  opaque token instead is what this algebra cannot survive: it is a set
  of NAMES, so a difference over one token removes nothing, and
  ``{{sets:Without (sets:Group `All`) `Start`}}`` evaluated back to
  `All` — granting `Start`, the one category with a child-process
  argument behind it. `TestTheAlgebraHoldsOverEveryGroup` is the guard.

- **`None` does not expand.** It has no primitive names, so its
  expansion would be the empty string — which markup reads as an
  attribute that was NOT WRITTEN, i.e. no `<Frozen>` at all. The token
  costs the algebra nothing: `None` is the identity for union and holds
  no primitive for a difference to remove.

Removing a primitive that another primitive IMPLIES is also a no-op, and
that is by design rather than a gap: the implications live in the
constants (`AllowAlpha = bitAlpha|bitFocus`), so there is no path through
this algebra that yields a key class without its focus bit.
