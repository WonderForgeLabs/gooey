// Tutorial 4 — input: commands, focus, and KeyBindings that scope
// themselves by where they are declared.
//
//	cd docs/learn/examples/04-input-commands && go run .
//
// Walkthrough: docs/learn/04-input-commands.md
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// checkbox is a focus stop rendering "[x] label". Tutorial 6 takes this
// apart line by line; here it is just a component that happens to exist.
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
	var app *gooey.App

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
			"Quit":        gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Components: map[string]markup.Builder{
			"Checkbox": func(e markup.Element, c *markup.Context) (gooey.Component, error) {
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

	// The App is the run loop: it owns the terminal, the input decoder,
	// frame scheduling and the hot-reload swap. markup.Page is its
	// content — it loads "app.gooey" and rebuilds the tree whenever the
	// file changes, on the UI goroutine, with your viewmodel properties
	// carrying the state across.
	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
