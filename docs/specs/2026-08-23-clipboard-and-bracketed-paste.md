# Clipboard and bracketed paste (decision record)

Decided 2026-08-23 while adding copy/paste to `apps/wysiwyg`. Two
features that share three key bindings and have almost nothing else in
common; conflating them is how you get both wrong.

## What was there before: nothing

`grep -rn '2004' term/ input/` returned nothing. `grep -rni 'osc52\|
clipboard'` over the whole tree returned four comment lines in
`components/textbox.go` explaining why its kill buffer is deliberately
NOT the system clipboard. So:

- A paste was **indistinguishable from typing.** The terminal wrote the
  clipboard's bytes to the tty exactly as if every key had been pressed,
  and every newline decoded as `KeyEnter`, which means "activate". In
  the designer, the payload's letters also fired the root KeyBindings:
  `d` toggles design mode, `x` deletes, `q` quits.
- Nothing could put text on the system clipboard at all.

## Decision 1 — a paste is ONE event on the ONE ordered stream

`input.EventPaste` is a third `EventKind`, with a `Paste PasteEvent`
arm on `input.Event`. Not a second channel: keys, mouse reports and
pastes interleave on one wire, and two channels could reorder them.
That is the same invariant `input/mouse.go` already states for mouse.

Not N key events either. The terminal has just solved the boundary
problem for us; a consumer that had to reassemble N keys back into a
string would solve it again and get it wrong the first time a payload
contained a tab.

Decoding is in `decodeCSI`, **before the key mapping** — `CSI 200 ~` is
shaped exactly like a key sequence and would otherwise be dropped as an
unmapped one, with its payload arriving as the keystroke burst the mode
exists to prevent. A stray `CSI 201 ~` (the mode toggled around a
suspend; a paste that straddled the window) takes the decoder's existing
"complete but unmapped, skip it" contract: `n > 0, ok = false`.

### The boundary that passes silently

**An unterminated paste keeps WAITING even when `idle` is true**, unlike
a truncated CSI, which resolves to the Esc key.

`idle` exists to resolve the ESC-vs-CSI *ambiguity* — a lone ESC and the
first byte of a sequence are the same byte. `ESC [ 200 ~` has no
ambiguity: six bytes nothing else spells and no keyboard can produce. A
large paste crosses many 128-byte reads in `term.DecodeEvents` and
routinely outlasts `EscTimeout` (40ms), so resolving on idle would
deliver a stray Esc followed by the payload as keystrokes — the original
failure, plus an Esc.

Cost, stated rather than hidden: a terminal that opens a bracket and
never closes it wedges the decoder, which then holds every subsequent
keystroke in the pending buffer. A byte cap was rejected because it
silently TRUNCATES — a user who pastes 40KB and gets 8KB has no way to
tell, where a wedge is at least visible.

Pinned by `TestUnterminatedPasteWaitsEvenWhenIdle`.

**The same reasoning was later extended to a split MARKER, and that half is
now bounded.** `ESC [ 2` is a strict prefix of the six-byte opener, so
[#425](https://github.com/WonderForgeLabs/gooey/pull/425) gave it the same
wait. Unlike an open paste, that buffer is *also* three keys a person can
type — Esc, `[`, `2` — and for those the wait never ends: the Esc is
never delivered, the next keystroke is absorbed into the CSI parse, and the
decoder wakes every 40ms forever
([#440](https://github.com/WonderForgeLabs/gooey/issues/440)). It now resolves
after `term.PasteMarkerGrace` consecutive timeouts, through
`input.DecodeFinal`. The unterminated-paste wedge above is explicitly **not**
on that scale and still waits indefinitely — see
[specs/2026-09-01-paste-marker-grace.md](2026-09-01-paste-marker-grace.md).

## Decision 2 — on by default, and `TextBox` must handle it

`gooey.App` enables mode 2004 by default with `WithoutPaste()` to opt
out, matching `WithoutMouse()`.

The default creates a regression that had to be closed in the same
change: **an app that enables the mode and implements `PasteHandler`
nowhere DROPS pastes** that previously arrived as keystrokes and were
inserted one rune at a time. So `components.TextBox` implements
`HandlePaste`, flattening newlines and tabs to spaces and dropping other
control bytes — a single-line field has no way to show a second line, and
"insert the payload verbatim" is not the absence of a policy, it is the
policy of inserting NULs into the value.

`Composer.Handle`'s switch was made exhaustive on `ev.Kind` at the same
time. Its previous `if IsMouse() … else Dispatch(ev.Key)` shape would
route a paste as a zero `KeyEvent` — `KeyRune` of rune 0 — which matches
no binding, consumes nothing and reports nothing.

Routing itself is `FocusManager.DispatchPaste`: focused component, then
up its ancestors, first true wins. There is deliberately **no**
`PreviewPasteHandler` — every tunnelling phase in this framework exists
because something real needed to swallow an event for the layer beneath,
and nothing needs to swallow a paste yet.

## Decision 3 — `Restore` disables 2004 unconditionally

Alongside `DisableMouse`, and for the same reason: leaving the mode set
after exit corrupts the user's **shell**. Every paste they make
afterwards is wrapped in a literal `[200~` … `[201~` on a command line
that has nothing to do with the app that did it and nothing on screen to
connect the two.

Unconditionally — not "if we enabled it" — because the redundant escape
costs nothing and a missed one strands a terminal whose mode was set
before this Screen existed (a suspend/resume, a hosted guest, a crashed
predecessor). Pinned by
`TestRestoreDisablesPasteAndMouseUnconditionally`, which deliberately
never calls `EnablePaste`.

## Decision 4 — OSC 52 is implemented in ONE direction, and the other
## direction has no API at all

    WRITE  ESC ] 52 ; c ; <base64> ST      broadly supported
    READ   ESC ] 52 ; c ; ? ST             refused by most terminals

The refusal is correct on the terminals' part: a readable clipboard lets
any program that can write a byte to your tty exfiltrate whatever you
last copied. xterm gates it behind `allowWindowOps`, kitty behind
`clipboard_control read-clipboard`, VTE (GNOME Terminal and everything
built on it) refuses outright with no setting, iTerm2 prompts. A
terminal that refuses does so **silently** — the query goes out and no
reply comes, which is indistinguishable from a reply that is slow.

So `term/clipboard.go` has no read function, and that absence is the
decision rather than an omission. A `ReadClipboard() (string, bool)`
would have to block on a timeout, and every caller would read the
timeout as "the clipboard is empty" — a silent wrong answer, where an
absent API is a compile error.

**Bracketed paste is what replaces it.** Pasting INTO an app needs no
clipboard read: the user presses their terminal's own paste key, the
terminal performs the privileged read, and mode 2004 tells us where the
result starts and stops. That is the answer to give a user who asks why
the app cannot paste on its own.

### Two silent failures closed in the write path

- **An empty payload CLEARS the clipboard** on most terminals, so
  `ClipboardSeq("")` refuses. Copying an empty selection would otherwise
  destroy whatever the user had there, with no undo, as the result of a
  no-op.
- **An oversize sequence is dropped silently**, so the clipboard keeps
  its old contents while the app says "copied". `ClipboardLimit` is
  74994 base64 bytes — xterm's practical limit, adopted by tmux; it is
  not a protocol constant — and the sequence is REFUSED past it rather
  than truncated, truncation being the same silent-wrong-answer shape.

There is no acknowledgement in OSC 52, so a nil error means the escape
was *written*, never that the clipboard changed.
`term.ClipboardCaveat()` names the known cases where a written sequence
does not arrive — `TMUX` set (needs `set -g set-clipboard on`) and `STY`
set (GNU screen). It keys off `STY` and **not** `TERM`, because
`TERM=screen-256color` is what tmux sets too, so a TERM-based check
names the wrong multiplexer for most of the people who would see it.

## Decision 5 — pasting a COMPONENT subtree: rename, and re-key

Two failure modes produce a document that builds and is wrong.

**Names collide.** A `Name` is an address — the outline, the property
grid, `hitTest` and every `{{.Binding}}` resolve by it — and
`markup.Build` treats a duplicate as a load error. Policy: strip
trailing digits for a base, take the lowest free suffix from 2 up
(`T1` → `T2` → `T3`, never `T1_copy_copy`); leave a name that is already
free alone, which is what makes pasting into a *different* document
preserve the names the user's bindings were written against; descend
into the subtree, because inner names collide too and an element nobody
looks at is where a duplicate goes unnoticed.

The non-obvious half: the "already used" set must include names derived
from `ctx.Values` keys, not just names in the tree. Binding keys are
`<Name>_<Attr>` and nothing ever unregisters one — deliberately, so an
undone paste can be redone onto the values the user had set — so a name
can be free in the tree while still owning live handles, and reusing it
silently adopts a deleted element's state.

**Bindings alias.** `markup.Seeded` gives each new instance its own keys
precisely so a second `<Gauge>` does not move the first one's needle. A
copy inherits the original's keys, so without re-keying both elements
bind the same handle: both documents load, both paint, and moving one
moves the other.

`markup.SeedKey` and `markup.SeedPlaceholder` were **exported** for this
rather than the rule being restated in the editor. Two callers spelling
`name + "_" + attr` for themselves is the drift this repo keeps
deleting, and reaching for `PlaceholderFor` alone is worse than it looks:
it returns nil for `KindCommand`, so a pasted `<KeyBinding>` would
silently lose its action. `SeedPlaceholder` knows a command needs a real
`gooey.Action`.

A binding that is *not* of that shape is carried across verbatim. It
names a viewmodel property the user wired by hand; rewriting or dropping
it would be editing their intent, and if the destination has no such name
the build says so visibly.

**The deep copy must carry `node.Slots`.** Slots holds property elements
(`<ItemsView.ItemTemplate>`) — structured attributes, not children — so a
copy that walks `Kids` and stops loses a whole subtree with no error
anywhere. `control.collectSubtree` records the same lesson from the
other end. A pre-existing gap found while doing this and not fixed here:
`parentIn` (`apps/wysiwyg/main.go`) walks only `Kids`, never `Slots`, so
a node inside a property element has no findable parent and
`deleteSelected` silently does nothing for it.

**Cut checks before it copies.** `deleteSelected` refuses the user's
root, so a cut that copied first and deleted second would leave the user
believing the root was on the clipboard while it sat untouched — and the
next paste would duplicate it.

## Undo

Nothing here opts into undo, and nothing needs to. `apps/wysiwyg`'s
mutation seam is `ed.rebuild()` itself, which `undo.go` hooks; every
mutator already passes through it, so a rule enforced there cannot be
forgotten by the next writer. The clipboard holds deep copies rather
than pointers, because undo replaces `ed.root` wholesale and every prior
`*node` pointer dangles — and because a cut-then-paste of a live pointer
would alias one node into two places in one tree.
