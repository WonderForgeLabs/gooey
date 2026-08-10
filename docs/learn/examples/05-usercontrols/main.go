// Tutorial 5 — reusable controls: an Include (markup only) and a
// UserControl (markup plus a typed setup func).
//
//	cd docs/learn/examples/05-usercontrols && go run .
//
// Walkthrough: docs/learn/05-usercontrols.md
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// attr resolves one attribute of a control instance in the PARENT
// context and type-asserts it — the receiving half of the hand-off. The
// demos in cmd/reader carry the same helper.
func attr[T any](parent *markup.Context, e markup.Element, name string) (T, error) {
	var zero T
	v, err := parent.BindingValue(e.Attrs[name])
	if err != nil {
		return zero, fmt.Errorf("%s: %w", name, err)
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("%s: got %T, want %T", name, v, zero)
	}
	return t, nil
}

// statPanel is the code-behind for statpanel.gooey. It runs once per
// instance and returns that instance's own Context: bindings inside the
// control's markup resolve against this, never against the page.
func statPanel(e markup.Element, parent *markup.Context) (*markup.Context, error) {
	value, err := attr[*prop.Property[int]](parent, e, "Value")
	if err != nil {
		return nil, err
	}
	// A control-local computed over the handed-in handle. It is live:
	// the page and the panel share one property, not a copied value.
	reading := prop.NewComputed(func() string {
		return fmt.Sprintf("reading = %d", value.Get())
	})
	return &markup.Context{
		Values: map[string]any{
			"Title":   e.Attrs["Title"], // literal hand-off
			"Note":    e.Attrs["Note"],
			"Reading": reading,
			"Up":      gooey.Command(func() { value.Set(value.Get() + 1) }),
			"Down":    gooey.Command(func() { value.Set(value.Get() - 1) }),
		},
	}, nil
}

func main() {
	var app *gooey.App

	a := prop.NewSource(0)
	b := prop.NewSource(0)
	total := prop.NewComputed(func() string {
		return fmt.Sprintf("A + B = %d", a.Get()+b.Get())
	})

	fsys := os.DirFS(".")

	ctx := &markup.Context{
		Values: map[string]any{
			"A": a, "B": b, "Total": total,
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Components: map[string]markup.Builder{
			"StatPanel": markup.UserControl(fsys, "statpanel.gooey", statPanel),
		},
		// With Includes set, <Card/> resolves to card.gooey by
		// convention — no registration, no code-behind.
		Includes: fsys,
	}

	// The page instantiates two other files, and editing either of them
	// must reload the page — one rebuild re-instantiates every control.
	// markup.Page watches all three; the App does the rebuild on the UI
	// goroutine and swaps the composition.
	app = gooey.NewApp(markup.Page(fsys, "page.gooey", ctx, "statpanel.gooey", "card.gooey"))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
