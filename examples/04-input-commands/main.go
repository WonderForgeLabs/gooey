// Tutorial 4 — input: commands, focus, and KeyBindings that scope
// themselves by where they are declared.
//
//	cd examples/04-input-commands && go run .
//
// Walkthrough: docs/learn/04-input-commands.md
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// checkbox is a focus stop rendering "[x] label". Tutorial 6 takes this
// apart line by line; here it is just a widget that happens to exist.
type checkbox struct {
	gooey.Base
	gooey.FocusState
	checked *prop.Property[bool]
	label   string
}

func (c *checkbox) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(4+len(c.label), avail.W), H: min(1, avail.H)}
}

func (c *checkbox) Render(f *gooey.Frame) {
	b := c.Bounds()
	box := "[ ] "
	if c.checked.Get() {
		box = "[x] "
	}
	st := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	if c.IsFocused() {
		st.Reverse = true
	}
	f.Cells.SetString(b.X, b.Y, box+c.label, st)
}

func (c *checkbox) toggle() { c.checked.Set(!c.checked.Get()) }

func (c *checkbox) HandleKey(ev input.KeyEvent) bool {
	if ev == input.Named(input.KeyEnter) || ev == input.Rune(' ') {
		c.toggle()
		return true
	}
	return false
}

func (c *checkbox) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MouseClick {
		c.toggle()
		return true
	}
	return false
}

func main() {
	running := true

	last := prop.NewSource("ready — press tab to move focus")
	loud := prop.NewSource(false)

	// status depends on both: toggling the checkbox restyles the line
	// without any command touching it.
	status := prop.NewComputed(func() string {
		s := "last: " + last.Get()
		if loud.Get() {
			return strings.ToUpper(s)
		}
		return s
	})

	say := func(what string) gooey.Command {
		return gooey.Command(func() { last.Set(what) })
	}

	ctx := &markup.Context{
		Values: map[string]any{
			"Status":      status,
			"Loud":        loud,
			"LeftA":       say("left A"),
			"LeftB":       say("left B"),
			"RightA":      say("right A"),
			"LeftScoped":  say("s in the LEFT pane"),
			"RightScoped": say("s in the RIGHT pane"),
			"Quit":        gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Widgets: map[string]markup.Builder{
			"Checkbox": func(e markup.Element, c *markup.Context) (gooey.Widget, error) {
				v, err := c.BindingValue(e.Attrs["Checked"])
				if err != nil {
					return nil, err
				}
				checked, ok := v.(*prop.Property[bool])
				if !ok {
					return nil, fmt.Errorf("Checkbox Checked: got %T, want *prop.Property[bool]", v)
				}
				return &checkbox{checked: checked, label: e.Attrs["Label"]}, nil
			},
		},
	}

	fsys := os.DirFS(".")
	tree, err := markup.Load(fsys, "app.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	var comp *gooey.Composer
	needsFrame := true
	attach := func(w gooey.Widget) {
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)

	swaps := make(chan gooey.Widget, 1)
	stopWatch := markup.Watch(fsys, "app.gooey", ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()
	screen.EnableMouse() // clicks and hover

	events := make(chan input.Event, 16)
	go term.DecodeEvents(screen, events)

	for running {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case ev := <-events:
			comp.Handle(ev)
		}
	}
}
