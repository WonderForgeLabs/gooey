# Tutorial 7: Add app chrome — menu, status bar, toasts, and tips

In this tutorial you take a small working page and dress it, one layer
at a time: a **MenuBar** you can open with `alt+letter` from anywhere, a
**StatusBar** with live bound sections, a **Toast** popped from a
command, and **Tooltips** that appear when the pointer rests. Along the
way you meet more of the toolkit — `ProgressBar`, `Spinner`,
`Sparkline`, `Toggle`, `Segmented`, `ButtonBar` — as the content the
chrome dresses. (`Tabs` and the validation components are the other two
toolkit families; they have their own homes in
[markup-reference.md](../markup-reference.md#tabs) and
[how-to: validate a form](howto/howto-forms.md).)

**Time:** about 30 minutes.
**Prerequisites:** [Tutorial 4](04-input-commands.md) (commands, key
bindings, focus). [Tutorial 3](03-binding-and-state.md) explains why
the bindings below repaint what they repaint.

When you finish, you will have a fake download with a menu over it, a
status row under it, and notifications on top of everything.

*(GIF: docs-and-demos workflow.)*

The finished code is in
[`docs/learn/examples/07-app-chrome`](examples/07-app-chrome). Run it
from its own directory:

```sh
cd docs/learn/examples/07-app-chrome && go run .
```

## Step 1: Start with content worth dressing

The base page is nothing you have not built already: a viewmodel of
sources (`Pct`, `Running`, `Status`, a `Rates` history), two `<Timer>`s
driving it, and a `Grid` of rows:

```xml
<Grid Rows="Auto,Auto,Auto,Auto,Auto,Auto,Auto,*,Auto">
  <Text Grid.Row="1" Style="accent">a download, wearing its chrome</Text>

  <HStack Grid.Row="2" Gap="3">
    <ProgressBar Value="{{.Pct}}" Label="fetch " BarWidth="30"/>
    <Spinner Frames="braille" Interval="90ms" Label="{{.Status}}" Enabled="{{.Running}}" Style="accent"/>
  </HStack>

  <HStack Grid.Row="3" Gap="2">
    <Text Style="dim">rate</Text>
    <Sparkline Values="{{.Rates}}" Height="2" BarWidth="36"/>
  </HStack>

  <HStack Grid.Row="4" Gap="4">
    <Toggle Checked="{{.Running}}" Label="downloading" Changed="{{.RunChanged}}"/>
    <Segmented Options="Slow | Normal | Turbo" Selected="{{.Speed}}" Changed="{{.SpeedChanged}}"/>
  </HStack>

  <ButtonBar Grid.Row="5" Gap="3" Separator="│">
    <Button Content="restart" Click="{{.Start}}" Tooltip="start again from zero"/>
    <Button Content="toast" Click="{{.Notify}}">
      <Tooltip Text="pop the status as a toast" Gesture="ctrl+t"/>
    </Button>
  </ButtonBar>
  ...
</Grid>
```

Step 6 tours these widgets properly. For now, one thing to notice:
`Running` is a single bool that the `Toggle` sets, the `Spinner` reads
while painting, and the timer's tick handler checks. Flip the rocker
and the spinner parks — no code connects them, the graph does. That is
tutorial 3's rule paying rent.

The rest of this tutorial adds chrome, and every piece of chrome is an
**overlay**: something that paints *above* the content. The recipe is
operational and short:

> **Declare overlay elements wherever they belong.** An overlay is
> lifted out of document order into a paint layer of its own, so it
> paints above the page from anywhere; within that layer a rank decides,
> so a toast is never hidden by an open menu. In a `Grid`, `Grid.Row`
> still places the element wherever it belongs.

**"Declare overlay elements LAST" is what this box used to say**, and
you will still find the shape in older apps — including this tutorial's
own example, where the overlays are at the end of the `Grid` because
that is where they read best. It is harmless and no longer load-bearing:
position stopped deciding paint in
[#437](https://github.com/WonderForgeLabs/gooey/issues/437) and stopped
deciding order *among* overlays in
[#439](https://github.com/WonderForgeLabs/gooey/issues/439). Where it is
still load-bearing is **hit-testing**, which is not lifted — so an
overlay that wants presses (none of the built-in ones do) still cares.

How the layer works — and what happens when an overlay is dismissed and
the cells under it come back — is the subject of
[concepts/overlays.md](concepts/overlays.md). Here we just use it.

## Step 2: Add a MenuBar

Append this to the `Grid` — after the content, after the timers and key
bindings — and it becomes the top row of the app:

```xml
<MenuBar Grid.Row="0" Style="accent">
  <Menu Title="_Job">
    <MenuItem Text="Re_start" Gesture="ctrl+s" Command="{{.Start}}"/>
    <MenuItem Separator="true"/>
    <MenuItem Text="_Quit" Gesture="q" Command="{{.Quit}}"/>
  </Menu>
  <Menu Title="_Notify">
    <MenuItem Text="_Toast the status" Gesture="ctrl+t" Command="{{.Notify}}"/>
  </Menu>
</MenuBar>
```

`<Menu>` and `<MenuItem>` are **data, not components** — like a Grid's
track list, they declare the bar's contents and never enter the visual
tree. The dropdown that appears below an open title paints over the
progress row and the sparkline, and **document order is not what does
it**: the popup surface implements `gooey.Overlay`, so the Composer
lifts it out of the ordinary layer entirely and paints it above the
page wherever the bar is declared.

"Declare the bar last" is what this page used to say, and it was
specifically the thing that did not work — a component declared *after*
the bar painted over an open dropdown and nothing could put it back,
because the z-ordered pass forces forward only
([#430](https://github.com/WonderForgeLabs/gooey/issues/430)). Position
is free now.

**Mnemonics.** The underscore in `Title="_Job"` marks the accelerator:
`alt+j` opens the Job menu from anywhere on the page, whatever holds
focus. Inside an open menu, a plain letter activates the item wearing
it (`Text="Re_start"` → `s`), and `alt+letter` switches menus. The
rules, all of them:

- `__` renders a literal underscore; only the first marker counts.
- An unmarked title or item defaults to its **first letter**, so every
  menu has an accelerator without any authoring.
- Accelerator letters render **underlined, always**. A terminal cannot
  see a held ALT — there are no key-up events — so WPF's
  show-underlines-on-ALT is unimplementable, and always-on is honest.
  The underline is static chrome; it costs no repaints.
- A `KeyBinding` on the same `alt+…` gesture wins over the mnemonic:
  the bar is offered only keys nothing in the focused chain consumed.

> **If you know XAML:** the underscore is the `AccessText` convention,
> chosen over `&` because these strings live in XML attributes, where
> `&` is an entity.

**Gesture hints are display, not bindings.** `Gesture="ctrl+s"` prints
a right-aligned hint in the dropdown; it does not bind the key. The
string is validated at load (a typo is a load error) and shown in the
canonical spelling — byte-identical to what a `KeyBinding` would
declare. Wiring the key is still the `KeyBinding`'s job, which is why
the example declares one for every gesture the menu shows. **The hints
must tell the truth, and you are the one who keeps them honest.**

**While open, the menu is modal.** Arrows navigate (separators are
skipped, the highlight wraps), `enter` activates, `esc` dismisses, and
everything else is swallowed — the page's `q` cannot quit underneath an
open menu. Dismissing **restores focus** to whatever had it when the
menu opened, so clicking a menu while typing in a `TextBox` and
pressing `esc` puts the caret back. And the costs follow tutorial 3's
arithmetic: opening paints 2 (bar highlight + dropdown), moving the
highlight paints 1 (the dropdown alone).

An item with no `Command` is inert — activating it just closes the
menu. An item whose command has a `CanExecute` condition paints `Dim`
and refuses activation while the condition says no, exactly like a
`Button`; the condition is read while painting, so the flip repaints
the open dropdown by itself.

## Step 3: Add a StatusBar

The dim bottom row every terminal app hand-rolls, promoted to a
component. It is content, not an overlay — it covers nothing — so it
sits in the row list like any other child:

```xml
<StatusBar Grid.Row="8" Left="{{.Status}}" Center="{{.Clock}}">
  <StatusBar.Right>
    <Text Style="dim">alt+j: menu   tab: focus   q: quit</Text>
  </StatusBar.Right>
</StatusBar>
```

Each of the three sections takes one of two forms, and giving a section
both is a load error:

- The `Left`/`Center`/`Right` **attribute** is shorthand for "a dim
  line of text", bindable or literal. `Left="{{.Status}}"` is the whole
  promoted pattern.
- The `<StatusBar.Left>` **property element** holds exactly one
  component — anything at all. A bar whose right section is a
  `<Spinner>` while something loads is the same component as one
  showing three strings.

Sections being components means each keeps its own paint node: the
clock ticking in the center repaints the center section and leaves the
key hints alone. Layout gives the edges priority — `Left` takes what it
asked for, `Right` takes what is left of what it asked for, and
`Center` gets the gap — so a long status message shortens the middle
rather than pushing the hints off screen.

One honest limit: `StatusBar` paints nothing of its own and has **no
`Background`**. A container's bounds enclose its children's cells, so
filling the row would wipe sections whose paint nodes are clean. A bar
that should look like a bar styles its sections. The why is recorded in
[container backgrounds](../specs/2026-08-10-container-backgrounds.md).

## Step 4: Pop a Toast from a command

Toasts need a host on the page — an overlay spanning everything, so a
notification can sit in the top-right corner regardless of what is
under it. Full span, anywhere in the `Grid`:

```xml
<ToastHost Name="Toasts" Grid.Row="0" Grid.RowSpan="9" Duration="2500ms"/>
```

The host takes **no children, and a toast has no markup form** — that
is deliberate, not a gap. A toast is imperative by nature ("show this
now"); the declarative surface is the host, and showing is code,
reached through the named element:

```go
toast := func(msg string) {
	if toasts, err := markup.Find[*components.ToastHost](ctx, "Toasts"); err == nil {
		toasts.Show(msg)
	}
}
```

The example fires it from two places: the `Notify` command (the
`toast` button, the menu item, and `ctrl+t` all route there), and the
tick handler itself when the download completes — a toast from ordinary
code, no button involved. Note the lookup happens **per fire** rather
than being captured once: a hot-reload swap rebuilds the named
elements, and a captured pointer would be a dead layer.

`Show` uses the host's `Duration` (absent means 3s); `ShowFor(msg, d)`
picks a lifetime per toast, and a negative duration is sticky until
`Dismiss`. Auto-dismiss follows the same discipline as `Timer` — the
goroutine posts the dismissal to the UI loop, and `Composer.Close`
stops-and-joins so nothing arrives after teardown.

What the layer costs: nothing while no toast is up — the host paints
nothing and measures nothing visible. Showing paints one component (the
toast); dismissing repaints exactly the cells it was covering, which is
the overlay restore pass from [concepts/overlays.md](concepts/overlays.md)
doing its job.

## Step 5: Hover help with Tooltip

Tooltips come in two spellings, and the example uses both on the
`ButtonBar`:

```xml
<Button Content="restart" Click="{{.Start}}" Tooltip="start again from zero"/>
<Button Content="toast" Click="{{.Notify}}">
  <Tooltip Text="pop the status as a toast" Gesture="ctrl+t"/>
</Button>
```

The `Tooltip="…"` **attribute** works on any element — it belongs to
the element like the layout attributes do. The `<Tooltip>` **child
form** is a non-visual attachment like `KeyBinding`, and it is the form
you reach for when you want more than text: here, `Gesture="ctrl+t"`
adds a dim gesture hint to the tip, validated at load and shown in the
canonical spelling — the same display-only rule as `MenuItem`. (With no
`Gesture` at all, a tip renders its host's own `KeyBinding` gesture
automatically.)

Both forms need somewhere to paint: an **AdornmentLayer** on the page.
Same hosting rule as the toast layer, and declared *after* it so tips
paint above even the toasts:

```xml
<AdornmentLayer Grid.Row="0" Grid.RowSpan="9"/>
```

Rest the pointer on a button for the delay (600ms; tune with `Delay`)
and the tip appears adjacent — below, flipping above when the screen
runs out. It dismisses on hover-out, on any key, and on any press,
and the key or press still does its normal job. Only one tip is ever
up: crossing to another tooltipped element swaps them.

Without an `AdornmentLayer` on the page, tooltips silently never show —
the attachment has nowhere to realize itself. If tips are not
appearing, check the layer first.

The layer hosts more than tips. `<ValidationMarker/>` is the second
packaged adorner — it floats a field's error message beside it instead
of taking a row for it ([how-to: validate a form](howto/howto-forms.md))
— and code can `Add`/`Remove` its own adornments for badges, focus
rings, and anything else anchored to another component's bounds.
Authoring one of those is its own topic; the layer's contract is in
[markup-reference.md](../markup-reference.md#adornmentlayer).

## Step 6: The widgets, quickly

The content rows from step 1, one honest paragraph each. Full attribute
tables live in [markup-reference.md](../markup-reference.md).

**[`ProgressBar`](../markup-reference.md#progressbar)** — how far along
a task is. `Value` binds an int, clamped to 0-100 on read. The optional
`Indeterminate` bool binding turns it into a marching band while true —
for the phase of a job that has no number yet — and when the attribute
is **absent, the bar starts no animation goroutine at all**; absence is
load-bearing. `Thresholds="true"` opts into the shared good/warn/crit
color ramp, and it is opt-in for a reason: a gauge's high number is a
warning, but painting a 96%-finished job crit-red says the reverse of
what happened. (`cmd/toolkit` drives the indeterminate mode from a
computed, which is the idiomatic wiring.)

**[`Spinner`](../markup-reference.md#spinner)** — activity with no
progress at all. `Frames` picks a glyph set by name (`braille`, `line`,
`arc`, `dot`), `Enabled` binds a bool. `Enabled` is read **while
painting** as well as at tick time: a paused spinner should look
paused, so it parks at its first frame, and that read is what makes
the flip repaint it — one repaint, once, then zero cost per tick.

**[`Sparkline`](../markup-reference.md#sparkline)** — a series of 0-100
values as block rows, most recent on the right. `Values` binds a
`*prop.Property[[]float64]`; the example appends a rate sample per tick
and caps the history. The series is tail-cropped to the arranged width,
so a narrow window shows recent history rather than compressing all of
it. Set a fresh slice rather than mutating the stored one — the
example's `appendCapped` copies for exactly that reason.

**[`Toggle`](../markup-reference.md#toggle)** — a rocker switch, not a
checkbox, and the difference is the arrows: `←` means off, `→` means
on, and an arrow that would not change anything **is not consumed** —
it keeps bubbling and moves focus instead. Space, enter, and a click
flip it. `Changed` is an after-the-fact notification (the bool has
already flipped when it runs); give `Changed` a `CanExecute` condition
and it doubles as the disable switch, exactly like a `Button`.

**[`Segmented`](../markup-reference.md#segmented)** — the rocker past
two positions. `Options` is a pipe-separated literal or a bound
`[]string`; `Selected` binds the index, clamped on read. Unlike
Toggle's rocker rule, an own-axis arrow at either end **is consumed**
and CYCLES the selection back around by default — `Wrap="false"` turns
that off, and only then does the end-of-travel arrow bubble out like
Toggle's. Either way a keyboard user never dead-ends: the cross-axis
arrow is always left unhandled for spatial navigation. `home`/`end`
jump, space and enter cycle.

**[`ButtonBar`](../markup-reference.md#buttonbar)** — already carrying
the buttons in step 1. More than an `HStack` twice over: `Uniform`
sizing is a measure-pass decision, and the bar is a focus **scope** —
`←`/`→` move between members and wrap at the ends, while `tab` walks
straight through and `↑`/`↓` leave by the ordinary spatial route. A
scope, not a trap. Members that do not fit are collapsed, not clipped,
so `tab` never lands on a button nobody can see.

## What you learned

- Overlays are **lifted out of document order** into a paint layer of
  their own and ranked within it, so where you declare one does not
  decide what it paints over; `Grid.Row` places it independently either
  way. Hit-testing is *not* lifted — that divergence is the one thing
  position still decides.
- `MenuBar` mnemonics come from underscores (`_Job`), default to first
  letters, and render underlined always; `alt+letter` works page-wide,
  and an open menu is modal.
- Menu `Gesture` and Tooltip `Gesture` are **display hints** — a
  `KeyBinding` wires the key, and keeping hints truthful is your job.
- `StatusBar` sections are components with their own paint nodes;
  attribute shorthand for dim text, property elements for anything.
- Toasts are imperative: the host is markup, `Show` is code through
  `markup.Find`, looked up per fire so hot reload cannot strand it.
- Tooltips (both spellings) need an `AdornmentLayer` on the page. Its
  position no longer decides what tips paint over: the layer ranks
  itself to the top of the overlay layer, above toasts and above an open
  dropdown, which is what a validation marker needs.
- The wave-1 widgets share the framework's rules rather than inventing
  their own: arrows are consumed only when they move something, and
  disabled is always "a command whose condition says no".

## Current limitations

- **No context menus or submenus.** The menu machinery could anchor a
  popup at the pointer, but nothing does yet; menus open from the bar
  only.
- **Toast severity is just `Style`.** No error/warning/info variants,
  no per-toast styling from `Show`.
- **Menus ignore the wheel.** Arrows and the pointer are the
  navigation.
- **Your own adornments are code-only.** `Tooltip` and
  `ValidationMarker` are the two packaged adorners with markup forms;
  anything else goes into the `AdornmentLayer` through `Add`/`Remove`
  from Go, with no markup spelling and no tutorial.

## Next steps

- Concept: [overlays](concepts/overlays.md) — the two paint layers and
  their ranks, and how dismissing an overlay restores what it covered.
- The full-size version: [`cmd/toolkit`](../demos.md) puts waves 1
  and 2 on one page, including the pixel-chrome `Button` this tutorial
  skipped.
- Reference: the per-element sections in
  [markup-reference.md](../markup-reference.md) — every attribute of
  every widget used here.
- Depth: the decision records —
  [toolkit wave 1](../specs/2026-08-10-toolkit-wave1.md) and
  [wave 2: overlays](../specs/2026-08-10-toolkit-wave2.md) — say why
  each contract is shaped the way it is.
