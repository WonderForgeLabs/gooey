# The Rectangle — narration script

Eight beats, about nine minutes. The script, the slides, and the deck data are
this one file: `present.py` reads the ```` ```slide ```` blocks and `say.sh`
reads the ```` ```speak ```` blocks. Nothing else on the page reaches either, so
prose here is for the presenter and cannot desync from what renders.

**Extraction:**

```sh
# every spoken line, in order
python3 - <<'PY'
import re, pathlib
md = pathlib.Path("NARRATION.md").read_text()
for m in re.finditer(r"```speak\n(.*?)```", md, re.S):
    print(m.group(1).strip(), end="\n\n")
PY
```

**The shape of the thing.** This is a talk *about* gooey given *in* gooey, and
more specifically inside `examples/wysiwyg` — a running markup editor whose
centre pane has been replaced with a slide. The editor is not a host that was
written to present; it is an editor, still running, still editing, that had a
`patch_markup` done to it from outside and kept going. Beat 8 collects on that,
so nothing before beat 8 may restart the process. If you have to restart, start
the talk over.

**One voice or two.** The default is one narrator throughout. `say.sh` also
takes `claude` as a role and renders in a second voice (`en_US-lessac-high`),
which is worth using if beats 4 and 7 are delivered as the agent speaking about
itself rather than the presenter speaking about the agent. Don't mix mid-beat.

**What must not be faked.** Beats 2, 3, 4 and 7 each make a claim the screen has
to back up: that only the card repainted, that one attribute collapsed two
panes, that those addresses are real and reachable, and that `Deck.*` was
registered into a process compiled without it. Each is trivially falsifiable
live. If a take doesn't show it, re-run rather than narrate around it.

**Timing.** `DURATION` is piper at default rate — measure, don't trust. `HOLD`
is dead air after the words, where the thing being described actually happens on
screen. The holds on 3, 4 and 7 are the demo; don't compress them.

---

## The scrim

Every slide paints this behind the card, in `dim`. It is the wysiwyg editor's
own markup, thinned to fit and with rows 3, 37, 39, 43 and 47–48 left blank so
the callouts have somewhere to land that isn't on top of it.

A sparser version was tried and rejected: the density is the point. At this
weight it reads as the thing the card is sitting on. Sparse, it reads as an
empty screen with a box on it.

```ghost
<Gooey xmlns="wonderforge.io/gooey/2026">   <Grid Name="Page" Rows="1*,0,1" Cols="4,0,1*,0">   <KeyBinding Gesture="d" Command="{{.ToggleMode}}"/>
  <ActivityBar Name="ActivityBar" Grid.Row="0" Grid.Col="0" Sel="{{.ActivitySel}}" Height="8" VAlign="Start"/>   <Panel Name="SideBar">
    <Tabs Selected="{{.ActivitySel}}">   <Tab Header="DESIGN">   <Text Style="dim">The designer is the centre pane.</Text>   </Tab>

        <Text>{{.Name}}</Text>   </ItemTemplate>   </ItemsView>   </VStack>   </Tab>   <Tab Header="CODE">   <Text>{{.Source}}</Text>
      </Tab>   <Tab Header="ISSUES">   <Text Style="warn">{{.FitMsg}}</Text>   </Tab>   </Tabs>   </Panel>   <Panel Name="EditorArea">
  <Preview
   <Panel
    <Grid
     <VStack
      <Text
      <Button
      <Color
     </VStack>
    </Grid>
   </Panel>
  </Preview>
  <Panel
   <Grid
    <HStack
     <Text
    </HStack>
    <ItemsView
     <ItemTem
      <Text
     </ItemTem
    </ItemsVie
    <Grid
     <TextBox
     <Button
    </Grid>
    <Text
   </Grid>
  </Panel>
  <StatusBar
   <Text
   <Text
   <Text
  </StatusBar
 </Grid>
</Gooey>

    <Panel Name="Properties" Grid.Row="0" Grid.Col="3" Title="PROPERTIES" Style="dim">   <Grid Rows="1,1*,1,2" Cols="1*">

      <TextBox Name="Edit" Value="{{.EditValue}}" Changed="{{.CommitEdit}}" AccentStyle="sel" Placeholder="value"/>




  prop.NewComputed(func() string { return fmt.Sprintf("%d demos", len(demos.Get())) })   //  a read inside a computed IS a subscribe
```

---

## 1 · The rectangle

**DURATION:** 0:34 · **HOLD:** 0:04

```slide
{
  "Deck.Kicker": " 01 · WHAT THIS IS ",
  "Deck.Title": " ██████   ██████   ██████  ███████ ██    ██\n██       ██    ██ ██    ██ ██       ██  ██\n██   ███ ██    ██ ██    ██ █████     ████\n██    ██ ██    ██ ██    ██ ██         ██\n ██████   ██████   ██████  ███████    ██",
  "Deck.Body": "A framework for building things inside that rectangle.\n\nA terminal is the oldest way of using a computer that people still use every day.\nYou type, it prints, it scrolls off the top like a receipt. No windows. No mouse.\nNo such thing as a button, or a panel, or one thing being inside another thing.\n\ngooey gives the rectangle a retained visual tree, a dependency-property graph,\nXML markup with Go-template bindings, and damage-tracked rendering.\n\nEverything on this screen — the rail, this card, the bar at the bottom — is gooey.",
  "Deck.Spot": "◀── that rail is a <Segmented>, and its icons are drawn in PIXELS, not box-drawing characters",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "this card is one <Border> on a <Canvas>, placed with Canvas.Left / Canvas.Top.\nit did not exist ninety seconds ago. it was patched into the RUNNING editor\nover MCP, by the agent typing in the other window. nothing restarted.",
  "Deck.Meme": "no DOM. no CSS. no browser. 142 × 51 cells and a damage list.",
  "Deck.Pos": "01 / 08",
  "Deck.Pct": 12,
  "Deck.Hint": "real, not a mockup ──────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Welcome to the rectangle. That black box is a terminal. It is the oldest way of
using a computer that people still use every day, and the model is very simple.
You type a command. You press enter. It prints something back. Then the whole
conversation scrolls up and off the top, like a receipt.

No windows. No mouse. Nothing on that screen is a widget. There is no such thing
as a button there, or a panel, or one thing being inside another thing.

gooey gives that rectangle a retained visual tree, a dependency property graph,
X M L markup with Go template bindings, and damage tracked rendering.

Everything you are looking at right now is gooey.
```

---

## 2 · The one interesting idea

The claim in the top callout is checkable live: advancing to this slide was
eleven `set_value` calls and nothing else. The rail, the scrim and the status bar
never repainted.

**DURATION:** 0:40 · **HOLD:** 0:06

```slide
{
  "Deck.Kicker": " 02 · THE ONE INTERESTING IDEA ",
  "Deck.Title": "when something changes, what redraws?",
  "Deck.Body": "Every UI framework answers exactly one question. Everything else follows from it.\n\n\n  IMMEDIATE MODE    redraw everything, every frame.\n                    dead simple. turns the terminal into a 60fps furnace,\n                    and every cell you repaint is a cell that flickers.\n\n  RETAINED MODE     keep a tree. mark things dirty BY HAND.\n                    fast — right up until you forget one InvalidateVisual.\n                    then you have a stale cell, no error, and no idea why.\n\n  gooey             neither. THE READ IS THE SUBSCRIPTION.\n\n\nThere is no AffectsRender. There is no InvalidateVisual.\nThere is no dirty flag for you to forget, because there is no dirty flag.",
  "Deck.Spot": "◀── the rail didn't repaint when this slide changed. neither did the status bar. only the card did.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "every component's Render is wrapped in a prop.NewComputed (composer.go:260).\nso reading a property while painting IS the damage declaration. the graph\nrecords the edge, and a change repaints exactly the components that read it.",
  "Deck.Meme": "\"I forgot to call InvalidateVisual\" is not a bug that is available to you here.",
  "Deck.Pos": "02 / 08",
  "Deck.Pct": 25,
  "Deck.Hint": "1 property Set ─────────┐",
  "Deck.Line1": "                        ▼"
}
```

```speak
Every user interface framework answers exactly one question, and everything else
follows from the answer. The question is: when something changes, what redraws?

The first answer is immediate mode. Redraw everything, every frame. It is dead
simple, and it turns the terminal into a sixty frames per second furnace. Every
cell you repaint is a cell that can flicker.

The second answer is retained mode. Keep a tree, and mark things dirty by hand.
That is fast, right up until you forget one call. Then you have a stale cell on
screen, no error anywhere, and no idea why.

gooey does neither. The read is the subscription.

Every component's render method is wrapped in a computed node. So reading a
property while you are painting is not a read that happens to work. It is the
damage declaration. The graph records the edge, and when that property changes,
exactly the components that read it repaint. Nothing else.

There is no AffectsRender here. There is no InvalidateVisual. There is no dirty
flag for you to forget, because there is no dirty flag.
```

---

## 3 · The layout is a file

**HOLD is the beat.** Collapse the panes live if the room is technical: edit
`Cols` in `examples/wysiwyg/wysiwyg.gooey`, save, and let them watch it reflow.
If you do, note out loud that the ColorPicker's value survived.

**DURATION:** 0:38 · **HOLD:** 0:10

```slide
{
  "Deck.Kicker": " 03 · THE LAYOUT IS A FILE ",
  "Deck.Title": "the layout is a file, and the file is live",
  "Deck.Body": "This editor's shell is one Grid:\n\n    <Grid Name=\"Page\" Rows=\"1*,10,1\" Cols=\"4,38,1*,46\">\n      ActivityBar │ SideBar │ EditorArea │ Properties\n\nTo give this talk its screen, one attribute changed:\n\n    Cols=\"4,38,1*,46\"    →    Cols=\"4,0,1*,0\"\n\nThat is the entire diff. The toolbox and the property editor went to zero\nwidth; the designer took the rest.\n\nNothing restarted. The editor watches its own .gooey file, so saving it\nrebuilt the tree in place — and the ColorPicker's value, which was #DD4258\nbefore the save, was still #DD4258 after it.\n\nMarkup describes the SHAPE. The property graph holds the STATE. Different\nthings, different lifetimes — which is why a reload is not destructive.",
  "Deck.Spot": "◀── the toolbox and the property editor are still here. they are zero cells wide.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "markup loads through an fs.FS seam: os.DirFS plus a watcher in dev, embed.FS in\nrelease, the same code path either way. hot reload is not a debug-only feature\nbolted onto the side — it is what the seam is for.",
  "Deck.Meme": "the layout is data. it was always data. we just stopped compiling it in.",
  "Deck.Pos": "03 / 08",
  "Deck.Pct": 37,
  "Deck.Hint": "one attribute ──────────┐",
  "Deck.Line1": "                        ▼"
}
```

```speak
The shell you are looking at is one grid. Four columns: an activity rail, a side
bar, the editor area, and a properties pane.

To give this talk a screen to live on, exactly one attribute changed. The column
widths went from four, thirty eight, star, forty six — to four, zero, star,
zero. That is the entire diff. The toolbox and the property editor went to zero
width, and the designer took everything that was left.

Nothing restarted. The editor watches its own markup file, so saving it rebuilt
the tree in place. And here is the part that matters: there was a colour picker
on screen at the time, holding a colour someone had dragged to. It was still
holding that exact colour afterwards.

Markup describes the shape. The property graph holds the state. They are
different things with different lifetimes, and that is the whole reason a reload
can be non destructive.
```

---

## 4 · The tree is the API

Point at the status bar. The addresses are real; if someone in the room wants to
`curl` the MCP endpoint, let them.

**DURATION:** 0:47 · **HOLD:** 0:10

```slide
{
  "Deck.Kicker": " 04 · THE TREE IS THE API ",
  "Deck.Title": "the tree is the API",
  "Deck.Body": "Look at the bottom right of this screen. Those addresses are real:\n\n    grpc 127.0.0.1:46573        mcp http://127.0.0.1:45975/mcp\n\nAn app opts in with one call:\n\n    srv, err := mcp.Serve(app, mcp.Options{Addr: \"127.0.0.1:0\", Context: ctx})\n\nAnd an agent gets fourteen tools: read the component tree, read the screen,\nsend keys, send mouse, move focus, invoke commands, set values, register new\nproperties, patch one named region, swap the whole page, and validate markup\nwithout touching the app at all.\n\nNothing here was written for the agent's benefit. Names come from Name=\nattributes. The bindable state IS the Context's Values map. The commands the\nbuttons already use are the commands an agent invokes.\n\nThe automation surface, the accessibility surface and the live-edit surface\nturn out to be one protocol.",
  "Deck.Spot": "◀── I have clicked nothing. every slide so far arrived over that mcp address.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "every tool body runs on the app's UI goroutine, marshalled through control.Bridge,\nand returns only after the next frame is composed — so a read taken right after a\nwrite sees the write. nothing holds a component reference between requests.",
  "Deck.Meme": "your UI was already an API. it just didn't have a port.",
  "Deck.Pos": "04 / 08",
  "Deck.Pct": 50,
  "Deck.Hint": "read it yourself ───────┐",
  "Deck.Line1": "                        ▼"
}
```

```speak
Look at the bottom right corner of the screen. Those two addresses are real, and
they are listening right now.

An application opts in with one call. You hand M C P dot Serve your app and your
binding context, and you are done.

What the other end gets is fourteen tools. Read the component tree. Read the
screen as text. Send keys. Send mouse events. Move focus. Invoke a command. Set
a value. Register new properties. Replace one named region. Replace the whole
page. And validate markup without touching the running app at all.

Now, the important part. Nothing in that application was written for the agent's
benefit. The names come from the Name attributes that were already in the
markup. The bindable state is the same values map the markup already binds to.
The commands an agent invokes are the same commands the buttons already use.

Which means the automation surface, the accessibility surface, and the live edit
surface are not three features. They are one protocol, and you get all three the
moment you have the first one.
```

---

## 5 · Properties are unlocked, on purpose

**DURATION:** 0:42 · **HOLD:** 0:04

```slide
{
  "Deck.Kicker": " 05 · CONFINEMENT ",
  "Deck.Title": "properties are unlocked, on purpose",
  "Deck.Body": "There is no mutex on a property. Reading one is a pointer dereference and a\nslice append. That is why a frame is cheap enough to do this way at all.\n\nThe price is a rule: nothing off the UI goroutine may Get or Set. Ever.\n\n    // wrong — a data race, and nothing will tell you\n    go func() { count.Set(count.Get() + 1) }()\n\n    // right — the closure runs on the loop\n    app.Post(func() { count.Set(count.Get() + 1) })\n\nA Startable is handed `post` as its ONLY route to the graph. An MCP tool body\nnever runs on the goroutine that received the request. A Temporal activity, a\ngRPC handler, a child-process callback — all the same door.\n\nNothing in the framework will catch a violation. The tests run under -race,\nso the detector does.",
  "Deck.Spot": "◀── a 128 BPM sequencer costs this graph 30 Sets a second, not 8000. that's the next slide.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "a Startable's stop func must close AND join: func() { close(done); <-stopped }.\nclose alone lets a tick that already won its select post after Close — and then\nthe lifetime test flakes instead of failing, which is worse.",
  "Deck.Meme": "unlocked is a feature. undisciplined is a bug.",
  "Deck.Pos": "05 / 08",
  "Deck.Pct": 62,
  "Deck.Hint": "one goroutine ──────────┐",
  "Deck.Line1": "                        ▼"
}
```

```speak
There is no mutex on a property. Reading one is a pointer dereference and a
slice append, and that is the reason a frame is cheap enough to build this way
in the first place.

The price is a rule, and it is not negotiable. Nothing off the U I goroutine may
read or write the graph. Not once.

If you spawn a goroutine and set a property from it, you have a data race, and
nothing will tell you. The correct version posts a closure instead, and the main
loop runs it.

That one door is the whole design. A background worker is handed post as its
only route to the graph. An M C P tool body never runs on the goroutine that
received the request — every call is marshalled onto the loop first. A Temporal
activity, a gRPC handler, a child process callback: same door, every time.

Nothing in the framework will catch you breaking this. The test suites run under
the race detector, so the detector catches it instead.
```

---

## 6 · And then it could sing

If the room has speakers, run `examples/soundboard` here instead of showing the
slide. The slide is the fallback, not the plan.

**DURATION:** 0:44 · **HOLD:** 0:20

```slide
{
  "Deck.Kicker": " 06 · AND THEN IT COULD SING ",
  "Deck.Title": "and then it could sing",
  "Deck.Body": "examples/soundboard: eight channels, a sixteen-step sequencer, one stereo\nstream out. Real audio — voices summed in Go with per-channel gain and pan,\ninterleaved into one buffer, piped to the sound server.\n\nTwo rendering strategies, chosen by what the data IS:\n\n    THE STEP GRID    discrete state, so it is drawn as CELLS. it lines up, it\n                     reads at a distance, and it survives a capture that only\n                     records the cell plane.\n\n    THE SCOPE        a continuous signal, so it is drawn as PIXELS, through\n                     halfblock — or sixel, where the terminal has it.\n\nAnd the rule from the last slide, under load: the mixer owns its numbers\nbehind a mutex, and a Startable copies ONE snapshot per frame. Forty-eight\nthousand samples a second become thirty property Sets.",
  "Deck.Spot": "◀── a rectangle full of letters, making a noise, and showing you the noise.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "<Image> binds from an image.Image handle, so the badge is real graphics on a\nterminal with sixel and degrades to halfblocks on one without — and there is no\nbranch anywhere in the program that knows which happened.",
  "Deck.Meme": "no dependency was added for any of this. it pipes raw PCM to pacat.",
  "Deck.Pos": "06 / 08",
  "Deck.Pct": 75,
  "Deck.Hint": "cells AND pixels ───────┐",
  "Deck.Line1": "                        ▼"
}
```

```speak
This is a mixer. Eight channels, a sixteen step sequencer, and one stereo stream
going out. It is not a picture of a mixer — the voices are summed in Go, with
per channel gain and pan, interleaved into a single buffer and piped to the
sound server.

Look at how two different things on that screen are drawn.

The step grid is discrete state. Sixteen steps, on or off. So it is drawn as
cells. It lines up, it reads from the back of a room, and it survives a screen
capture that only records characters.

The scope is a continuous signal. So it is drawn as pixels, through half blocks,
or through sixel graphics where the terminal supports it.

The data decided, not the framework.

And this is the confinement rule from a moment ago, running under real load. The
mixer owns its numbers behind an ordinary mutex, and a background task copies
one snapshot per frame. Forty eight thousand samples a second arrive at the
property graph as thirty sets a second.
```

---

## 7 · None of this existed in the binary

The turn. Everything before this could have been a well-built app; this is the
part that isn't about gooey being nice to write.

**DURATION:** 0:50 · **HOLD:** 0:12

```slide
{
  "Deck.Kicker": " 07 · BUILT FROM OUTSIDE ",
  "Deck.Title": "none of this existed in the binary",
  "Deck.Body": "Every string on every slide you have seen is a property named Deck-something:\n\n    Deck.Kicker    Deck.Title     Deck.Body     Deck.Ghost\n    Deck.Spot      Deck.Arrow     Deck.Blurb    Deck.Meme\n    Deck.Pos       Deck.Pct       Deck.Hint     Deck.Bg    ...\n\nThe wysiwyg editor was compiled knowing none of them. They were created at\nruntime, from outside the process, by an agent:\n\n    register_properties   →   14 typed source properties\n    patch_markup          →   one named region replaced: EditorArea\n    set_value             →   advance a slide\n\nThe editor did not consent, and could not have refused. There is no approval\nhook on the control plane today: an app that calls mcp.Serve grants UI-rewrite\ncapability at process start and is never asked again.\n\nThat is a known gap, written down, and it is the next thing to build.",
  "Deck.Spot": "◀── the toolbox still lists the element vocabulary. it has never heard of Deck.Title.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "registration is typed and all-or-nothing, and a name that already exists is\nrefused — the binding context stays the one source of truth. a swap that fails\nto build rolls its own registrations back with it.",
  "Deck.Meme": "the app is a document. it turns out someone else can hold the pen.",
  "Deck.Pos": "07 / 08",
  "Deck.Pct": 87,
  "Deck.Hint": "compiled hours ago ─────┐",
  "Deck.Line1": "                        ▼"
}
```

```speak
Here is what has actually been happening for the last few minutes.

Every string on every slide you have seen is a property. Deck dot Title, Deck
dot Body, Deck dot Ghost, and eleven more.

The editor running this talk was compiled knowing none of them. Not one. They
were created at runtime, from outside the process, by an agent holding an M C P
connection. Fourteen typed properties registered into a live binding context.
One named region of the tree replaced. And then, to advance a slide, a single
value set.

Now the uncomfortable part, and I want to say it plainly rather than skip it.

The editor did not consent to any of that, and it could not have refused.
There is no approval hook on the control plane today. An application that calls
serve grants full user interface rewrite capability at process start, and it is
never asked again.

That is a known gap. It is written down. And it is the next thing to build.
```

---

## 8 · You are looking at it

Press `d` at the end if the room is the right kind of room. The pane behind the
card becomes an editable document again, live, with the deck still on it.

**DURATION:** 0:36 · **HOLD:** ∞

```slide
{
  "Deck.Kicker": " 08 · YOU ARE LOOKING AT IT ",
  "Deck.Title": "you are looking at it",
  "Deck.Body": "This talk is not a slideshow. It is:\n\n    ·  one <Canvas>, patched into a text editor that is still running\n    ·  fourteen properties that did not exist when that editor started\n    ·  a <ProgressBar> bound to an int\n    ·  a scrim of the editor's own markup, in dim grey\n    ·  zero restarts, and zero lines of presentation code\n\nPress `d` and the pane behind this card becomes an editable document again.\nNothing was consumed. The editor never stopped being an editor.\n\n\n    A terminal is 142 × 51 cells and a damage list.\n    Everything else is what you decide to put in it.",
  "Deck.Spot": "◀── still an editor. still live. still has no idea it just gave a talk.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "gooey — a XAML-like TUI framework for Go. retained visual tree, lazy dependency-\nproperty graph, XML markup with Go-template bindings, damage-tracked rendering.",
  "Deck.Meme": "thanks for watching a text editor pretend to be PowerPoint.",
  "Deck.Pos": "08 / 08",
  "Deck.Pct": 100,
  "Deck.Hint": "that's the whole talk ──┐",
  "Deck.Line1": "                        ▼"
}
```

```speak
So, what is this.

It is one canvas, patched into a text editor that is still running. Fourteen
properties that did not exist when that editor started. A progress bar bound to
an integer. A backdrop made of the editor's own markup in grey. Zero restarts,
and zero lines of presentation code, because there is no presentation program —
there is an editor with something else in its middle pane.

If I press D right now, that pane goes back to being an editable document, live,
with all of this still on it. Nothing was consumed. It never stopped being an
editor.

A terminal is a hundred and forty two columns by fifty one rows, and a list of
what changed. Everything else is whatever you decide to put in it.

Thanks for watching a text editor pretend to be PowerPoint.
```
