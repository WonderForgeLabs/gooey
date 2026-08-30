# How to declare key bindings

`<KeyBinding>` binds a gesture to a command. It is non-visual: never
measured, never painted, attached to whichever element encloses it.

```xml
<KeyBinding Gesture="ctrl+s" Command="{{.Save}}"/>
```

## Gesture syntax

Zero or more `+`-separated modifiers, then the key. Order does not
matter; modifier and named-key matching is case-insensitive.

| Part | Accepted spellings |
|---|---|
| Modifiers | `ctrl` / `control` / `c`, `alt` / `meta` / `option`, `shift` |
| Named keys | `enter`, `tab`, `esc`, `backspace`, `delete`, `up`, `down`, `left`, `right`, `home`, `end`, `pageup`, `pagedown`, `space` |
| Characters | any single rune: `j`, `q`, `/` |
| The `+` key | `alt++` — the one case that needs spelling out |

Three normalizations match what terminals actually send:

- `shift` on a printable character folds into the rune: `shift+j` is
  `J`, and the shift modifier is dropped. **`ModShift` only ever appears
  on named keys** — a terminal cannot report shift for a character, it
  sends the shifted character.
- `ctrl+<letter>` lowercases the letter, because control bytes decode to
  the lowercase rune. `ctrl+S` and `ctrl+s` are the same gesture.
- `ctrl+@` becomes `ctrl+space`. `0x00` is the byte for both spellings,
  and space is what people press.

An unparseable gesture is a load-time error naming the offending token.

**And so is a ctrl gesture the decoder cannot produce.** That is a
different failure with the same remedy: the gesture parses perfectly and
no terminal will ever send it, so the binding loads, dispatches forever,
and never fires — no error, no warning, nothing at runtime to tell it
apart from a key you never pressed. Those are refused at load time
([#427](https://github.com/WonderForgeLabs/gooey/issues/427)), naming the
cause:

```
input: gesture "ctrl+j" never fires: a terminal sends 0x0a for it, which decodes as enter
```

Two rules cover the whole refused set:

- **`ctrl` reaches only `@`–`_` and `a`–`z`.** A control byte decodes as
  `byte|0x40`, so the digits, most punctuation, the braces and
  `` ctrl+` `` are unreachable by construction.
- **`ctrl+h`, `ctrl+i`, `ctrl+j`, `ctrl+m` and `ctrl+[` are taken** by
  backspace, tab, enter, enter and esc. Bind the named key instead.

`alt+` is not affected — it is an ESC prefix rather than part of the
byte, so `alt+j` and `alt+1` are both fine.

## Scope a binding by where you declare it

Dispatch starts at the focused component and walks up to the root, matching
attached bindings at each level before that component's own key handler. So
placement *is* scoping:

```xml
<Border Title="page" Style="panel">          <!-- root -->
  <Grid Cols="1*,1*">
    <Border Grid.Col="0" Title="stories">
      <StoryList/>
      <KeyBinding Gesture="enter" Command="{{.Open}}"/>   <!-- only while this pane has focus -->
    </Border>
    <Border Grid.Col="1" Title="reader">
      <ArticleBody/>
    </Border>
  </Grid>
  <KeyBinding Gesture="q" Command="{{.Quit}}"/>            <!-- global -->
</Border>
```

Any element embedding `gooey.Base` can host a binding — a `Grid`,
`Border`, stack, or custom component.

> **A page with no focus stops** starts dispatch at the root, so only
> root-attached bindings can ever fire. Add a `Button`, or put the
> binding on the root element.

## Override tab and the arrows

Framework navigation runs only in the **unconsumed tail**: `tab` and
`shift+tab` move focus, and arrows navigate spatially, but *only* if
nothing in the tree consumed the event. So both are overridable:

```xml
<KeyBinding Gesture="tab" Command="{{.NextPane}}"/>
```

A component's `HandleKey` returning true does the same thing for that
component's subtree — which is how a list keeps its own up/down arrows while
the rest of the page still navigates with them.

## Bind the command

`Command` resolves exactly like `Button`'s `Click`:

| Form | Resolves against |
|---|---|
| `Command="{{.Quit}}"` | `Context.Values` — anything implementing `gooey.Action`: a `gooey.Command`, a `*gooey.Cmd` from `gooey.NewCommand`, or a plain `func()` |
| `Command="OnQuit"` | `Context.Handlers` — the code-behind registry |

The binding form needs no code-behind at all, which is what lets
markup-only controls wire their own keys. An unregistered bare name is a
load-time error; an empty attribute is not — it just means no command.

## Disable a binding with a condition

([#33](https://github.com/WonderForgeLabs/gooey/issues/33) landed this
mechanism.) The `Command` field is typed `gooey.Action`, and one
implementation carries a `CanExecute` condition:
`gooey.NewCommand(run).When(cond)`,
where `cond` is an ordinary `*prop.Property[bool]` — a computed is the
point:

```go
save := gooey.NewCommand(vm.Save).When(prop.NewComputed(func() bool {
	return vm.Dirty.Get()
}))
```

A binding whose command is disabled does **not** consume the gesture:
the key keeps bubbling, so an outer binding for the same gesture — or
the framework's own tab/arrow handling — can still take it. A `Button`
bound to the same command paints itself dim and refuses activation.
Markup is unchanged either way: `Command="{{.Save}}"` resolves the
richer command transparently.

There is no `CanExecuteChanged` to raise. The condition is a property,
so a component that read it while painting repaints when it flips — the
graph is the notification system.

## Declare bindings from Go

A tree built in Go composition has no markup to declare bindings in, so
attach them directly. A `KeyBinding` is a non-visual component; any element
embedding `gooey.Base` can host one:

```go
for _, kb := range []*gooey.KeyBinding{
	{Gesture: input.Rune(' '), Command: togglePause},
	{Gesture: input.Rune('f'), Command: cycleFilter},
	{Gesture: input.Named(input.KeyEsc), Command: quit},
	{Gesture: input.KeyEvent{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}, Command: quit},
} {
	root.Attach(kb)
}
```

This is exactly what `<KeyBinding Gesture="space" Command="{{.TogglePause}}"/>`
builds — same attachment, same scoping, same dispatch. `input.Rune`,
`input.Named`, and a literal `input.KeyEvent` are the Go spellings of the
gesture syntax; `input.ParseGesture("ctrl+c")` accepts the string form if
you would rather write it that way.

`Command` is typed `gooey.Action`, so a bare `func(){…}` literal does
not assign — wrap it: `Command: gooey.Command(func() { … })`. And test
whether an action is set with `gooey.CanExecute(a)`, not `a != nil`.

`cmd/logview` and `cmd/markuplog` in this repository are the same app
built both ways, and are worth diffing if you are choosing between them.

## Gotchas

- **First match wins and stops the walk.** Two bindings for the same
  gesture on the same path: the one nearest the focused component fires.
- **`esc` needs a moment.** The decoder waits 40 ms to tell a lone Esc
  from the start of an arrow sequence. That delay is inherent to
  terminals, not a gooey choice.
- **No chords.** One gesture, one key. There is no `ctrl+k ctrl+s`.
- **Disabled means declined, not consumed.** A binding whose command's
  `When` condition is false lets the gesture keep bubbling, exactly as
  if the binding were not there — see the condition section above.

## See also

- [Tutorial 4: Handle input with commands and key bindings](../04-input-commands.md)
- [Concept: input routing](../concepts/input-routing.md)
