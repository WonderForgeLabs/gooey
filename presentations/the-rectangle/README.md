# The Rectangle

A talk introducing gooey, given *inside* gooey. Twenty-four beats in two acts.

**Act I · Concept** (1–16) is the explanation: what a terminal actually is, a
tour of what people built in one anyway, why drawing one is harder than it
looks, and the single idea the framework is built around. It is adapted from
`examples/introdeck`, and several of its beats want a real program on screen
rather than a slide — each says which.

**Act II · The Rectangle** (17–24) is the functional demo: the same claims,
live, in a running program that is rewritten from outside while the audience
watches. It needs nothing but the wysiwyg editor.

The hand-over is beat 11, and it is audible — beats 1–11 are the presenter's
voice, everything from 12 on is the agent's.

**21:16 end to end** — 17:54 spoken, 3:22 of holds, split 13:03 / 8:13 between
the acts. Those are measured off the rendered audio, not estimated; see the
Narration section.

There is no presentation program here. The slides are a `<Canvas>` patched over
the centre pane of a running `examples/wysiwyg` — the markup editor — which
keeps running underneath and goes back to being an editor when the talk ends.
Advancing a slide is one `set_value` per field; nothing rebuilds, and only the
components that read the changed property repaint.

That arrangement is not a stunt for its own sake. Four of Act II's beats make a
claim about the framework that the screen has to back up live, and building the
deck any other way would make those beats a lie:

| beat | the claim | what proves it |
|---|---|---|
| 18 | a change repaints only what read it | the rail and status bar don't flicker when a slide advances |
| 19 | markup is a live file, state outlives it | one attribute collapsed two panes; the ColorPicker kept its colour |
| 20 | the tree is the API | the `grpc` / `mcp` addresses on the status bar are real and reachable |
| 23 | the UI can be built from outside | every `Deck.*` property was registered into a process compiled without them |

Act I's live beats (3, 5, 6, 7, 8, 16) are the same rule from the other side:
they host a real program, or they are honestly a slide about one.

## One overlap the merge introduced

Beats **6** ("And then it could hear") and **22** ("And then it could sing") are
the same demo — `examples/soundboard` — making the same point twice, that the
step grid is cells because it is discrete state and the scope is pixels because
it is a continuous signal. Beat 6 is introdeck's; beat 22 is this deck's. Both
hold twenty seconds for the program to run, so it is twenty minutes apart and
forty seconds of the same thing.

Left in deliberately: cutting either one renumbers the deck, and Act II was to
be kept as it was. If you are giving this live, pick one. Beat 22 is the
stronger of the two — it says "the data decided, not the framework" out loud —
so the cut is probably beat 6, demoted to a slide about the era rather than a
live run.

Neither beat plays anything by itself. `examples/soundboard` is a separate
program and nothing here launches it; in the first rehearsal both holds were
twenty seconds of silence. It needs a second window, a pty, or `App.Suspend` the
way `cmd/browser` does it — that staging is not decided.

## Running it

Start the editor and leave it alone — **do not restart it mid-talk**, or beat 8
stops being true:

```sh
cd examples/wysiwyg && go run .
```

Collapse the side panes so the designer owns the width. This is beat 3's demo,
so do it on camera if the room is technical:

```
# examples/wysiwyg/wysiwyg.gooey
<Grid Name="Page" Rows="1*,10,1" Cols="4,38,1*,46">   →   Rows="1*,0,1" Cols="4,0,1*,0"
```

Then drive it:

```sh
cd presentations/the-rectangle
./present.py setup      # register 14 properties, install deck.gooey, show slide 1
./present.py next       # advance
./present.py 5          # jump
./present.py keys       # presenter mode: pgdn/pgup in THIS terminal
./present.py rehearse   # the whole talk unattended, with narration and holds
./present.py teardown   # give the designer pane back
```

## Driving it from the keyboard

Two ways, and they differ in more than ergonomics.

`./present.py keys` reads pgdn/pgup in the **presenter's** terminal and turns
each press into the same `set_value` calls. That terminal needs focus, which is
the real cost — but the deck stays property-bound, so beat 23's claim holds in
full.

`./present.py tabs` instead generates a deck built from `<Tabs>`, whose
`HandleKey` consumes `ctrl+pgup` / `ctrl+pgdn` from anywhere in its subtree
(`components/tabs.go:265`) — so the **editor window itself** drives the talk,
with no host code, no `gooey.Action`, and no `KeyBinding`. Setup focuses the
`Tabs` by name, because those keys bubble *up* from the focused component and
focus otherwise sits on the ActivityBar, a sibling of the pane.

Whichever is installed, the commands are the same — `present.py` sets `Deck.Sel`
as well as the `Deck.*` fields when that property exists, so `next`, `prev`, a
slide number and `keys` all drive either deck. (They did not, for a while: a
numeric jump against the tab deck moved nothing and reported success anyway.)

The trade is real and worth stating out loud: the control plane registers
**source** properties, never **computeds**. A tab index cannot recompute
`Deck.Title`, so the tab deck carries its slides **literally** in markup. The
words move out of the property graph and into the XML, which softens beat 23
from "every string is a property registered from outside" to "the whole deck
was patched in from outside". Both are true; one is a bigger claim.

`present.py` finds the app by probing loopback for a gooey MCP server. With more
than one gooey app running that guess can land on the wrong one — pin it with
`GOOEY_MCP=http://127.0.0.1:PORT/mcp`, taking the port off the editor's own
status bar.

## Narration

```sh
./say.sh all            # every beat, each in its own marked voice
./say.sh 3              # just beat 3
./say.sh 3 claude       # override the voice
./say.sh play 03-ryan
```

Each beat carries a `**VOICE:**` marker and `say.sh` reads it: beats 1–11
render as `en_US-ryan-high`, 12–24 as `en_US-lessac-high`. That split is not
decoration — beat 11 *is* the hand-over and says so out loud, so a single-voice
render makes that beat a lie.

Re-render and then **re-measure**: the `**DURATION:**` markers in `NARRATION.md`
come from `ffprobe` on the wavs, and the estimated first pass was short on
nearly every beat — one by twenty-four seconds. A timing you rehearse against is
worse than no timing, because you pace to it.

Each wav gets a `.txt` receipt holding the exact line that produced it. That is
what makes two things possible: `present.py rehearse` **refuses to run** against
audio whose text no longer matches the script, and a truncated take is
detectable, because characters-per-second is comparable across the deck. Both
checks exist because both failures happened — a stale take sounds completely
fine and times wrong, and a half-written wav measured 0:29 for a 0:47 beat.

The staleness check compares *text*, not mtime. mtime was the first attempt and
it was useless: re-measuring the DURATION markers rewrites `NARRATION.md` and
marked all 24 takes stale over a change that touched no speech. A check that
fires on every routine edit is a check you learn to skip.

Offline, via [piper](https://github.com/OHF-Voice/piper1-gpl) through `uvx` — no
key, no budget, no network, so the script can be re-cut as many times as the
timings need. The first run downloads two voice models (~230 MB) into `voices/`.
Both `voices/` and `audio/` are gitignored: they are derived, and one of them is
worth re-deriving every time the script changes.

Piper is audibly synthetic. It is a rehearsal instrument — good enough to time
the beats and hear where a sentence runs long. A final take probably wants a
better voice, and beats 4 and 7 in particular read well in a *second* voice, as
the agent speaking about itself.

## The files

| | |
|---|---|
| `NARRATION.md` | the talk. Slides (```` ```slide ````), speech (```` ```speak ````) and the backdrop (```` ```ghost ````) in one file, the way `examples/introdeck` does it — so the deck and the script cannot drift apart |
| `deck.gooey` | the slide surface: a `patch_markup` fragment rooted at `Name="EditorArea"` |
| `present.py` | the remote control |
| `say.sh` | the narration renderer |

`NARRATION.md` is the only file to edit for content. `deck.gooey` is layout and
changes rarely; `present.py` reads both and hardcodes neither.

One sharp edge if you edit slide text: a slide that **shows** markup can contain
a binding as prose, and `scanBindings` finds bindings anywhere in a `Text` body
(`markup.go:984`) with no escape syntax. In the property deck that is harmless —
the text is a property *value* and nothing re-parses it. In the tab deck it is
markup, and beat 16's literal `{{.Count}}` fails the build with `"Count" not
found in context`. `present.py tabs` handles it by registering each such name as
a string whose value is its own spelling, so `{{.Count}}` resolves to the text
`{{.Count}}` and the slide shows what it meant to. It reports which names it
quoted.

## Known cosmetic defect

The body text inside the card renders on the terminal's default background
rather than the card's, so each line of text sits in a dark band. It is a real
framework gap, visible on every slide, and there is no markup-side fix:

`Border` fills its interior, and the leaf pre-clear inherits that fill
(`composer.go:263` — its comment says this exists precisely so "a Text inside a
colored panel must not punch a default-colored hole"). But `Text.Render`
(`components/text.go:36`) then writes each glyph cell with its own
`render.Style` via `SetString` → `Set` (`render/cell.go:59`), which stamps the
whole struct. None of the host's registered styles set a `Bg`, so `Style.Bg` is
the zero value, every glyph cell gets the terminal default, and only the padding
around the text keeps the card colour.

The pre-clear invariant is half-implemented: it inherits the ancestor background
for the *clear* but not for the *glyph write*. A leaf whose style leaves `Bg`
unset should merge the inherited `clearStyle.Bg`.
