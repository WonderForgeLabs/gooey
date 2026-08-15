# intro — narration script

The script, the deck, and the edit list are this one file. `introdeck` parses
it, so a ```` ```screen ```` block *is* the slide and a ```` ```speak ```` block
*is* the audio. Nothing else on the page reaches either.

**Extraction:**

```sh
# all spoken copy, in order
awk '/^```speak$/{f=1;next} /^```$/{f=0} f' NARRATION.md
```

**Two voices.** Elan opens — four beats, about two and a half minutes — and then
hands over. Everything after that is Claude, including the building. That split
is the piece's whole argument, so it should be audible: `VOICE: Elan` is Elan's
cloned voice, `VOICE: Claude` is a different one.

**The thread to protect.** Claude cannot run a program on a screen anyone can
see. So for the first half, Claude writes files and *asks Elan to run them*.
Six lines into Part 4, that stops being true. Do not lose that — the demo is a
capability arriving, not a feature being toured.

**Before a run.** Several beats host a real program that has to exist as a
binary first, because each is in its own module and `go run ../x` cannot cross
that boundary. If one is missing the island says so in red — visible, on screen,
before the take rather than during it:

```sh
(cd ../intro       && go build -o intro .)
(cd ../introtarget && go build -o introtarget .)
(cd ../scene       && go build -o scene .)
(cd ../soundboard  && go build -o soundboard .)
(cd ../synth       && go build -o synth .)
```

Beat 3.2 and beat 3.5 both edit a **tracked** file — `examples/intro/main.go`
and `counter.gooey` — because a copy would not be the program the beat claims to
be showing. Reset between rehearsals:

```sh
git checkout examples/intro/main.go examples/introdeck/counter.gooey
```

`introtarget` is the Part 4 subject: `examples/intro`'s empty tree plus
`control.NewService` and `mcp.Serve`, and nothing else. It listens on
`127.0.0.1:7900`, and every property and every element that appears in it
during 4.6 through 4.8 is registered from outside, at runtime, into a process
compiled knowing none of it.

The chronology beats also want a sound server. Without one the soundboard and
the synth still run and still draw; they say `silent` on their status line and
the meters stay at zero.

**Timing.** `DURATION` is speech at ~150 wpm. `HOLD` is dead air after the
words, where the thing being described actually happens on screen. `SLIDE` is
the deck slide; `(live)` means the real terminal is on camera and the deck is
not.

---

## Part 1 — What this even is

Elan, on camera or over a still terminal. No code appears in Part 1.

### 1.1 · The rectangle

**SLIDE:** `title` · **DURATION:** 0:40 · **HOLD:** 0:03 · **VOICE:** Elan

```screen
gooey

a framework for building
things inside that rectangle
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

### 1.2 · The green screen

**SLIDE:** `era-green` · **DURATION:** 0:55 · **HOLD:** 0:08 · **VOICE:** Elan

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Auto,*,Auto everywhere in this chronology: the caption and the
     footnote take their line and the era takes the rest, so every one of
     these slides is the size of the window it is shown in. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">1970s · you type, it prints, it scrolls off the top</Text>

  <!-- A real shell on a real pty, typing to itself from
       era/greenscreen.keys. Not a recording: when the machine is slow,
       the slide is slow. -->
  <Border Grid.Row="1" Title="a terminal" Style="island">
    <Terminal Cmd="PS1='$ ' sh -i" Script="era/greenscreen.keys" Loop="true"/>
  </Border>

  <Text Grid.Row="2" Style="dim">text goes in · text comes out · nothing on this screen is a widget</Text>

</Grid>
</Gooey>
```

```speak
Let's do the history, because the shape of the thing explains the problem.

This is the oldest model there is, and it is still exactly what happens when
you open a terminal today. You type a command. You press return. It prints. And
the whole conversation scrolls up and off the top, like a receipt.

Nothing on that screen is a widget. There is no such thing as a button there, or
a panel, or one thing being inside another thing. There is a cursor, and there
are characters, and that is the entire vocabulary.

Hold on to that, because everything after this is people getting more and more
ambitious with exactly those two things.
```

### 1.3 · The one that took the screen

**SLIDE:** `era-vi` · **DURATION:** 1:05 · **HOLD:** 0:12 *(the gag plays out)* · **VOICE:** Elan

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">1976 · a program stops printing and takes the whole rectangle</Text>

  <!-- vi, editing this very presentation's counter.gooey, driven from
       era/editor.keys. Loop is deliberately OFF: the schedule ends in a
       joke, and a joke on a loop is a bug. -->
  <Border Grid.Row="1" Title="vi era/sample.gooey" Style="island">
    <!-- era/sample.gooey, not counter.gooey: the schedule leaves the
         buffer modified on purpose, and pointing a scripted editor at a
         tracked file is how you find out that `:exit` writes. -->
    <!-- `-n` is not optional, and it is not about performance.
         The deck KILLS its guest when the slide changes, so vim never
         gets to clean up — and vim reads that as a crash and leaves a
         swap file behind. The next visit to this slide then opens on
         `E325: ATTENTION … [O]pen Read-Only, (E)dit anyway …`, the
         schedule's keystrokes answer that prompt instead of editing,
         and the gag silently mangles line 1 rather than appending to
         the end. `-n` means no swap file, so there is nothing to
         recover and no prompt to intercept the script. -->
    <Terminal Cmd="TERM=xterm-256color vi -n era/sample.gooey" Script="era/editor.keys"/>
  </Border>

  <Text Grid.Row="2" Style="dim">same two operations · move the cursor, print a character · now addressing the whole screen at once</Text>

</Grid>
</Gooey>
```

```speak
Then somebody did this.

That is an editor, and it is not printing anything. It has taken the whole
rectangle. There is a cursor it tracks, a file it redraws in place, and a line
at the bottom that is its own little world. And it is doing all of that with the
same two operations we just said were the entire vocabulary — move the cursor,
print a character. Somebody worked out every single position.

That program is from nineteen seventy-six. It is still installed on this
machine. It is, right now, editing a file that is part of this presentation.

And — watch the bottom of that box — it is being driven by the AI. Which is
about to discover the thing every single person watching this has discovered at
least once.

...

Yeah. It can restructure a codebase and it cannot get out of vi.

We'll come back to that.
```

### 1.4 · One idea per program

**SLIDE:** `era-filters` · **DURATION:** 0:50 · **HOLD:** 0:10 · **VOICE:** Elan

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">1980s–90s · small programs, one idea each, piped together</Text>

  <!-- banner and cowsay. Neither is installed on this machine, so both
       are in era/ — thirty lines of shell and forty of awk. That is not
       a workaround, it is the beat: programs of this era were small
       enough that anyone could have written them, and here they are,
       written. -->
  <Border Grid.Row="1" Title="banner · cowsay" Style="island">
    <Terminal Cmd="PS1='$ ' sh -i" Script="era/filters.keys" Loop="true"/>
  </Border>

  <Text Grid.Row="2" Style="dim">both of these were written for this slide · neither was installed · that is how small they are</Text>

</Grid>
</Gooey>
```

```speak
Meanwhile, the other tradition. Small programs. One idea each. Text in one end,
text out the other, and you chain them together with a pipe.

Those two are called banner and cowsay, and they are exactly as serious as they
look. Banner draws big letters. Cowsay puts your words in a speech bubble and
draws a cow under it. That is the whole program.

Here's the part I like. Neither of those was installed on this machine. So
they're not installed — they're written. One is about thirty lines of shell, the
other about forty lines of awk, sitting in a folder next to this presentation.
That's the era in one sentence: a program was small enough that when you wanted
one, you wrote one.

And look at the last thing it does. It pipes the output of the first into the
second. Nobody planned for that. It works because the only thing either of them
agreed on was text.
```

### 1.5 · And then it started showing off

**SLIDE:** `era-scene` · **DURATION:** 0:55 · **HOLD:** 0:14 *(let the effects run)* · **VOICE:** Elan

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">and then people started doing it for fun</Text>

  <!-- examples/scene, hosted. Its own app, its own module, thirty
       frames a second — and inside it exactly one component repaints
       per frame, which is pinned by a damage-count test rather than
       claimed in a comment.

       Build it first:  (cd ../scene && go build -o scene .) -->
  <Border Grid.Row="1" Title="scene · a gooey app, hosted inside a gooey app" Style="island">
    <Terminal Cmd="cd ../scene &amp;&amp; ./scene -fps 24"/>
  </Border>

  <Text Grid.Row="2" Style="dim">every cell is one character · ▀ · top half is the foreground colour, bottom half is the background · click the island to drive it, ctrl+alt+any key to release</Text>

</Grid>
</Gooey>
```

```speak
And then, inevitably, people started doing it for no reason at all.

That is a demo — in the demoscene sense, the tradition of writing something
purely to prove it could be done. And I want to tell you how it works, because
it is the single best trick in here.

Every character cell on that screen is one letter: the upper half block. Just a
rectangle that fills the top half of the cell. The terminal lets you set a
foreground colour and a background colour per cell — so the top half is one
colour and the bottom half is another. That is two pixels per cell, for free, in
a program that thinks it is printing text.

An eighty by twenty-four terminal is an eighty by forty-eight screen. And that
is what you are looking at.

It's also a gooey app, running inside another gooey app, which is the one I'm
presenting from. We'll get to that.
```

### 1.6 · And then it could hear

**SLIDE:** `era-sound` · **DURATION:** 0:55 · **HOLD:** 0:16 *(let the pattern run — it is audible)* · **VOICE:** Elan

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">and it turned out the rectangle could make a noise, and show you the noise</Text>

  <!-- examples/soundboard, hosted. Eight channels summed in Go, one
       stereo stream to the sound server, a sixteen-step sequencer whose
       clock is counted in SAMPLES rather than in frames. The badge in
       its corner is an <Image> from a generated handle: real graphics
       on a terminal with sixel, halfblocks on one without, no branch in
       the program.

       Build it first:  (cd ../soundboard && go build -o soundboard .) -->
  <Border Grid.Row="1" Title="soundboard · a gooey app, hosted" Style="island">
    <Terminal Cmd="cd ../soundboard &amp;&amp; ./soundboard -bpm 124"/>
  </Border>

  <Text Grid.Row="2" Style="dim">the grid is drawn as characters · the waveform under it is drawn as pixels · same frame, chosen by what the data is</Text>

</Grid>
</Gooey>
```

```speak
This is the part I did not expect to be able to show you.

That is a drum machine. Eight channels, a sixteen-step pattern, and what you are
hearing is being mixed in the program — the samples are added together, panned,
and sent to the speaker as one stream. There is no file being played. Every one
of those sounds is about twenty lines of arithmetic, worked out when the program
started.

And look at how it is drawn, because there are two different answers on that
screen at once. The grid of steps is made of characters — it is a table of on
and off, and characters line up. The waveform underneath is made of pixels, the
same half-block trick as the demo. Same frame. Same program. The choice is about
what the data is, not about what the terminal can do.

The little badge in the corner is an image. On a terminal that supports it, that
is real graphics sitting over the text. On one that doesn't, it quietly becomes
half-blocks. The program doesn't know which.
```

### 1.7 · And then it could sing

**SLIDE:** `era-synth` · **DURATION:** 0:50 · **HOLD:** 0:14 · **VOICE:** Elan

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">1999, roughly · the visualiser</Text>

  <!-- examples/synth: a polyphonic instrument and the spectrum
       analyser everyone remembers from a media player. The bars are a
       real FFT of the samples going to the speaker, not an animation
       of one.

       Build it first:  (cd ../synth && go build -o synth .)

       Click the island to capture input, then z through m play;
       ctrl+alt+any key gives the keyboard back to the deck. -->
  <Border Grid.Row="1" Title="synth · click it, then play it" Style="island">
    <Terminal Cmd="cd ../synth &amp;&amp; ./synth"/>
  </Border>

  <Text Grid.Row="2" Style="dim">a 48 kHz audio thread · one property change per frame · the bars and the border are the same tree</Text>

</Grid>
</Gooey>
```

```speak
And of course, if you can draw the sound, somebody is going to draw the sound.

Anyone who was near a computer around 1999 recognises those bars. That is a
spectrum analyser, and it is a real one — it takes the samples on their way to
the speaker, runs a Fourier transform over them, and groups the result into
bands you can actually read. The white caps fall at a fixed speed. That is not
decoration, that is how you read a peak.

Under it, that green line is the actual waveform. Above it is a synthesiser you
can play with the letter keys.

Here is the number I want you to hold on to, because the rest of this
presentation is about it. There is an audio thread in that program running at
forty-eight thousand samples a second. The screen is told about it thirty times
a second, and each time, it is told exactly one thing: something changed. Which
letters on the screen have to be redrawn as a result — that is worked out for
free, by the thing I'm about to explain.
```

### 1.8 · This machine, right now

**SLIDE:** `takeover` · **DURATION:** 0:50 · **HOLD:** 0:05 · **VOICE:** Elan

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Auto,*,Auto like the rest of the chronology, so the process table
     grows into the window instead of stopping at the height it was
     written at. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

<Text Grid.Row="0" Style="body">and all of it is still just a rectangle full of letters</Text>

<Border Grid.Row="1" Title="top" Style="island">
  <!-- The process list gets the star row so the table grows into the
       window. Four Auto rows stopped at the height the slide was
       written at, which is the whole of the sizing complaint in one
       attribute. -->
  <Grid Rows="Auto,Auto,Auto,*" Margin="1,0">
    <Text Grid.Row="0" Style="dim">{{.Sys}}</Text>

    <HStack Grid.Row="1" Gap="3">
      <Gauge Value="{{.Cpu}}" Label="cpu" BarWidth="22"/>
      <Gauge Value="{{.Mem}}" Label="mem" BarWidth="22"/>
    </HStack>

    <Sparkline Grid.Row="2" Values="{{.Load}}" Height="3" Style="island"/>

    <!-- A real list, not a pre-formatted string. Width and HAlign do
         the column alignment, which is the layout engine's job; the
         viewmodel supplies a pid, a name and a size and knows nothing
         about how wide anything is. -->
    <ItemsView Grid.Row="3" Items="{{.Procs}}" Focusable="false">
      <ItemsView.ItemTemplate>
        <HStack Gap="2">
          <Text Style="mono" Width="8">{{.PID}}</Text>
          <Text Style="mono" Width="22">{{.Name}}</Text>
          <Text Style="mono" Width="8">{{.RSS}}</Text>
        </HStack>
      </ItemsView.ItemTemplate>
    </ItemsView>
  </Grid>
</Border>

<Text Grid.Row="2" Style="dim">installers · git tools · anything with panels and a selection — applications, made of letters instead of pixels</Text>

</Grid>
</Gooey>
```

```speak
Which brings us back to now, and to the thing you are actually looking at.

That box is not a picture of a program called top. It is this machine. Those
numbers are being read off the running system, once a second, while I talk. The
processes in that list are the ones running right now — one of them is this
presentation.

And that is the whole point of the tour. Installers with menus you arrow
through. Git tools with panels down the side. Monitors like that one. They are
applications — they have layout, and selection, and things that react when you
press a key.

They are also, still, a rectangle full of letters. Nothing has changed about
what the terminal can do. Everything has changed about what people ask it to do.
```

### 1.9 · Why that's harder than it looks

**SLIDE:** `hard` · **DURATION:** 1:05 · **HOLD:** 0:04 · **VOICE:** Elan

```screen
what the terminal actually knows about:

  buttons        no
  panels         no
  "inside"       no
  layout         no

what you get:

  move the cursor
  print a character
```

```speak
Here's the part that's easy to miss: the terminal has no idea any of that is
happening.

It doesn't know what a button is. It has never heard of a panel. It has no
concept of one thing being inside another thing. The only things you can
actually do are move an invisible writing position around, and print a
character.

That's it. That's the whole instruction set.

So every border you've ever seen in one of those programs — somebody chose those
line characters and worked out where each one goes. Every highlighted row,
somebody computed. Every time the window is resized, somebody has to figure out
what all of it becomes.

And that's before the hard part, which is deciding what to erase and rewrite
when something changes.

gooey is a framework for not doing any of that by hand.
```

### 1.10 · Which way round

**SLIDE:** `quote` · **DURATION:** 0:35 · **HOLD:** 0:05 · **VOICE:** Elan

```screen
"Just a couple of years ago, we would label
 pictures so computers knew what they had in them.

 Now we have computers giving us pictures
 to tell us what our data means."

                        — Elan Hasson, 8/2026
```

```speak
Before we go on, one thing I keep coming back to.

You know, it's funny. Just a couple of years ago, we would label pictures so
computers knew what they had in them. Now we have computers giving us pictures
to tell us what our data means.

That's the whole thing, isn't it. It flipped. We used to do the tedious part so
the machine could see. Now the machine does the tedious part so we can.

Keep that in your head for the next hour, because it's about to happen
literally, on this screen, to me.
```

### 1.11 · Handing over

**SLIDE:** `handover` · **DURATION:** 0:45 · **HOLD:** 0:04 · **VOICE:** Elan

```screen
from here on: Claude

  explains it
  writes it
  runs into the one thing it can't do

my job: run the program.
watch how long that lasts.
```

```speak
So I could take you through the rest of this myself. I'm not going to.

Everything after this point — the explanation, and then the actual building, file
by file, from an empty directory — is Claude. I'm still in the room, and there's
exactly one job left for me, which you'll spot the moment it comes up, because
it's the one thing an AI cannot do on its own.

Watch for when that stops being true. That's the demo.

Go ahead.
```

---

## Part 2 — The one interesting idea

Claude takes the narration. Still no code — this part earns everything Part 3
shows.

### 2.1 · The redraw question

**SLIDE:** `redraw` · **DURATION:** 0:45 · **HOLD:** 0:03 · **VOICE:** Claude

```screen
a screen with a hundred things on it

one number changes

what do you redraw?
```

```speak
Thanks.

So. Picture a screen with a hundred things on it. A list, some panels, a status
bar, a couple of counters. One number changes.

What do you redraw?

That question sounds like a detail. It isn't. How a framework answers it decides
how the whole thing gets built, and it's the reason this exists rather than
being one more library that draws boxes.
```

### 2.2 · The first two answers

**SLIDE:** `answers` · **DURATION:** 1:15 · **HOLD:** 0:04 · **VOICE:** Claude

```screen
one · redraw everything
      simple. flickers. gets slow.

two · redraw everything into memory,
      compare against last time,
      send the differences
      (this is a virtual DOM)

      rebuilds 100 things
      to discover 99 didn't change
```

```speak
The first answer is: redraw everything. Wipe the screen, draw all hundred things
again. It's text, it's cheap, who cares.

And that works, for a while. Until the screen gets big, or the updates come
fast, and then you get flicker, and you get lag, and you can watch the thing
being rebuilt.

So the second answer is smarter. Redraw everything, but into memory rather than
onto the screen. Then compare that fresh copy against what was there a moment
ago, find the differences, and send only those.

That's what most of these frameworks do, and it's a good answer. If it sounds
familiar from the web, it should — it's the same shape as a virtual DOM.

But look at what it's doing. Every single frame, it rebuilds a hundred things in
order to discover that ninety-nine of them didn't change.
```

### 2.3 · The third answer

**SLIDE:** `third` · **DURATION:** 1:10 · **HOLD:** 0:05 · **VOICE:** Claude

```screen
three · know which parts read which data

        one number changes
        → two things on screen ever read it
        → redraw those two

no comparing. nothing was rebuilt.

and you never declare any of it —
reading the value IS the subscription
```

```speak
gooey takes a third route.

It keeps track of which parts of the screen looked at which pieces of data. So
when that one number changes, it already knows — before doing any work at all —
that exactly two things on screen ever read that number. It redraws those two.
It never touches the other ninety-eight, and it never rebuilds them to find out
it didn't need to.

There's no comparing. There's no diffing. There's nothing to compare, because
nothing was rebuilt.

And here's the part I find genuinely elegant: you don't declare any of that.
There's no dependency list to maintain, no "this depends on that", no call to
say "I changed something, please update".

You read the value while you're drawing. And the reading is the subscription.
That's the whole mechanism.
```

### 2.4 · Why one list beats two

**SLIDE:** `onelist` · **DURATION:** 0:45 · **HOLD:** 0:03 · **VOICE:** Claude

```screen
most systems keep two lists:

  what the code actually reads
  what you told the framework it reads

they drift.

  forgot one   → silently stops updating
  one too many → redraws for nothing

here there is one list,
and it is produced by running the code
```

```speak
The reason that's worth building a framework around is that the two lists can
never disagree.

In most systems there's what your code actually depends on, and then there's
what you told the framework it depends on. Those drift apart. You forget an
entry and something silently stops updating. You add one too many and things
redraw for no reason.

Here there's only one list, and it's produced by running the code. It cannot be
out of date, because it's rebuilt by the act of drawing.
```

### 2.5 · The layout is a file

**SLIDE:** `markup` · **DURATION:** 0:55 · **HOLD:** 0:04 · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- A Grid, not a VStack, because the middle has to EAT the window.
     Rows="Auto,*,Auto" gives the two captions exactly their line and
     hands everything else to the panes, so this slide is the size of
     whatever terminal it is shown in rather than the size it was
     written at. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">the layout isn't in the code. it's a file. and it's open on the left.</Text>

  <!-- The same file, twice. Left is a real vim editing counter.gooey.
       Right is <Counter/>, which the Include seam resolves to that same
       file by convention. There is no second copy to keep in step — if
       the right pane is wrong, the left pane is what made it wrong, and
       you are looking at the cursor that made it wrong.

       Cols="*,*" is what makes both panes half the window and keeps
       them half the window through a resize. Neither pane has a size in
       it anywhere. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="counter.gooey — vim" Style="island">
      <!-- No Cols/Rows: the Terminal fills its half and tells the guest,
           so vim gets a TIOCSWINSZ and a SIGWINCH and redraws itself
           into the new shape. TERM is set here rather than inherited
           because the deck may itself be running under something the
           guest would refuse. -->
      <Terminal Name="Vim" Cmd="TERM=xterm-256color vim -n -u NONE -N counter.gooey"/>
    </Border>

    <!-- Named, and kept identical to live.gooey, because this is what
         gets rebuilt when vim saves. Patching the Stage would work and
         would also kill the editor beside it. -->
    <VStack Name="Live" Grid.Col="1">
      <Counter Label="…and that file, running"
               Count="{{.Count}}"
               Bump="{{.Bump}}"/>
    </VStack>

  </Grid>

  <Text Grid.Row="2" Style="dim">click the editor to capture input · edit and :w · the right pane rebuilds from the file within a second · ctrl+alt+any key gives it back</Text>

</Grid>
</Gooey>
```

```speak
One more idea before anything gets built.

You don't write the layout in code. You write it in a separate file that
describes what goes where — a panel, some text inside it, a line under that. If
you've seen HTML, it will look familiar.

That's it on the left. And on the right is the same file — not a picture of what
it would look like, and not a copy I kept in step by hand. The right pane is
that exact file, loaded and running.

And look at what isn't in it. There's no instruction to update, nothing that
watches anything, nothing that redraws on a schedule. It's a description of a
panel with a number in it. The number is changing because that line reads it,
and reading it is the only thing that has to happen.

And that file is live. You can edit it while the program is running, save it,
and watch the screen change. No restart. No rebuild. Whatever the program was in
the middle of doing, it keeps doing.

That isn't a development convenience bolted on the side. It's how the app loads
its interface in the first place. Which matters a great deal in about ten
minutes, for reasons I'm not going to spoil.
```

---

## Part 3 — Building it, from nothing

Code appears. Claude writes every file; Elan runs them, because Claude can't.

### 3.1 · The one thing I can't do

**SLIDE:** `cant` · **DURATION:** 0:50 · **HOLD:** 0:03 · **VOICE:** Claude

```screen
I can write the files.

I cannot run a program
on a screen you can see.

  Claude → writes
  Elan   → runs

remember this
```

```speak
Before we start, the thing Elan told you to watch for.

I can write files. I'm going to write every line of what follows. What I cannot
do is run a program on a screen that any of you can see — I don't have a
terminal, I have no window, and the app we're about to build has to appear
somewhere real.

So for this next part, Elan is my hands. I write, he runs, he tells me what
happened.

It's a real limitation and I'd rather show it than route around it. Keep an eye
on it.
```

### 3.2 · Nothing

**SLIDE:** `(live)` · **DURATION:** 0:50 · **HOLD:** 0:08 *(app launches, black screen)* · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Auto,*,Auto: the two captions get their line and the island gets
     the rest, however much that is. The island had a Cols="88"
     Rows="14" on it, which made this slide a fixed rectangle in the
     middle of a window that could be any size — the guest never learned
     the window had changed, because nothing had a size to tell it. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">sixteen lines. no user interface in any of them. the numbers are on the left — count them.</Text>

  <!-- Two islands, because the beat makes two claims and used to show
       only one of them. It says "sixteen lines" and "here's the whole
       program" while the audience looked at a black rectangle and took
       both on faith.

       LEFT is the source, in a real vim on the real file — no swapfile
       (-n), no vimrc (-u NONE). `set nu` is the load-bearing flag: line
       numbers are what turn "sixteen lines" from a claim into something
       anyone in the room can check, and the file is exactly sixteen
       lines long.

       It is WRITABLE, and that is the beat. Edit it, save, and the
       right-hand pane rebuilds and restarts in front of you.

       RIGHT is watchgo.sh: `go build`, shown, then the binary it just
       produced. Not a screenshot and not a cue card — a child process
       on a pty, its output modelled by render.Screen, its cells blitted
       into this slide. It fills its column and follows it; resize the
       window and the guest gets a SIGWINCH.

       Showing the compile is the whole reason this pane is not just
       `../intro/intro`. Beat 3.5 edits MARKUP and nothing restarts —
       the running tree picks the file up and the count survives. Here
       the source is Go, so there is a compiler in the way and the
       process has to die. Watching the build run is what makes that
       difference something the room SEES rather than something I
       assert two slides later. A broken edit is worth doing on purpose
       once: the compiler's error lands in the pane, and the app that
       was running is gone until it builds.

       The one-second pause after `ok` is deliberate — without it the
       compile is a single frame nobody can read.

       This edits a TRACKED file. That is the same bargain beat 3.5
       makes with counter.gooey, and the same reset:

           git checkout examples/intro/main.go

       Nothing needs prebuilding here — watchgo.sh builds on entry — but
       `examples/intro` is in the ROOT module and this deck is not, so
       the build has to run in that directory rather than through
       `go run ../intro`, which cannot cross the boundary. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="examples/intro/main.go — all of it" Style="island">
      <Terminal Name="Source" Cmd="TERM=xterm-256color vim -n -u NONE -N --cmd 'set shortmess+=F' -c 'set nu' ../intro/main.go"/>
    </Border>

    <Border Grid.Col="1" Title="go build · then run · on every save" Style="island">
      <Terminal Name="App" Cmd="./watchgo.sh ../intro main.go intro"/>
    </Border>

  </Grid>

  <Text Grid.Row="2" Style="dim">click the editor to capture input · edit and :w · the right pane recompiles and restarts · ctrl+alt+any key gives it back</Text>

</Grid>
</Gooey>
```

```speak
Here's the whole program. Sixteen lines. And if you read them looking for the
user interface, there isn't one — it makes an app with an empty tree, and runs
it.

Elan, run it.
```

### 3.3 · What the blank screen already did

**SLIDE:** `act0` · **DURATION:** 1:10 · **HOLD:** 0:05 *(quit; show intact scrollback)* · **VOICE:** Claude

```screen
16 lines bought:

  the whole window          (a mode you must ask for)
  cursor hidden
  keys, mouse, resize       (listening)
  every keystroke arrives   (no waiting for enter)

and on exit:

  everything back exactly as it was
```

```speak
Black. Which is correct — nothing was asked for.

But quite a lot just happened. The program took the entire window, which is a
mode the terminal has to be explicitly put into. It hid the cursor. It started
listening for keys, and for the mouse, and for the window being resized. And it
put itself in the state where every keystroke arrives immediately instead of
waiting for you to press enter.

Now quit it.

Everything's back. The prompt is where it was, the scroll history is intact, the
terminal is in the state it was in before. That sounds like table stakes right
up until you've used a program that got it wrong and left your terminal
unusable.

Sixteen lines. That's the floor.
```

### 3.4 · A tree

**SLIDE:** `(live)` · **DURATION:** 1:00 · **HOLD:** 0:06 · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">nine lines of layout, and the thing those nine lines describe.</Text>

  <!-- The file's text on the left, the file's meaning on the right.
       {{.CounterSource}} is the bytes of counter.gooey, re-read on the
       tick, so this pane cannot drift from the pane beside it — and
       <Counter/> is that same path resolved through Includes. Nothing
       here is a transcription. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="counter.gooey — the file" Style="island">
      <Text Style="mono" Margin="1,0">{{.CounterSource}}</Text>
    </Border>

    <VStack Name="Live" Grid.Col="1">
      <Counter Label="…and that file, running"
               Count="{{.Count}}"
               Bump="{{.Bump}}"/>
    </VStack>

  </Grid>

  <Text Grid.Row="2" Style="dim">the left pane is the bytes on disk · the right pane is that same path, loaded and running</Text>

</Grid>
</Gooey>
```

```speak
Now the interface. I'm writing a second file — nine lines.

A border with a title. A stack of things inside it, with a gap between them. Two
pieces of text.

Two details worth pointing at. That style name: the markup doesn't say what
colour to use, it says a name, and the program decides what the name means. So
appearance lives in one place instead of being sprinkled through the layout.

And the last line binds the letter Q to quitting. Notice where it is — the key
binding sits inside the thing it belongs to, rather than in a table of shortcuts
at the top of the program.

Run it.
```

### 3.5 · Editing it while it runs

**SLIDE:** `(live)` · **DURATION:** 0:55 · **HOLD:** 0:10 *(Claude edits the file; save; screen changes)* · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">same file, now open in a real editor. edit it. save it. don't restart anything.</Text>

  <!-- Identical to beat 2.5's pair, deliberately. The editor is a real
       vim on the real file, and the right-hand pane is that same file
       resolved through the Include seam — so there is no second copy to
       keep in step, and a save reaches the pane by way of the disk.

       The right pane is Name="Live" because that is the name
       syncCounter re-patches when the file changes. Patching the whole
       Stage would also work, and would also kill the editor next to
       it. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="counter.gooey — vim" Style="island">
      <Terminal Name="Vim" Cmd="TERM=xterm-256color vim -n -u NONE -N counter.gooey"/>
    </Border>

    <VStack Name="Live" Grid.Col="1">
      <Counter Label="…and that file, running"
               Count="{{.Count}}"
               Bump="{{.Bump}}"/>
    </VStack>

  </Grid>

  <Text Grid.Row="2" Style="dim">click the editor to capture input · edit and :w · the right pane rebuilds from the file within a second · ctrl+alt+any key gives it back</Text>

</Grid>
</Gooey>
```

```speak
Leave it running. Don't touch it.

I'm going to change that file while the program is running. Not restart it, not
rebuild it. Change the text, save.

There. And now something that wasn't there at all — a whole new line, appearing
in a program that never stopped.

That's not a reload the way a web page reloads. Nothing was thrown away and
recreated. And the reason it works is the next thing we add.
```

### 3.6 · State

**SLIDE:** `(live)` · **DURATION:** 1:05 · **HOLD:** 0:06 · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">a number that changes, and a sentence that was never told about it. its definition is on the right.</Text>

  <!-- The running thing and its own source, side by side. The beat's
       claim is about what is NOT in the markup — "nobody ever updates
       that sentence" — and that is only worth saying if the room can
       read the definition and find no update in it. Written inline, as
       this slide used to be, the audience had to take my word for it.

       The editor is on the RIGHT here, and on the left on beat 3.5.
       That is deliberate: 3.5 is about the file driving the program, so
       the file leads; this beat is about the program and then where it
       came from, so the program leads.

       READ-ONLY (-R), and not an oversight. Beat 3.5 one slide ago is
       the edit-markup-live demo and owns it; doing the same trick twice
       running dilutes both. This pane is a viewer, and -R is what stops
       a stray keystroke turning it into a broken edit nobody meant to
       make. If it should ever become editable, it needs a syncState
       twin of deck.go's syncCounter — the file is not watched.

       No second copy: the left is state.gooey resolved through the
       Include seam — <State/> finds it by convention (view.go:34), the
       same way <Counter/> finds counter.gooey — and the right is vim on
       that same path. Count and Bump arrive as attributes.

       state.gooey carries NO comment, which is a rule and not an
       omission: counter.gooey has none either, and the two are the only
       files in this deck a room ever reads. The first draft of
       state.gooey explained itself in sixteen lines, and on screen that
       put the whole point — {{.Count}} on lines 20 and 24 — below the
       fold, behind prose about slide construction that the audience has
       no reason to read. A file that gets shown is a slide. Every file
       here that is NOT shown (live.gooey, stage.gooey) is commented at
       length; the reasoning for these two lives in the beat instead. -->
  <Grid Grid.Row="1" Cols="*,*">

    <VStack Grid.Col="0">
      <State Count="{{.Count}}" Bump="{{.Bump}}"/>
    </VStack>

    <Border Grid.Col="1" Title="state.gooey — the thing on the left, defined" Style="island">
      <Terminal Name="Source" Cmd="TERM=xterm-256color vim -n -u NONE -N -R --cmd 'set shortmess+=F' -c 'set nu' state.gooey"/>
    </Border>

  </Grid>

  <Text Grid.Row="2" Style="dim">click +1 · the sentence follows because it read the number · read the right pane and find the line that updates it — there isn't one</Text>

</Grid>
</Gooey>
```

```speak
So far the screen has been a fixed picture. Let's give it something that
changes.

I'm adding a counter, and a button that increases it. And then a second piece of
text that isn't stored anywhere — it's defined as a sentence built out of the
counter. Nobody ever updates that sentence. It's a rule, not a value.

Elan, click it.

The number goes up, and the sentence follows, because the sentence read the
number, and that is all it takes.
```

### 3.7 · The receipt

**SLIDE:** `(live)` · **DURATION:** 1:00 · **HOLD:** 0:08 *(click several times; counter stays at 1)* · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">the claim was: only what read the number gets redrawn. here is the receipt.</Text>

  <!-- Same app, one line more. {{.Painted}} is App.PaintedLastFrame for
       the frame the click caused — the framework's own damage count,
       published once per click by an AfterFrame hook, not a number this
       deck computed to agree with itself. -->
  <Border Grid.Row="1" Title="the receipt" Style="island">
    <VStack Gap="1" Margin="4,2">

      <Text Style="headline">count {{.Count}}</Text>

      <Button Content="+1" Click="{{.Bump}}"/>

      <Text Style="body">nobody ever updates this sentence. it says {{.Count}}
because it reads {{.Count}}.</Text>

      <Text Style="hot">components repainted by that click: {{.Painted}}</Text>

      <Text Style="dim">there are six things in this panel. the border did not
repaint. the button did not repaint. nothing compared
anything against anything to work that out.</Text>

    </VStack>
  </Border>

  <Text Grid.Row="2" Style="dim">click +1 again · the number below is the framework's own damage count for that frame</Text>

</Grid>
</Gooey>
```

```speak
Now — I made a claim a few minutes ago. Claims like that are easy to make and
hard to see, so here's the receipt.

That number in the corner is how many things on screen were redrawn in the last
frame. Not how many exist. How many were actually redrawn.

Click it again.

Two.

There are half a dozen things on that screen. Two of them read the counter — the
line with the number in it, and the sentence underneath — so two of them were
redrawn. The border wasn't. The button you just clicked wasn't. The paragraph at
the bottom wasn't touched, and nothing compared it against anything to work that
out.

And nowhere in what I wrote did I say which parts depend on the counter.
```

### 3.8 · Where the state actually lives

**SLIDE:** `(live)` · **DURATION:** 0:50 · **HOLD:** 0:08 *(click to 7, then Claude rewrites the layout)* · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">click it up to seven, then watch the layout get replaced underneath it.</Text>

  <!-- Identical to beat 2.5's pair, deliberately. The editor is a real
       vim on the real file, and the right-hand pane is that same file
       resolved through the Include seam — so there is no second copy to
       keep in step, and a save reaches the pane by way of the disk.

       The right pane is Name="Live" because that is the name
       syncCounter re-patches when the file changes. Patching the whole
       Stage would also work, and would also kill the editor next to
       it. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="counter.gooey — vim" Style="island">
      <Terminal Name="Vim" Cmd="TERM=xterm-256color vim -n -u NONE -N counter.gooey"/>
    </Border>

    <VStack Name="Live" Grid.Col="1">
      <Counter Label="…and that file, running"
               Count="{{.Count}}"
               Bump="{{.Bump}}"/>
    </VStack>

  </Grid>

  <Text Grid.Row="2" Style="dim">the count lives in the program · the file only points at it · rewrite the file and the count does not move</Text>

</Grid>
</Gooey>
```

```speak
Which brings back the live editing, with the piece that was missing.

Click it up to seven. Now I'll rewrite the layout underneath it — move things
around, change the wording — and save.

Seven.

The layout was replaced. The state wasn't, because the state was never in the
layout. The number lives in the program; the interface is just something
currently pointed at it. You can swap the interface out from under a running
program and it doesn't lose its place.

Hold onto that. It's about to stop being a convenience.
```

---

## Part 4 — The turn

### 4.1 · Six lines

**SLIDE:** `(live)` · **DURATION:** 0:45 · **HOLD:** 0:05 · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">the six lines. this is the whole diff between examples/intro and examples/introtarget.</Text>

  <!-- Written out rather than included, because this slide is about
       reading the lines. They are the real ones: examples/introtarget/
       main.go is examples/intro/main.go plus exactly this. -->
  <Border Grid.Row="1" Title="examples/introtarget/main.go — the addition" Style="island">
    <VStack Gap="1" Margin="4,2">

      <Text Style="mono">ctx := &amp;markup.Context{Values: map[string]any{}}

app := gooey.NewApp(gooey.Tree(&amp;components.Text{}))
_ = control.NewService(app, ctx)

srv, err := mcp.Serve(app, mcp.Options{Addr: *addr, Context: ctx})
defer srv.Close()</Text>

      <Text Style="dim">no property is declared here. no markup is declared here.
nothing about how it draws is different. the tree is still
one empty Text.</Text>

    </VStack>
  </Border>

  <Text Grid.Row="2" Style="dim">nothing else changes · the next four beats are what those six lines make possible</Text>

</Grid>
</Gooey>
```

```speak
One more addition. Six lines.

Nothing else changes. Not the layout, not the state, not one thing about how it
draws.

Those six lines open a door.
```

### 4.2 · What's on the other side

**SLIDE:** `mcp` · **DURATION:** 1:00 · **HOLD:** 0:04 · **VOICE:** Claude

```screen
MCP — Model Context Protocol

a standard way for a program to say
what it can do, in a form a model can act on

  not screen scraping
  not simulated clicks

an actual list of operations,
with names, and what each one takes
```

```speak
A word on what's through it, because it has an acronym and the acronym is
load-bearing.

MCP stands for Model Context Protocol. It's a standard way for a program to
describe what it can do, in a form a model like me can read and then act on. Not
scraping a screen. Not pretending to be a person clicking. An actual list of
operations, with names, and what each one expects.

That's all it is. A program says here is what I can do, and something on the
other end does those things.

So those six lines mean I can open this app the way you'd open a file.
```

### 4.3 · The thing nobody wrote

**SLIDE:** `tools` · **DURATION:** 1:20 · **HOLD:** 0:06 · **VOICE:** Claude

```screen
13 operations

  read the tree          press keys
  read the screen        click
  list every value       move focus
  list every style       set a value
                         run a command
  register new state
  replace the markup     check markup for errors
                         (without applying it)

nobody wrote any of this
```

```speak
Here's what's on the other side. Thirteen operations.

Read the component tree. Read the screen as text. List every piece of state and
every style name. Press keys. Click. Move focus. Change a value. Run a command.
Register new state. Replace the markup — a piece of it, or all of it. And check
markup for mistakes without applying it.

Now the part I want to be exact about, because it's the actual claim.

Nobody wrote any of that.

There is no automation layer in this program. No scripting interface. No list of
what an agent may touch. Nobody annotated a single element. I certainly didn't,
and I wrote the whole thing.

Those six lines exposed what was already there. The names come from the layout
file, where they were written for a human's benefit. The state is the same state
the buttons use. The commands are the same commands the keyboard runs.

The automation surface and the interface aren't two things kept in sync. They're
one thing, looked at from two directions.
```

### 4.4 · Why that follows

**SLIDE:** `tools` · **DURATION:** 0:55 · **HOLD:** 0:03 · **VOICE:** Claude

```screen
it isn't luck.

to redraw the right things,
the framework already has to hold
a live, structured description
of the whole interface

handing that to something else
isn't a feature that got built —
it's a door in a wall already standing
```

```speak
And that isn't luck. It's the same idea from Part Two, arriving somewhere else.

Because the framework already has to know what every part of the screen reads,
in order to redraw the right things, it already holds a complete, live,
structured description of the interface. It has to. That's how it works.

Handing that description to something else was not a feature that had to be
built. It's a door in a wall that was already standing.

Which means it can't fall out of date, for the same reason the redraw list
can't. There is nothing to keep in sync.
```

### 4.5 · I don't need him anymore

**SLIDE:** `alone` · **DURATION:** 0:50 · **HOLD:** 0:06 · **VOICE:** Claude

```screen
the thing to watch for:

  Elan → runs the program        ← still true
  Elan → does anything in it     ← no longer true

the app is running.
it is empty, on purpose.
nothing restarts from here.
```

```speak
And this is the thing Elan told you to watch for, forty minutes ago.

Up to now I've written files and asked him to run them and tell me what he saw.
That was real. I wasn't being polite about it.

He still has to have started the program — I can't do that. But from this
moment, I don't need him inside it. I can see the screen, because I can ask. I
can change what's on it, because there's a door.

The app is running right now, and it's empty. Deliberately. Nothing restarts
from here.
```

### 4.6 · Building it from outside

**SLIDE:** `(live)` · **DURATION:** 1:15 · **HOLD:** 0:10 *(register properties, then swap markup)* · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">one program with no interface, and one shell. everything that appears on the left arrives through the door on the right.</Text>

  <!-- Two processes, on camera, both real.

       LEFT is examples/introtarget: examples/intro's empty tree plus
       control.NewService and mcp.Serve, and nothing else. It declares no
       properties and no markup, so everything that appears in it during
       Part 4 was registered from outside, into a process compiled
       knowing none of it.

       RIGHT is a shell, so the calls are visible as calls rather than as
       a cut to a result. curl against 127.0.0.1:7900 is the whole
       surface — there is no second, private door.

       These three beats share one identical fence on purpose: Restage
       skips a patch whose source has not changed, so the target app and
       the shell survive 4.6 → 4.7 → 4.8 instead of being killed and
       relaunched under the narration that is still talking about
       them. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="introtarget — an app with nothing in it" Style="island">
      <Terminal Cmd="../introtarget/introtarget -mcp 127.0.0.1:7900"/>
    </Border>

    <Border Grid.Col="1" Title="the door · MCP on 127.0.0.1:7900" Style="island">
      <Terminal Cmd="TERM=xterm-256color sh -i"/>
    </Border>

  </Grid>

  <Text Grid.Row="2" Style="dim">click a pane to capture input · ctrl+alt+any key to release · the left process was compiled knowing nothing about any of this</Text>

</Grid>
</Gooey>
```

```speak
First: what's on the screen. I get it back as text — the same rectangle of
characters you're looking at.

Second: what state exists, and what I'm allowed to touch. That one matters. I
can't reach past it. Everything I do from here is inside what the app chose to
expose, and if it hadn't opened the door, I'd have nothing.

Right. There's nothing here, so I need somewhere to put things.

I'm adding state to the running process. A slide number, a title, a body. A
second ago this program had no concept of a slide, and now it does.

And now the layout — the same kind of file you watched me write earlier, handed
over as text.

That's a new interface, in a program that never stopped, that was never built
with any of this in mind.
```

### 4.7 · Getting told no

**SLIDE:** `(live)` · **DURATION:** 1:05 · **HOLD:** 0:04 · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">one program with no interface, and one shell. everything that appears on the left arrives through the door on the right.</Text>

  <!-- Two processes, on camera, both real.

       LEFT is examples/introtarget: examples/intro's empty tree plus
       control.NewService and mcp.Serve, and nothing else. It declares no
       properties and no markup, so everything that appears in it during
       Part 4 was registered from outside, into a process compiled
       knowing none of it.

       RIGHT is a shell, so the calls are visible as calls rather than as
       a cut to a result. curl against 127.0.0.1:7900 is the whole
       surface — there is no second, private door.

       These three beats share one identical fence on purpose: Restage
       skips a patch whose source has not changed, so the target app and
       the shell survive 4.6 → 4.7 → 4.8 instead of being killed and
       relaunched under the narration that is still talking about
       them. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="introtarget — an app with nothing in it" Style="island">
      <Terminal Cmd="../introtarget/introtarget -mcp 127.0.0.1:7900"/>
    </Border>

    <Border Grid.Col="1" Title="the door · MCP on 127.0.0.1:7900" Style="island">
      <Terminal Cmd="TERM=xterm-256color sh -i"/>
    </Border>

  </Grid>

  <Text Grid.Row="2" Style="dim">click a pane to capture input · ctrl+alt+any key to release · the left process was compiled knowing nothing about any of this</Text>

</Grid>
</Gooey>
```

```speak
Let me do something wrong deliberately, because how a system fails tells you
more than how it succeeds.

Here's markup with a mistake in it — an element that doesn't exist, bound to
something that isn't there.

Refused. Before touching the app. It told me the line, and the name, and what
was wrong with it, and the running program never saw it.

That's the same rule the human side gets. If Elan had saved a broken layout file
back in Part Three, it would have been caught the same way, at the same moment,
with the same message. Nothing here is allowed to fail three clicks later in
front of a user if it could have been caught when the file was read.

And I'd rather be told no like that than be trusted more than I've earned.
```

### 4.8 · Two of us in the room

**SLIDE:** `(live)` · **DURATION:** 0:55 · **HOLD:** 0:12 *(Elan types into the app)* · **VOICE:** Claude

```gooey
<Gooey xmlns="wonderforge.io/gooey/2026">
<!-- Rows="Auto,*,Auto": the captions get their line, the panes get the
     rest of the window, whatever the window turns out to be. -->
<Grid Name="Stage" Grid.Row="2" Rows="Auto,*,Auto">

  <Text Grid.Row="0" Style="body">one program with no interface, and one shell. everything that appears on the left arrives through the door on the right.</Text>

  <!-- Two processes, on camera, both real.

       LEFT is examples/introtarget: examples/intro's empty tree plus
       control.NewService and mcp.Serve, and nothing else. It declares no
       properties and no markup, so everything that appears in it during
       Part 4 was registered from outside, into a process compiled
       knowing none of it.

       RIGHT is a shell, so the calls are visible as calls rather than as
       a cut to a result. curl against 127.0.0.1:7900 is the whole
       surface — there is no second, private door.

       These three beats share one identical fence on purpose: Restage
       skips a patch whose source has not changed, so the target app and
       the shell survive 4.6 → 4.7 → 4.8 instead of being killed and
       relaunched under the narration that is still talking about
       them. -->
  <Grid Grid.Row="1" Cols="*,*">

    <Border Grid.Col="0" Title="introtarget — an app with nothing in it" Style="island">
      <Terminal Cmd="../introtarget/introtarget -mcp 127.0.0.1:7900"/>
    </Border>

    <Border Grid.Col="1" Title="the door · MCP on 127.0.0.1:7900" Style="island">
      <Terminal Cmd="TERM=xterm-256color sh -i"/>
    </Border>

  </Grid>

  <Text Grid.Row="2" Style="dim">click a pane to capture input · ctrl+alt+any key to release · the left process was compiled knowing nothing about any of this</Text>

</Grid>
</Gooey>
```

```speak
One last thing, and I need Elan back for it.

I haven't taken the app away from him. There's no agent mode. Nothing's locked.
He can still use it, because as far as the program is concerned nothing unusual
is going on.

Go ahead — type something.

I can see that. Not because I'm watching a screen, but because I asked what
changed, and the answer includes his typing exactly the way it would include
mine.

Same state. Same door. Two of us in the same room, and the program doesn't have
an opinion about which of us is which.
```

---

## Part 5 — Close

### 5.1 · Putting it back together

**SLIDE:** `recap` · **DURATION:** 1:10 · **VOICE:** Claude

```screen
a terminal is a rectangle you print into

  → what do you redraw when something changes?
  → notice what read what
  → so nothing is rebuilt to discover it needn't be

  → which means the framework holds a live,
    structured description of the interface

  → which means it was already readable

  → six lines, no new concepts
```

```speak
So, back together.

A terminal is a rectangle you can print characters into. A framework that wants
to build interfaces there has to decide what to redraw when something changes,
and the answer here is to notice what read what — so that nothing is ever
rebuilt in order to discover it didn't need to be.

That decision forced the framework to hold a live, structured description of the
interface. Which meant the interface was already readable. Which meant opening
it to something else took six lines and no new concepts.

And then I wrote a working interface into a program that was already running,
using the same doors a person uses, and got told off when I made a mistake.
```

### 5.2 · Which way round, again

**SLIDE:** `quote` · **DURATION:** 0:50 · **HOLD:** 0:04 · **VOICE:** Claude

```screen
"Just a couple of years ago, we would label
 pictures so computers knew what they had in them.

 Now we have computers giving us pictures
 to tell us what our data means."

                        — Elan Hasson, 8/2026
```

```speak
Elan said something at the start that I'd like to hand back to him.

That we used to label pictures so computers could tell what was in them, and now
computers make the pictures that tell us what our data means.

Watch what actually happened here. He described a rectangle to you, and then
handed the labelling to me — the layout, the wiring, the file after file of it.
And at the end I made him a picture. This one. In a program that was still
running.

I'd add one line to his. The reason I could do that isn't that I'm clever about
terminals. It's that somebody built the thing so that what it draws and what it
can be asked about are the same object. That's a choice. Frameworks that didn't
make it can't be handed over like this, no matter how good the model gets.
```

### 5.3 · The last line

**SLIDE:** `end` · **DURATION:** 0:35 · **VOICE:** Claude

```screen
~100 lines of Go
never restarted once

gooey

github.com/WonderForgeLabs/gooey
```

```speak
It's about a hundred lines of Go, and it never restarted once.

It's called gooey. It's on GitHub. Go and break it.
```

---

## Production notes

**Beats that must not be faked.** 3.7, 3.8, 4.3, 4.7 and 4.8 each make a claim
the screen has to actually back up — the repaint count, the state surviving a
layout swap, the tool list, the refusal, Elan's typing coming back. If a take
doesn't show it, re-shoot rather than narrate around it.

**The holds are the demo.** Dead air after 3.5, 3.8, 4.6 and 4.8 is where the
described thing happens. Don't compress them to tighten the runtime; the talking
only exists to frame them.

**Side-by-side.** From 4.5 onward the frame should be split: tool calls on one
side, the app on the other. Before 4.5 it's a single terminal — the split
arriving *is* the handover, and it should not appear early.

**Numbers asserted out loud.** "Sixteen lines" (3.2, 3.3), "nine lines" (3.4),
"thirteen operations" (4.3), "about a hundred lines" (5.2). All are visible on
screen and all are trivially falsifiable. Re-count against the final code and
fix the script, not the code.

**One-line honesty check.** Beat 3.1 promises the audience a limitation, and
4.5 collects on it. If the shoot ends up with Elan driving anything after 4.5,
either cut the promise or cut the claim — do not keep both.
