# Concept: markup tiers and the loading seam

A `.gooey` file is XML: elements are widgets, attributes are properties,
and `{{.Path}}` expressions are bindings resolved against a
`markup.Context`. Resolution happens **once at build time, to property
handles** — the built widget holds the handle, so rendering performs no
lookups. The design calls this lvalue semantics.

There are three tiers of reuse, in increasing order of what they cost
you:

| Tier | Go code | What attributes do |
|---|---|---|
| **Include** | none | Become the control's context verbatim: a binding hands over the live handle, a literal a string. |
| **UserControl** | a setup func per control | You resolve and type-assert them, and build the instance's own context. |
| **Custom widget** | a `Builder` | You interpret them however you like and return any `gooey.Widget`. |

All three give the control its own context, so bindings inside a control
resolve against the control and never against the page. Data crosses the
boundary only through the instance's attributes. See
[tutorial 5](../05-usercontrols.md).

Element resolution order, in full: a registered `Widgets` builder, then a
built-in element, then the `Includes` convention (`<Card/>` →
`card.gooey`), then an error.

## The `fs.FS` seam is the deployment story

`markup.Load` reads from any `fs.FS` and cannot tell them apart:

- **Development** — `os.DirFS(".")` plus `markup.Watch`/`WatchAll`, which
  poll ModTimes and rebuild in place. See
  [how-to: hot reload](../howto/howto-hot-reload.md).
- **Release** — `embed.FS`, which reports constant zero ModTimes, so the
  same `Watch` call is a natural no-op. The same code ships both ways.
  See [how-to: embed markup for release](../howto/howto-embed-release.md).

A compiled tier (`gooey gen`, producing compiled markup and typed
surfaces) is designed but not implemented.

## Current limits

- `Style="name"` is a named lookup: no cascading, no selectors, no
  per-property overrides in markup (except `Text Bold`), and an unknown
  name silently yields the zero style.
- No DataTemplates — every list is a hand-written rows widget.
- A control's property surface is implicit and unchecked. See the
  [`x:Property` spec](../../specs/2026-08-10-markup-declared-properties.md).

Depth: [architecture.md — markup](../../architecture.md#markup).
