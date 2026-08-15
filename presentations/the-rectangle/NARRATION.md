# The Rectangle — narration script

Twenty-four beats in two acts. **Act I · Concept** (1–16) is the explanation:
what a terminal actually is, why drawing one is harder than it looks, and the
one idea the framework is built around. **Act II · The Rectangle** (17–24) is
the same claims, live, in a running program that gets rewritten from outside
while the audience watches.

Act I is adapted from `examples/introdeck`, which is a talk of its own with its
own runnable host; several of its beats want a real program on screen rather
than a slide, and each says so. Act II is the functional demo and needs nothing
but the wysiwyg editor.

The script, the slides, and the deck data are
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

**Two voices, and the switch is the point.** Every beat carries a `**VOICE:**`
marker. Beats 1–11 are the presenter; 12 onward — the whole mechanism
explanation and all of Act II — are the agent, in a second voice. Beat 11 is
the hand-over and says so out loud, so the change must be audible or the beat
does not land. `say.sh all` reads the markers.

**What must not be faked.** Act II beats 18, 19, 20 and 23 each make a claim the
screen has to back up: that only the card repainted, that one attribute
collapsed two panes, that those addresses are real and reachable, and that
`Deck.*` was registered into a process compiled without it. Each is trivially
falsifiable live. If a take doesn't show it, re-run rather than narrate around
it. Act I's live beats (3, 5, 6, 7, 8, 16) are the same rule: they host a real
program or they are honestly a slide about one.

**Timing.** Every `DURATION` here is **measured** off the rendered wav, not
estimated — `ffprobe` on `audio/NN-*.wav`, rounded to the second. The first pass
of this file estimated them and ran short on nearly every beat, by as much as
twenty-four seconds; an estimate you rehearse against is worse than no number,
because you pace to it. Re-run `say.sh all` and re-measure whenever the words
change.

`HOLD` is dead air *after* the words, where the thing being described actually
happens on screen. Beat 24's hold is `∞` — the deck ends and stays up.

Spoken 16:41 · holds 3:22 · **total 20:03**, split 12:12 / 7:51 across the two
acts. The holds on 6, 7 and 22 are demos, not pauses; don't compress them.

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

**DURATION:** 0:24 · **HOLD:** 0:03 · **VOICE:** narrator

```slide
{
  "Deck.Kicker": " 01 · THE RECTANGLE ",
  "Deck.Title": " ██████   ██████   ██████  ███████ ██    ██\n██       ██    ██ ██    ██ ██       ██  ██\n██   ███ ██    ██ ██    ██ █████     ████\n██    ██ ██    ██ ██    ██ ██         ██\n ██████   ██████   ██████  ███████    ██",
  "Deck.Body": "a framework for building things inside that rectangle\n\n\nThat black box is a terminal. The oldest way of using a computer that people\nstill use every day, and the whole idea of it is very simple:\n\n    you type a command · you press enter · it prints something back\n\nThen the whole conversation scrolls up and off the top, like a receipt.\n\nNo windows. No mouse. No buttons. Just a rectangle that text moves through.",
  "Deck.Spot": "◀── everything you are about to see happens in one of these",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "this talk is in two halves. first, what the problem actually is and why the\nanswer is interesting. then the same claims, live, in a running program that\nis rewritten from outside while you watch.",
  "Deck.Meme": "no DOM. no CSS. no browser. a rectangle full of letters.",
  "Deck.Pos": "01 / 24",
  "Deck.Pct": 4,
  "Deck.Hint": "ACT I · CONCEPT ─────────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Let's start with the window.

That black rectangle is a terminal. It's the oldest way of using a computer that
people still use every day, and the whole idea of it is very simple. You type a
command. You press enter. The computer prints something back. Then you type the
next one.

That's the entire model. Text goes in, text comes out, and it scrolls up and off
the top like a receipt printing. No windows. No mouse. No buttons. Just a
rectangle that text moves through.
```

---

## 2 · The green screen

**DURATION:** 0:31 · **HOLD:** 0:08 · **VOICE:** narrator

**LIVE:** a real shell on a pty, typing to itself. `examples/introdeck` drives
this from `era/greenscreen.keys`. When the machine is slow, the slide is slow —
that is the point, and it is why it is not a recording.

```slide
{
  "Deck.Kicker": " 02 · 1970s ",
  "Deck.Title": "you type, it prints, it scrolls off the top",
  "Deck.Body": "The oldest model there is — and still exactly what happens when you open a\nterminal today.\n\n    $ ls\n    $ cat notes.txt\n    $ date\n\nNothing on that screen is a widget. There is no such thing as a button there,\nor a panel, or one thing being INSIDE another thing.\n\nThere is a cursor, and there are characters. That is the entire vocabulary.\n\nHold on to that. Everything after this is people getting more and more\nambitious with exactly those two things.",
  "Deck.Spot": "◀── text goes in · text comes out · nothing here is a widget",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "a real shell on a real pty, typing to itself from a key script. not a recording:\nwhen the machine is slow, the slide is slow.",
  "Deck.Meme": "1970s. still installed. still what a terminal is.",
  "Deck.Pos": "02 / 24",
  "Deck.Pct": 8,
  "Deck.Hint": "the whole vocabulary ────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Let's do the history, because the shape of the thing explains the problem.

This is the oldest model there is, and it is still exactly what happens when you
open a terminal today. You type a command. You press return. It prints. And the
whole conversation scrolls up and off the top, like a receipt.

Nothing on that screen is a widget. There is no such thing as a button there, or
a panel, or one thing being inside another thing. There is a cursor, and there
are characters, and that is the entire vocabulary.

Hold on to that, because everything after this is people getting more and more
ambitious with exactly those two things.
```

---

## 3 · The one that took the screen

**DURATION:** 0:41 · **HOLD:** 0:10 · **VOICE:** narrator

**LIVE:** vi, hosted. Editing a file that is genuinely part of this presentation.
Host it with `-n` so a killed session doesn't leave a swap prompt, and point it
at a throwaway target — `:wq` writes.

```slide
{
  "Deck.Kicker": " 03 · 1976 ",
  "Deck.Title": "a program stops printing and takes the whole rectangle",
  "Deck.Body": "That is an editor, and it is not printing anything. It has taken the whole\nrectangle.\n\nThere is a cursor it tracks, a file it redraws in place, and a line at the\nbottom that is its own little world.\n\nAnd it is doing all of that with the same two operations we just called the\nentire vocabulary — move the cursor, print a character. Somebody worked out\nevery single position.\n\nThat program is from 1976. It is still installed on this machine. It is,\nright now, editing a file that is part of this presentation.\n\nAnd watch the bottom of that box — it is being driven by the AI.",
  "Deck.Spot": "◀── same two operations. now addressing the whole screen at once.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "it can restructure a codebase and it cannot get out of vi.\nwe come back to that in Act II, when it stops being funny and starts being\nthe point.",
  "Deck.Meme": "fifty years old. still the thing everyone gets stuck in.",
  "Deck.Pos": "03 / 24",
  "Deck.Pct": 12,
  "Deck.Hint": "somebody computed all of it ┐",
  "Deck.Line1": "                            ▼"
}
```

```speak
Then somebody did this.

That is an editor, and it is not printing anything. It has taken the whole
rectangle. There is a cursor it tracks, a file it redraws in place, and a line at
the bottom that is its own little world. And it is doing all of that with the
same two operations we just said were the entire vocabulary — move the cursor,
print a character. Somebody worked out every single position.

That program is from nineteen seventy six. It is still installed on this machine.
It is, right now, editing a file that is part of this presentation.

And, watch the bottom of that box, it is being driven by the A I. Which is about
to discover the thing every single person watching this has discovered at least
once.

Yeah. It can restructure a codebase and it cannot get out of vi.

We'll come back to that.
```

---

## 4 · One idea per program

**DURATION:** 0:44 · **HOLD:** 0:06 · **VOICE:** narrator

```slide
{
  "Deck.Kicker": " 04 · 1980s–90s ",
  "Deck.Title": "small programs, one idea each, piped together",
  "Deck.Body": "Text in one end, text out the other, and you chain them with a pipe.\n\n    $ banner hello | cowsay\n\nbanner draws big letters. cowsay puts your words in a speech bubble and draws\na cow under it. That is the whole program.\n\nHere is the part worth noticing: neither of those was installed on this\nmachine. They are not installed — they are WRITTEN. One is about thirty lines\nof shell, the other about forty lines of awk, sitting in a folder next to this\npresentation.\n\nThat is the era in one sentence: a program was small enough that when you\nwanted one, you wrote one.",
  "Deck.Spot": "◀── both of these were written for this slide. that is how small they are.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "and look at the last thing it does — it pipes the first into the second.\nnobody planned for that. it works because the only thing either of them ever\nagreed on was text.",
  "Deck.Meme": "the composability was an accident of having no other option.",
  "Deck.Pos": "04 / 24",
  "Deck.Pct": 17,
  "Deck.Hint": "one agreement: text ─────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Meanwhile, the other tradition. Small programs. One idea each. Text in one end,
text out the other, and you chain them together with a pipe.

Those two are called banner and cowsay, and they are exactly as serious as they
look. Banner draws big letters. Cowsay puts your words in a speech bubble and
draws a cow under it. That is the whole program.

Here's the part I like. Neither of those was installed on this machine. So
they're not installed, they're written. One is about thirty lines of shell, the
other about forty lines of awk, sitting in a folder next to this presentation.
That's the era in one sentence: a program was small enough that when you wanted
one, you wrote one.

And look at the last thing it does. It pipes the output of the first into the
second. Nobody planned for that. It works because the only thing either of them
agreed on was text.
```

---

## 5 · And then it started showing off

**DURATION:** 0:42 · **HOLD:** 0:12 · **VOICE:** narrator

**LIVE:** `examples/scene` — a gooey app hosted inside a gooey app.

```slide
{
  "Deck.Kicker": " 05 · THE DEMOSCENE TRICK ",
  "Deck.Title": "an 80 × 24 terminal is an 80 × 48 screen",
  "Deck.Body": "Then, inevitably, people started doing it for no reason at all. That is a\ndemo — in the demoscene sense, written purely to prove it could be done.\n\nHere is how it works, and it is the single best trick in the room:\n\n    every cell is ONE character:  ▀   the upper half block\n\n    the terminal gives you a foreground colour and a background colour\n    per cell — so the top half is one colour, the bottom half is another\n\n    that is TWO PIXELS PER CELL, for free, in a program that believes\n    it is printing text\n\nAn 80 × 24 terminal is an 80 × 48 screen. That is what you are looking at.",
  "Deck.Spot": "◀── and it is a gooey app running inside the gooey app I am presenting from",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "the same trick carries the waveform two slides from now, and the pictures in\nAct II. where the terminal has sixel, gooey uses real graphics instead — and\nnothing in the program knows which happened.",
  "Deck.Meme": "the resolution was always there. nobody was using the bottom half.",
  "Deck.Pos": "05 / 24",
  "Deck.Pct": 21,
  "Deck.Hint": "▀ is the whole trick ────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
And then, inevitably, people started doing it for no reason at all.

That is a demo, in the demoscene sense, the tradition of writing something purely
to prove it could be done. And I want to tell you how it works, because it is the
single best trick in here.

Every character cell on that screen is one letter: the upper half block. Just a
rectangle that fills the top half of the cell. The terminal lets you set a
foreground colour and a background colour per cell, so the top half is one colour
and the bottom half is another. That is two pixels per cell, for free, in a
program that thinks it is printing text.

An eighty by twenty four terminal is an eighty by forty eight screen. And that is
what you are looking at.

It's also a gooey app, running inside another gooey app, which is the one I'm
presenting from. We'll get to that.
```

---

## 6 · And then it could hear

**DURATION:** 0:49 · **HOLD:** 0:20 · **VOICE:** narrator

**LIVE:** `examples/soundboard`. Needs a sound server; without one it still runs
and still draws, says `silent`, and the meters stay at zero.

**OVERLAPS BEAT 22.** Same program, same cells-vs-pixels point, twenty minutes
apart. If you are giving this live, cut one — beat 22 is the stronger, so this
is probably the one that becomes a slide about the era instead of a live run.

```slide
{
  "Deck.Kicker": " 06 · IT COULD MAKE A NOISE ",
  "Deck.Title": "and show you the noise",
  "Deck.Body": "A drum machine. Eight channels, a sixteen-step pattern, and what you are\nhearing is being mixed IN the program — samples added together, panned, sent\nto the speaker as one stream.\n\nThere is no file being played. Every one of those sounds is about twenty lines\nof arithmetic, worked out when the program started.\n\nNow look at how it is drawn, because there are two answers on that screen at\nonce:\n\n    THE STEP GRID   a table of on and off. discrete state. drawn as\n                    CHARACTERS, because characters line up.\n\n    THE WAVEFORM    a continuous signal. drawn as PIXELS — the same\n                    half-block trick as the last slide.\n\nSame frame. Same program. The choice is about what the data IS.",
  "Deck.Spot": "◀── the badge in the corner is an image: real graphics where the terminal has them",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "on a terminal with sixel that badge is real graphics sitting over the text. on\none without, it quietly becomes half-blocks. the program does not know which,\nand has no branch for it.",
  "Deck.Meme": "no audio library. it pipes raw PCM to pacat.",
  "Deck.Pos": "06 / 24",
  "Deck.Pct": 25,
  "Deck.Hint": "cells AND pixels ────────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
This is the part I did not expect to be able to show you.

That is a drum machine. Eight channels, a sixteen step pattern, and what you are
hearing is being mixed in the program. The samples are added together, panned,
and sent to the speaker as one stream. There is no file being played. Every one
of those sounds is about twenty lines of arithmetic, worked out when the program
started.

And look at how it is drawn, because there are two different answers on that
screen at once. The grid of steps is made of characters — it is a table of on and
off, and characters line up. The waveform underneath is made of pixels, the same
half block trick as the demo. Same frame. Same program. The choice is about what
the data is, not about what the terminal can do.

The little badge in the corner is an image. On a terminal that supports it, that
is real graphics sitting over the text. On one that doesn't, it quietly becomes
half blocks. The program doesn't know which.
```

---

## 7 · And then it could sing

**DURATION:** 0:50 · **HOLD:** 0:20 · **VOICE:** narrator

**LIVE:** `examples/synth` — click it, then play it with the letter keys.

```slide
{
  "Deck.Kicker": " 07 · 1999, ROUGHLY ",
  "Deck.Title": "the visualiser",
  "Deck.Body": "Anyone who was near a computer around 1999 recognises those bars.\n\nIt is a real spectrum analyser: it takes the samples on their way to the\nspeaker, runs a Fourier transform over them, and groups the result into bands\nyou can actually read. The white caps fall at a fixed speed — that is not\ndecoration, that is how you read a peak.\n\nUnder it, the green line is the actual waveform. Above it, a synthesiser you\ncan play with the letter keys.\n\nHere is the number to hold on to, because the rest of this talk is about it:\n\n    the audio thread runs at 48,000 samples a second\n    the screen is told 30 times a second, and told exactly ONE thing:\n    something changed\n\nWhich letters have to be redrawn as a result is worked out for free.",
  "Deck.Spot": "◀── 48 kHz in. 30 property changes a second out. that ratio is the next slide.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "the mixer owns its numbers behind an ordinary mutex, and a background task\ncopies one snapshot per frame. that is the whole bridge between a realtime\nthread and a UI that must never be touched off its own goroutine.",
  "Deck.Meme": "the bars and the border are the same tree.",
  "Deck.Pos": "07 / 24",
  "Deck.Pct": 29,
  "Deck.Hint": "48000 → 30 ────────────┐",
  "Deck.Line1": "                       ▼"
}
```

```speak
And of course, if you can draw the sound, somebody is going to draw the sound.

Anyone who was near a computer around nineteen ninety nine recognises those bars.
That is a spectrum analyser, and it is a real one. It takes the samples on their
way to the speaker, runs a Fourier transform over them, and groups the result
into bands you can actually read. The white caps fall at a fixed speed. That is
not decoration, that is how you read a peak.

Under it, that green line is the actual waveform. Above it is a synthesiser you
can play with the letter keys.

Here is the number I want you to hold on to, because the rest of this
presentation is about it. There is an audio thread in that program running at
forty eight thousand samples a second. The screen is told about it thirty times a
second, and each time, it is told exactly one thing: something changed. Which
letters on the screen have to be redrawn as a result — that is worked out for
free, by the thing I'm about to explain.
```

---

## 8 · This machine, right now

**DURATION:** 0:36 · **HOLD:** 0:08 · **VOICE:** narrator

**LIVE:** `cmd/sysmon`. Those numbers are read off the running system once a
second — one of the processes in that list is the presentation.

```slide
{
  "Deck.Kicker": " 08 · THIS MACHINE, RIGHT NOW ",
  "Deck.Title": "and all of it is still just a rectangle full of letters",
  "Deck.Body": "That box is not a picture of a program called top. It IS this machine. Those\nnumbers are being read off the running system, once a second, while I talk.\nThe processes in that list are the ones running right now — and one of them\nis this presentation.\n\nThat is the whole point of the tour:\n\n    installers with menus you arrow through\n    git tools with panels down the side\n    monitors like that one\n\nThey are applications. They have layout, and selection, and things that react\nwhen you press a key.\n\nThey are also, still, a rectangle full of letters. Nothing has changed about\nwhat the terminal can do. Everything has changed about what people ask of it.",
  "Deck.Spot": "◀── one of those processes is the program drawing this slide",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "applications, made of letters instead of pixels — and every one of them had to\nsolve the same problem by hand: what is on the screen, where does it go, and\nwhat has to be erased when it changes.",
  "Deck.Meme": "nothing changed about the terminal. everything changed about the ambition.",
  "Deck.Pos": "08 / 24",
  "Deck.Pct": 33,
  "Deck.Hint": "it is reading itself ────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Which brings us back to now, and to the thing you are actually looking at.

That box is not a picture of a program called top. It is this machine. Those
numbers are being read off the running system, once a second, while I talk. The
processes in that list are the ones running right now. One of them is this
presentation.

And that is the whole point of the tour. Installers with menus you arrow through.
Git tools with panels down the side. Monitors like that one. They are
applications — they have layout, and selection, and things that react when you
press a key.

They are also, still, a rectangle full of letters. Nothing has changed about what
the terminal can do. Everything has changed about what people ask it to do.
```

---

## 9 · Why that's harder than it looks

**DURATION:** 0:39 · **HOLD:** 0:06 · **VOICE:** narrator

```slide
{
  "Deck.Kicker": " 09 · WHAT THE TERMINAL KNOWS ",
  "Deck.Title": "the terminal has no idea any of that is happening",
  "Deck.Body": "    what the terminal actually knows about:\n\n        buttons        no\n        panels         no\n        \"inside\"       no\n        layout         no\n\n    what you get:\n\n        move the cursor\n        print a character\n\nThat is the whole instruction set.\n\nSo every border you have ever seen in one of those programs — somebody chose\nthose line characters and worked out where each one goes. Every highlighted\nrow, somebody computed. Every resize, somebody had to figure out what all of\nit becomes.\n\nAnd that is before the hard part: deciding what to ERASE and REWRITE when\nsomething changes.",
  "Deck.Spot": "◀── none of the words on this slide mean anything to a terminal",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "gooey is a framework for not doing any of that by hand. the next four slides\nare the one idea that makes it possible — and it is not the layout engine.",
  "Deck.Meme": "two verbs. everything else is somebody's arithmetic.",
  "Deck.Pos": "09 / 24",
  "Deck.Pct": 37,
  "Deck.Hint": "the hard part is erasing ┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Here's the part that's easy to miss: the terminal has no idea any of that is
happening.

It doesn't know what a button is. It has never heard of a panel. It has no
concept of one thing being inside another thing. The only things you can actually
do are move an invisible writing position around, and print a character.

That's it. That's the whole instruction set.

So every border you've ever seen in one of those programs — somebody chose those
line characters and worked out where each one goes. Every highlighted row,
somebody computed. Every time the window is resized, somebody has to figure out
what all of it becomes.

And that's before the hard part, which is deciding what to erase and rewrite when
something changes.

gooey is a framework for not doing any of that by hand.
```

---

## 10 · Which way round

**DURATION:** 0:24 · **HOLD:** 0:05 · **VOICE:** narrator

```slide
{
  "Deck.Kicker": " 10 · WHICH WAY ROUND ",
  "Deck.Title": "it flipped",
  "Deck.Body": "\n\n    \"Just a couple of years ago, we would label pictures\n     so computers knew what they had in them.\n\n     Now we have computers giving us pictures\n     to tell us what our data means.\"\n\n                                    — Elan Hasson, 8/2026\n\n\nWe used to do the tedious part so the machine could see.\n\nNow the machine does the tedious part so we can.\n\nKeep that in your head, because it is about to happen literally, on this\nscreen, to me.",
  "Deck.Spot": "◀── hold this one. Act II collects on it.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "the flip is not a metaphor here. in Act II the person stops driving and the\nagent starts, and the exact moment it happens is visible on screen.",
  "Deck.Meme": "we labelled pictures for them. now they draw pictures for us.",
  "Deck.Pos": "10 / 24",
  "Deck.Pct": 42,
  "Deck.Hint": "remember this ───────────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Before we go on, one thing I keep coming back to.

You know, it's funny. Just a couple of years ago, we would label pictures so
computers knew what they had in them. Now we have computers giving us pictures to
tell us what our data means.

That's the whole thing, isn't it. It flipped. We used to do the tedious part so
the machine could see. Now the machine does the tedious part so we can.

Keep that in your head for the next hour, because it's about to happen literally,
on this screen, to me.
```

---

## 11 · Handing over

**DURATION:** 0:21 · **HOLD:** 0:04 · **VOICE:** narrator

The last beat in the presenter's voice. Everything after this is the second
voice — including, in Act II, the building.

```slide
{
  "Deck.Kicker": " 11 · HANDING OVER ",
  "Deck.Title": "from here on: the agent",
  "Deck.Body": "\n    from here on: Claude\n\n        explains it\n        writes it\n        runs into the one thing it can't do\n\n    my job: run the program.\n    watch how long that lasts.\n\n\nI could take you through the rest of this myself. I'm not going to.\n\nEverything after this point — the explanation, and then the actual building —\nis the agent. I'm still in the room, and there is exactly one job left for me,\nwhich you will spot the moment it comes up, because it is the one thing an AI\ncannot do on its own.\n\nWatch for when that stops being true. That's the demo.",
  "Deck.Spot": "◀── the voice changes here, and it does not change back",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "there is exactly one thing left that needs a human: starting a program on a\nscreen an audience can see. Act II is where that stops being true.",
  "Deck.Meme": "watch for when the one remaining job disappears.",
  "Deck.Pos": "11 / 24",
  "Deck.Pct": 46,
  "Deck.Hint": "voice changes here ──────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
So I could take you through the rest of this myself. I'm not going to.

Everything after this point — the explanation, and then the actual building, file
by file, from an empty directory — is Claude. I'm still in the room, and there's
exactly one job left for me, which you'll spot the moment it comes up, because
it's the one thing an A I cannot do on its own.

Watch for when that stops being true. That's the demo.

Go ahead.
```

---

## 12 · The redraw question

**DURATION:** 0:21 · **HOLD:** 0:05 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 12 · THE REDRAW QUESTION ",
  "Deck.Title": "one number changes. what do you redraw?",
  "Deck.Body": "\n\n    a screen with a hundred things on it\n\n    a list, some panels, a status bar, a couple of counters\n\n\n    one number changes\n\n\n    what do you redraw?\n\n\nThat question sounds like a detail. It isn't.\n\nHow a framework answers it decides how the whole thing gets built — and it is\nthe reason this exists, rather than being one more library that draws boxes.",
  "Deck.Spot": "◀── every framework you have used answered this. most of them answered it twice.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "the next three slides are the three available answers. gooey takes the third,\nand the third one is why there is no dirty flag anywhere in this framework.",
  "Deck.Meme": "the boring question that decides everything.",
  "Deck.Pos": "12 / 24",
  "Deck.Pct": 50,
  "Deck.Hint": "this is the whole talk ──┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
Thanks.

So. Picture a screen with a hundred things on it. A list, some panels, a status
bar, a couple of counters. One number changes.

What do you redraw?

That question sounds like a detail. It isn't. How a framework answers it decides
how the whole thing gets built, and it's the reason this exists rather than being
one more library that draws boxes.
```

---

## 13 · The first two answers

**DURATION:** 0:45 · **HOLD:** 0:06 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 13 · THE FIRST TWO ANSWERS ",
  "Deck.Title": "redraw everything · or rebuild it and compare",
  "Deck.Body": "    ONE · redraw everything\n\n           simple. flickers. gets slow.\n           works until the screen gets big or the updates come fast,\n           and then you can watch the thing being rebuilt.\n\n\n    TWO · redraw everything into MEMORY,\n          compare against last time,\n          send only the differences\n\n           this is a virtual DOM, and it is a good answer.\n           it is what most of these frameworks do.\n\n           but look at what it is doing:\n\n               it rebuilds 100 things\n               to discover that 99 of them didn't change\n\n           every frame.",
  "Deck.Spot": "◀── if answer two sounds familiar from the web, it should. same shape.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "neither answer is wrong. both are doing work proportional to the SIZE OF THE\nSCREEN, when the thing that actually happened was proportional to the size of\nthe change.",
  "Deck.Meme": "rebuilding a hundred things to learn that ninety-nine are fine.",
  "Deck.Pos": "13 / 24",
  "Deck.Pct": 54,
  "Deck.Hint": "work ∝ screen ───────────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
The first answer is: redraw everything. Wipe the screen, draw all hundred things
again. It's text, it's cheap, who cares.

And that works, for a while. Until the screen gets big, or the updates come fast,
and then you get flicker, and you get lag, and you can watch the thing being
rebuilt.

So the second answer is smarter. Redraw everything, but into memory rather than
onto the screen. Then compare that fresh copy against what was there a moment
ago, find the differences, and send only those.

That's what most of these frameworks do, and it's a good answer. If it sounds
familiar from the web, it should — it's the same shape as a virtual DOM.

But look at what it's doing. Every single frame, it rebuilds a hundred things in
order to discover that ninety nine of them didn't change.
```

---

## 14 · The third answer

**DURATION:** 0:42 · **HOLD:** 0:08 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 14 · THE THIRD ANSWER ",
  "Deck.Title": "the read IS the subscription",
  "Deck.Body": "    THREE · know which parts read which data\n\n            one number changes\n              → two things on screen ever read it\n              → redraw those two\n\n            no comparing. nothing was rebuilt.\n            there is nothing to compare, BECAUSE nothing was rebuilt.\n\n\nAnd here is the part worth the whole framework: you never declare any of it.\n\nThere is no dependency list to maintain. No \"this depends on that\". No call\nto say \"I changed something, please update\".\n\nYou read the value while you are drawing.\n\nAnd the reading IS the subscription. That is the entire mechanism.",
  "Deck.Spot": "◀── no AffectsRender. no InvalidateVisual. no dirty flag to forget.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "every component's Render runs inside a computed node. a property read during\npaint records an edge in the graph — so reading a value while drawing is not a\nread that happens to work, it is the damage declaration.",
  "Deck.Meme": "work ∝ the change, not ∝ the screen.",
  "Deck.Pos": "14 / 24",
  "Deck.Pct": 58,
  "Deck.Hint": "no declaration at all ───┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
gooey takes a third route.

It keeps track of which parts of the screen looked at which pieces of data. So
when that one number changes, it already knows, before doing any work at all,
that exactly two things on screen ever read that number. It redraws those two. It
never touches the other ninety eight, and it never rebuilds them to find out it
didn't need to.

There's no comparing. There's no diffing. There's nothing to compare, because
nothing was rebuilt.

And here's the part I find genuinely elegant: you don't declare any of that.
There's no dependency list to maintain, no this depends on that, no call to say I
changed something, please update.

You read the value while you're drawing. And the reading is the subscription.
That's the whole mechanism.
```

---

## 15 · Why one list beats two

**DURATION:** 0:26 · **HOLD:** 0:05 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 15 · WHY ONE LIST BEATS TWO ",
  "Deck.Title": "the two lists can never disagree, because there is one",
  "Deck.Body": "    most systems keep TWO lists:\n\n        what the code actually reads\n        what you TOLD the framework it reads\n\n    they drift.\n\n        forgot one     →  it silently stops updating\n        one too many   →  it redraws for nothing\n\n\n    here there is ONE list,\n    and it is produced by running the code.\n\n\nIt cannot be out of date, because it is rebuilt by the act of drawing.\n\nThat is the reason this is worth building a framework around, rather than\nbeing a clever trick you use once.",
  "Deck.Spot": "◀── the failure mode that does not exist here: a stale cell with no error",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "there is one way to still get it wrong, and it is worth knowing: a Get behind\nan early return, or on the short-circuit side of || , doesn't run — so it isn't\nrecorded, and that component goes deaf to that property. hoist your reads.",
  "Deck.Meme": "you cannot forget to update a list you never write.",
  "Deck.Pos": "15 / 24",
  "Deck.Pct": 62,
  "Deck.Hint": "one list, derived ───────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
The reason that's worth building a framework around is that the two lists can
never disagree.

In most systems there's what your code actually depends on, and then there's what
you told the framework it depends on. Those drift apart. You forget an entry and
something silently stops updating. You add one too many and things redraw for no
reason.

Here there's only one list, and it's produced by running the code. It cannot be
out of date, because it's rebuilt by the act of drawing.
```

---

## 16 · The layout is a file

**DURATION:** 1:01 · **HOLD:** 0:10 · **VOICE:** claude

**LIVE:** `counter.gooey` open in vim on the left, the same file running on the
right. Edit and `:w`; the right pane rebuilds within a second.

```slide
{
  "Deck.Kicker": " 16 · THE LAYOUT IS A FILE ",
  "Deck.Title": "and the file is live",
  "Deck.Body": "You don't write the layout in code. You write it in a file that describes what\ngoes where. If you have seen HTML, it will look familiar.\n\n    <Panel Title=\"counter\">\n      <Text>{{.Count}}</Text>\n    </Panel>\n\nThat is it on the left. On the right is the SAME FILE — not a picture of what\nit would look like, and not a copy kept in step by hand. The right pane is\nthat exact file, loaded and running.\n\nAnd look at what is NOT in it. No instruction to update. Nothing watching\nanything. Nothing redrawing on a schedule. It is a description of a panel with\na number in it. The number changes because that line READS it.\n\nEdit it while the program runs, save, and watch the screen change. No restart.",
  "Deck.Spot": "◀── that is not a preview. it is the same file, loaded and running.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "and this is not a development convenience bolted on the side — it is how the app\nloads its interface in the first place. os.DirFS plus a watcher in dev, embed.FS\nin release, the same code path either way.",
  "Deck.Meme": "which matters enormously in about ten minutes, for reasons I won't spoil.",
  "Deck.Pos": "16 / 24",
  "Deck.Pct": 67,
  "Deck.Hint": "END OF ACT I ────────────┐",
  "Deck.Line1": "                         ▼"
}
```

```speak
One more idea before anything gets built.

You don't write the layout in code. You write it in a separate file that
describes what goes where — a panel, some text inside it, a line under that. If
you've seen HTML, it will look familiar.

That's it on the left. And on the right is the same file. Not a picture of what
it would look like, and not a copy I kept in step by hand. The right pane is that
exact file, loaded and running.

And look at what isn't in it. There's no instruction to update, nothing that
watches anything, nothing that redraws on a schedule. It's a description of a
panel with a number in it. The number is changing because that line reads it, and
reading it is the only thing that has to happen.

And that file is live. You can edit it while the program is running, save it, and
watch the screen change. No restart. No rebuild. Whatever the program was in the
middle of doing, it keeps doing.

That isn't a development convenience bolted on the side. It's how the app loads
its interface in the first place. Which matters a great deal in about ten
minutes, for reasons I'm not going to spoil.
```

---

## 17 · The rectangle

**DURATION:** 0:37 · **HOLD:** 0:04 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 17 · WHAT THIS IS ",
  "Deck.Title": " ██████   ██████   ██████  ███████ ██    ██\n██       ██    ██ ██    ██ ██       ██  ██\n██   ███ ██    ██ ██    ██ █████     ████\n██    ██ ██    ██ ██    ██ ██         ██\n ██████   ██████   ██████  ███████    ██",
  "Deck.Body": "A framework for building things inside that rectangle.\n\nA terminal is the oldest way of using a computer that people still use every day.\nYou type, it prints, it scrolls off the top like a receipt. No windows. No mouse.\nNo such thing as a button, or a panel, or one thing being inside another thing.\n\ngooey gives the rectangle a retained visual tree, a dependency-property graph,\nXML markup with Go-template bindings, and damage-tracked rendering.\n\nEverything on this screen — the rail, this card, the bar at the bottom — is gooey.",
  "Deck.Spot": "◀── that rail is a <Segmented>, and its icons are drawn in PIXELS, not box-drawing characters",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "this card is one <Border> on a <Canvas>, placed with Canvas.Left / Canvas.Top.\nit did not exist ninety seconds ago. it was patched into the RUNNING editor\nover MCP, by the agent typing in the other window. nothing restarted.",
  "Deck.Meme": "no DOM. no CSS. no browser. 142 × 51 cells and a damage list.",
  "Deck.Pos": "17 / 24",
  "Deck.Pct": 71,
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

## 18 · The one interesting idea

The claim in the top callout is checkable live: advancing to this slide was
eleven `set_value` calls and nothing else. The rail, the scrim and the status bar
never repainted.

**DURATION:** 1:00 · **HOLD:** 0:06 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 18 · THE ONE INTERESTING IDEA ",
  "Deck.Title": "when something changes, what redraws?",
  "Deck.Body": "Every UI framework answers exactly one question. Everything else follows from it.\n\n\n  IMMEDIATE MODE    redraw everything, every frame.\n                    dead simple. turns the terminal into a 60fps furnace,\n                    and every cell you repaint is a cell that flickers.\n\n  RETAINED MODE     keep a tree. mark things dirty BY HAND.\n                    fast — right up until you forget one InvalidateVisual.\n                    then you have a stale cell, no error, and no idea why.\n\n  gooey             neither. THE READ IS THE SUBSCRIPTION.\n\n\nThere is no AffectsRender. There is no InvalidateVisual.\nThere is no dirty flag for you to forget, because there is no dirty flag.",
  "Deck.Spot": "◀── the rail didn't repaint when this slide changed. neither did the status bar. only the card did.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "every component's Render is wrapped in a prop.NewComputed (composer.go:260).\nso reading a property while painting IS the damage declaration. the graph\nrecords the edge, and a change repaints exactly the components that read it.",
  "Deck.Meme": "\"I forgot to call InvalidateVisual\" is not a bug that is available to you here.",
  "Deck.Pos": "18 / 24",
  "Deck.Pct": 75,
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

## 19 · The layout is a file

**HOLD is the beat.** Collapse the panes live if the room is technical: edit
`Cols` in `examples/wysiwyg/wysiwyg.gooey`, save, and let them watch it reflow.
If you do, note out loud that the ColorPicker's value survived.

**DURATION:** 0:50 · **HOLD:** 0:10 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 19 · THE LAYOUT IS A FILE ",
  "Deck.Title": "the layout is a file, and the file is live",
  "Deck.Body": "This editor's shell is one Grid:\n\n    <Grid Name=\"Page\" Rows=\"1*,10,1\" Cols=\"4,38,1*,46\">\n      ActivityBar │ SideBar │ EditorArea │ Properties\n\nTo give this talk its screen, one attribute changed:\n\n    Cols=\"4,38,1*,46\"    →    Cols=\"4,0,1*,0\"\n\nThat is the entire diff. The toolbox and the property editor went to zero\nwidth; the designer took the rest.\n\nNothing restarted. The editor watches its own .gooey file, so saving it\nrebuilt the tree in place — and the ColorPicker's value, which was #DD4258\nbefore the save, was still #DD4258 after it.\n\nMarkup describes the SHAPE. The property graph holds the STATE. Different\nthings, different lifetimes — which is why a reload is not destructive.",
  "Deck.Spot": "◀── the toolbox and the property editor are still here. they are zero cells wide.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "markup loads through an fs.FS seam: os.DirFS plus a watcher in dev, embed.FS in\nrelease, the same code path either way. hot reload is not a debug-only feature\nbolted onto the side — it is what the seam is for.",
  "Deck.Meme": "the layout is data. it was always data. we just stopped compiling it in.",
  "Deck.Pos": "19 / 24",
  "Deck.Pct": 79,
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

## 20 · The tree is the API

Point at the status bar. The addresses are real; if someone in the room wants to
`curl` the MCP endpoint, let them.

**DURATION:** 0:58 · **HOLD:** 0:10 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 20 · THE TREE IS THE API ",
  "Deck.Title": "the tree is the API",
  "Deck.Body": "Look at the bottom right of this screen. Those addresses are real:\n\n    grpc 127.0.0.1:46573        mcp http://127.0.0.1:45975/mcp\n\nAn app opts in with one call:\n\n    srv, err := mcp.Serve(app, mcp.Options{Addr: \"127.0.0.1:0\", Context: ctx})\n\nAnd an agent gets fourteen tools: read the component tree, read the screen,\nsend keys, send mouse, move focus, invoke commands, set values, register new\nproperties, patch one named region, swap the whole page, and validate markup\nwithout touching the app at all.\n\nNothing here was written for the agent's benefit. Names come from Name=\nattributes. The bindable state IS the Context's Values map. The commands the\nbuttons already use are the commands an agent invokes.\n\nThe automation surface, the accessibility surface and the live-edit surface\nturn out to be one protocol.",
  "Deck.Spot": "◀── I have clicked nothing. every slide so far arrived over that mcp address.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "every tool body runs on the app's UI goroutine, marshalled through control.Bridge,\nand returns only after the next frame is composed — so a read taken right after a\nwrite sees the write. nothing holds a component reference between requests.",
  "Deck.Meme": "your UI was already an API. it just didn't have a port.",
  "Deck.Pos": "20 / 24",
  "Deck.Pct": 83,
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

## 21 · Properties are unlocked, on purpose

**DURATION:** 0:51 · **HOLD:** 0:04 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 21 · CONFINEMENT ",
  "Deck.Title": "properties are unlocked, on purpose",
  "Deck.Body": "There is no mutex on a property. Reading one is a pointer dereference and a\nslice append. That is why a frame is cheap enough to do this way at all.\n\nThe price is a rule: nothing off the UI goroutine may Get or Set. Ever.\n\n    // wrong — a data race, and nothing will tell you\n    go func() { count.Set(count.Get() + 1) }()\n\n    // right — the closure runs on the loop\n    app.Post(func() { count.Set(count.Get() + 1) })\n\nA Startable is handed `post` as its ONLY route to the graph. An MCP tool body\nnever runs on the goroutine that received the request. A Temporal activity, a\ngRPC handler, a child-process callback — all the same door.\n\nNothing in the framework will catch a violation. The tests run under -race,\nso the detector does.",
  "Deck.Spot": "◀── a 128 BPM sequencer costs this graph 30 Sets a second, not 8000. that's the next slide.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "a Startable's stop func must close AND join: func() { close(done); <-stopped }.\nclose alone lets a tick that already won its select post after Close — and then\nthe lifetime test flakes instead of failing, which is worse.",
  "Deck.Meme": "unlocked is a feature. undisciplined is a bug.",
  "Deck.Pos": "21 / 24",
  "Deck.Pct": 87,
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

## 22 · And then it could sing

If the room has speakers, run `examples/soundboard` here instead of showing the
slide. The slide is the fallback, not the plan.

**OVERLAPS BEAT 6**, which is the same program making the same point. Keep this
one and demote beat 6 if you are cutting.

**DURATION:** 0:54 · **HOLD:** 0:20 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 22 · AND THEN IT COULD SING ",
  "Deck.Title": "and then it could sing",
  "Deck.Body": "examples/soundboard: eight channels, a sixteen-step sequencer, one stereo\nstream out. Real audio — voices summed in Go with per-channel gain and pan,\ninterleaved into one buffer, piped to the sound server.\n\nTwo rendering strategies, chosen by what the data IS:\n\n    THE STEP GRID    discrete state, so it is drawn as CELLS. it lines up, it\n                     reads at a distance, and it survives a capture that only\n                     records the cell plane.\n\n    THE SCOPE        a continuous signal, so it is drawn as PIXELS, through\n                     halfblock — or sixel, where the terminal has it.\n\nAnd the rule from the last slide, under load: the mixer owns its numbers\nbehind a mutex, and a Startable copies ONE snapshot per frame. Forty-eight\nthousand samples a second become thirty property Sets.",
  "Deck.Spot": "◀── a rectangle full of letters, making a noise, and showing you the noise.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "<Image> binds from an image.Image handle, so the badge is real graphics on a\nterminal with sixel and degrades to halfblocks on one without — and there is no\nbranch anywhere in the program that knows which happened.",
  "Deck.Meme": "no dependency was added for any of this. it pipes raw PCM to pacat.",
  "Deck.Pos": "22 / 24",
  "Deck.Pct": 92,
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

## 23 · None of this existed in the binary

The turn. Everything before this could have been a well-built app; this is the
part that isn't about gooey being nice to write.

**DURATION:** 0:52 · **HOLD:** 0:12 · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 23 · BUILT FROM OUTSIDE ",
  "Deck.Title": "none of this existed in the binary",
  "Deck.Body": "Every string on every slide you have seen is a property named Deck-something:\n\n    Deck.Kicker    Deck.Title     Deck.Body     Deck.Ghost\n    Deck.Spot      Deck.Arrow     Deck.Blurb    Deck.Meme\n    Deck.Pos       Deck.Pct       Deck.Hint     Deck.Bg    ...\n\nThe wysiwyg editor was compiled knowing none of them. They were created at\nruntime, from outside the process, by an agent:\n\n    register_properties   →   14 typed source properties\n    patch_markup          →   one named region replaced: EditorArea\n    set_value             →   advance a slide\n\nThe editor did not consent, and could not have refused. There is no approval\nhook on the control plane today: an app that calls mcp.Serve grants UI-rewrite\ncapability at process start and is never asked again.\n\nThat is a known gap, written down, and it is the next thing to build.",
  "Deck.Spot": "◀── the toolbox still lists the element vocabulary. it has never heard of Deck.Title.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "registration is typed and all-or-nothing, and a name that already exists is\nrefused — the binding context stays the one source of truth. a swap that fails\nto build rolls its own registrations back with it.",
  "Deck.Meme": "the app is a document. it turns out someone else can hold the pen.",
  "Deck.Pos": "23 / 24",
  "Deck.Pct": 96,
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

## 24 · You are looking at it

Press `d` at the end if the room is the right kind of room. The pane behind the
card becomes an editable document again, live, with the deck still on it.

**DURATION:** 0:43 · **HOLD:** ∞ · **VOICE:** claude

```slide
{
  "Deck.Kicker": " 24 · YOU ARE LOOKING AT IT ",
  "Deck.Title": "you are looking at it",
  "Deck.Body": "This talk is not a slideshow. It is:\n\n    ·  one <Canvas>, patched into a text editor that is still running\n    ·  fourteen properties that did not exist when that editor started\n    ·  a <ProgressBar> bound to an int\n    ·  a scrim of the editor's own markup, in dim grey\n    ·  zero restarts, and zero lines of presentation code\n\nPress `d` and the pane behind this card becomes an editable document again.\nNothing was consumed. The editor never stopped being an editor.\n\n\n    A terminal is 142 × 51 cells and a damage list.\n    Everything else is what you decide to put in it.",
  "Deck.Spot": "◀── still an editor. still live. still has no idea it just gave a talk.",
  "Deck.Arrow": "                                        ▲",
  "Deck.Blurb": "gooey — a XAML-like TUI framework for Go. retained visual tree, lazy dependency-\nproperty graph, XML markup with Go-template bindings, damage-tracked rendering.",
  "Deck.Meme": "thanks for watching a text editor pretend to be PowerPoint.",
  "Deck.Pos": "24 / 24",
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
