# Type-ahead search on lists

*2026-08-11*

## What was asked for

> add fuzzy search support in a new component that builds upon the list
> control… when there is a list of things, fuzzy search as a filter would be
> great. We should be able to modify the behavior/adorn i think to do this.
> the behavior i want is like what is in windows explorer. you type and the
> first match in the current sort order gets selected and as you type it
> filters by moving you to the first match and when you have no more matches
> it beeps. moving in any direction by any method 'resets' the filter but
> takes you to where you'd expect from your action.

Followed, when the ambiguities below were put back: **"do it the way windows
does it."**

That sentence is the decision record's spine. Where this document had a
preference and Explorer had a behaviour, Explorer won.

## The request contained two contradictions, and Explorer resolves both

**"Fuzzy" versus the behaviour described.** The described behaviour — "the
first match in the current sort order" — is prefix matching. Fuzzy
(subsequence) matching does not have a first match in sort order in any
useful sense: `dc` matches `dcache` and `DocumentCache` and `.doc-cache`,
scattered through the collection, and which one is "first" depends on
scoring rather than on where the user is looking. **Decision: prefix,
case-insensitive.**

Recorded precisely because a later reader will find that we already ship a
good fuzzy matcher — `cmd/finder/main.go:319`, fzf-style scoring with
consecutive-hit and segment-start bonuses — and that `ItemsView` already
carries a `[]int` row-value case (`components/itemsview.go:761`) whose only
purpose is transporting matched-rune positions to a template. Fuzzy would
have been *cheap*. It was rejected on behaviour, not on effort, and it
remains cheap to add later as an opt-in mode.

**"Filters" versus "selects".** The request says both. Explorer selects and
hides nothing, and that is what makes the rest of the sentence coherent: if
rows were hidden, "moving in any direction resets the filter" would make
rows reappear underneath the user mid-gesture, and "takes you where you'd
expect from your action" would be undefined whenever the expected row is one
of the hidden ones. **Decision: selection only. No row is ever hidden.**

## What Explorer actually does

Verified behaviour, and the specification this implements:

- Typing a printable character **enters the mode implicitly**. There is no
  arming key.
- Characters **accumulate into a buffer** that resets after **~1 second** of
  idleness. `a`, pause, `b` lands on the first `b` — not on `ab`.
- Matching is **case-insensitive prefix**, over the collection **in its
  current order**, wrapping at the end.
- **Repeating one letter cycles.** `aaa` steps through successive items
  beginning with `a` rather than searching for the literal `aaa`.
- **No match leaves the selection alone** and signals failure.
- **Any navigation resets the buffer** — arrows, Home/End, PgUp/PgDn, a
  click, the wheel.

### The idle timeout is not optional

It is what makes implicit arming survivable. Without it the buffer grows
without bound and the feature becomes unusable within seconds of the user
forgetting they typed something. An earlier draft of this design proposed
arming the mode with `/` and dropping the timeout — a visible, explicitly
armed mode has explicit exits and does not need to expire. That was
rejected: it is a different feature that happens to be adjacent, and it is
not what Explorer does.

The cost of keeping it is a clock, which is the one piece of machinery this
component would otherwise not need. See *Lifetime*.

### `j` and `k` are casualties, and that is correct

`ItemsView.HandleKey` (`components/itemsview.go:537-546`) binds `j` and `k`
to down and up alongside the arrows, in both selection and scroll mode.
Implicit arming means `j` types `j`, so vim navigation cannot survive on a
list that has a `<TypeAhead>` attached.

Explorer offers no guidance here because Explorer has no vim keys. The
resolution comes from the attachment being **opt-in per list**: declaring
`<TypeAhead>` is a list saying "I have Explorer semantics", and every list
that does not declare it keeps `j` and `k` untouched. The trade is local and
visible in the markup, which is the only reason it is acceptable.

## Shape: an attachment, not a new list

The request guessed the architecture correctly — *"modify the
behavior/adorn"*. This is a non-visual attachment in the `<Validate>` /
`<Tooltip>` / `<Timer>` family, hosted by the list it searches:

```xml
<ItemsView Items="{{.Rows}}" Selected="{{.Sel}}">
  <TypeAhead Key="Title" Search="{{.Typed}}" NoMatch="{{.Missed}}"/>
  <ItemsView.ItemTemplate>…</ItemsView.ItemTemplate>
</ItemsView>
```

It composes onto the existing control rather than forking a second list
type, and `<ItemsView>`'s `Children` is already `ModeAttachments`
(`markup/elements.go:433`) with `markup/itemsview.go:106` already calling
`attachAll` — so the host needs no change to accept it.

### This required a new dispatch seam

**Attachments were never offered keys.** `FocusManager.walk` files an
attachment into `m.bindings[host]` only when it is a `*KeyBinding`
(`input.go:402`), and `Dispatch`'s bubble consulted exactly two things per
level: that binding list, then the component's own `HandleKey`. Attachments
receive `SetHost`, `SetFocusManager`, `HoverWatcher` and `Startable` — which
is what makes the omission easy to miss — but never a keystroke.

A `KeyBinding` maps one gesture to one command. A type-ahead consumes a
whole *class* of keys and keeps state between them, which no quantity of
bindings expresses. So `Dispatch` now offers each level's attachments a
`HandleKey`, via `attachedKey` in `input.go`.

**The ordering is the load-bearing part**, and it is a three-way ordering:

1. **KeyBindings** — a gesture the page declared out loud outranks a
   behaviour that would otherwise absorb it.
2. **Attachment key handlers** — before the host, because `ItemsView` claims
   `j` and a type-ahead consulted after its host could never search for a
   word beginning with `j`.
3. **The host's own `HandleKey`**.

Reversing (2) and (3) compiles, and passes almost everything.
`TestAttachmentKeysPrecedeHost` fails on the swap; it was verified by
performing the swap, observing the failure, and restoring.

The attachment list is read from the host at dispatch time rather than
collected during `walk`. A cached per-host list would be a second statement
of what `Attachments()` already says, and per the argument this codebase
makes for itself in `markup/elementdef.go`, the second copy is the one that
goes stale — here, across a `Resync`.

### Adding the seam cannot change existing behaviour

Provable rather than probable, and worth writing down so it can be
re-checked instead of re-derived:

- Only `NonVisual` components ever become attachments. `buildChildren`
  routes a child to `attach` only on that assertion
  (`markup/markup.go:636`), and the `<X.Behaviors>` path returns a load
  error otherwise (`:648`).
- There are six `NonVisual` implementers: `markup.Validate`,
  `gooey.KeyBinding`, `components.ValidationMarker`, `components.Tooltip`,
  `components.Companion`, `components.Timer`.
- None of them implements `KeyHandler`.

The two sets are disjoint, so the seam is inert until something opts in.

### The obvious objection does not apply

A behaviour that eats every printable key sounds like it must steal input
from a `TextBox`. It cannot. Key dispatch is chain-scoped: it starts at the
focused component and bubbles up its ancestors, so a `TypeAhead` on a list
is reached only while focus is inside that list, and a focused `TextBox`
deeper in the chain consumes the keystroke several levels before the bubble
arrives. `TestAttachmentKeysScopeToTheFocusChain` pins this.

## Reset: one comparison instead of a second seam

"Moving in any direction by any method resets" appears to need mouse
routing, since attachments get `HandleMouse` no more than they got
`HandleKey`. It does not.

`TypeAhead` remembers the index it last set. On the next keystroke, if the
host's `Selected` differs from it, something else moved the selection — a
click, the wheel, a viewmodel write — and the buffer clears before the key
is processed. One comparison covers every method, including ones that do not
exist yet.

This is not an approximation of eager reset. The buffer has no observable
effect except on the next keystroke, so clearing it at that moment is
behaviourally identical to clearing it at the moment of the click.

Navigation *keys* are additionally cleared eagerly, because `Down` at the
end of a list does not change `Selected` and Explorer resets there anyway.

## The beep

The request says "it beeps". This ships **without an audible bell**, and the
reason is not the one we expected.

The expected objection was that `\a` is unreliable — rendered as a flash by
some terminals, a sound by others, nothing by many. That is true, and there
is a sharper version: `render/screen.go:148` treats `0x07` as an OSC
terminator while modelling a screen, which is the exact path the pty-log
capture pipeline uses to extract a frame. A BEL would be invisible to our
own tests and absent from every recorded GIF.

The blocking reason is structural, though. **There is no path from a key
handler to the terminal.** Input dispatch runs nowhere near the output
stream; `Frame.Flush` owns the writer. Emitting a real bell needs a
Composer-level facility — a pending-bell flag set during dispatch and
drained at flush — which does not exist today. Building one is a second
root-package change, in service of a signal our harness discards.

**Decision: the failure signal is state, not sound.** `NoMatch` is a
bindable bool the attachment owns; a page renders it however it likes. This
keeps the signal testable by assertion, keeps it visible on every terminal,
and keeps the attachment non-visual — a built-in flash would require
`TypeAhead` to paint or to own an adornment, which is precisely the shape
that made it composable.

An audible bell remains a reasonable thing to want. It is a separate
decision with a separate blast radius, and it should get its own record.

## Surface

| Attribute | Meaning |
| --- | --- |
| `Key` | which projected item value to match against. Required — nothing else can be inferred without reflection. |
| `Search` | bindable `string`: the live buffer. Optional, and the mode indicator. |
| `NoMatch` | bindable `bool`: the last keystroke matched nothing. |
| `Timeout` | idle reset, default `1s`. |

`Search` is exposed deliberately. Explorer displays nothing, and gets away
with it because a Windows list view is a familiar object with a decade of
muscle memory behind it. An invisible mode that silently changes what every
keystroke does is a UI misrepresenting itself, which is a failure class this
project has been cataloguing. Binding `Search` into a status bar costs one
property and one `<Text>`, and the demo does it.

`Key` is required rather than inferred. A projection produces
`map[string]any`; guessing which entry is the label would mean picking the
sole string key when there is exactly one, which is magic that breaks the
day a second string is projected.

## Lifetime

The timeout means a clock, and a clock means the failure mode this repo has
hit before: work landing after `Close`.

`TypeAhead` implements `Startable`, so the `Composer` owns its lifetime
exactly as it owns `Timer`'s — started when the composition goes live,
stopped by `Composer.Close`, which covers hot reload, teardown and suspend.
The goroutine never touches a property; it `post`s a closure that checks
elapsed time on the UI goroutine, the same discipline `components/timer.go`
and `cmd/browser`'s playback clock follow.

**Stop joins.** `close(done)` followed by `<-stopped`, so a tick that
already won its select has posted before stop returns. Signalling alone lets
one post land after `Close`, which is how lifetime tests flake here.

The clock is injectable (`Now func() time.Time`, the field `ItemsView` and
`FocusManager` both already use) so expiry is tested by advancing a fake
clock rather than by sleeping.

## Consequences

- Any list declaring `<TypeAhead>` loses `j`/`k` navigation. Opt-in, visible
  in the markup, and reversible by removing one element.
- A behaviour attachment can now consume keys. This is a genuinely new
  capability in the framework and other things will want it; the seam was
  kept to the smallest form that serves this case, and generalising it into
  an attachment event system was explicitly declined.
- gooey still has no bell, and now has a written reason.
- `cmd/finder` is **not** an adopter. It is fzf-shaped — a query line owning
  the keyboard, results filtered and re-ranked — which is the other design,
  and a correct instance of it. Type-ahead is for lists that have no query
  box and should not grow one.
