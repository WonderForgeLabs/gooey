# The Rectangle

A nine-minute talk introducing gooey, given *inside* gooey.

There is no presentation program here. The slides are a `<Canvas>` patched over
the centre pane of a running `examples/wysiwyg` — the markup editor — which
keeps running underneath and goes back to being an editor when the talk ends.
Advancing a slide is one `set_value` per field; nothing rebuilds, and only the
components that read the changed property repaint.

That arrangement is not a stunt for its own sake. Four of the eight beats make a
claim about the framework that the screen has to back up live, and building the
deck any other way would make those beats a lie:

| beat | the claim | what proves it |
|---|---|---|
| 2 | a change repaints only what read it | the rail and status bar don't flicker when a slide advances |
| 3 | markup is a live file, state outlives it | one attribute collapsed two panes; the ColorPicker kept its colour |
| 4 | the tree is the API | the `grpc` / `mcp` addresses on the status bar are real and reachable |
| 7 | the UI can be built from outside | every `Deck.*` property was registered into a process compiled without them |

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
./present.py teardown   # give the designer pane back
```

`present.py` finds the app by probing loopback for a gooey MCP server. With more
than one gooey app running that guess can land on the wrong one — pin it with
`GOOEY_MCP=http://127.0.0.1:PORT/mcp`, taking the port off the editor's own
status bar.

## Narration

```sh
./say.sh all            # render every beat to audio/NN-ryan.wav
./say.sh 3              # just beat 3
./say.sh 3 claude       # beat 3 in the second voice
./say.sh play 03-ryan
```

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
