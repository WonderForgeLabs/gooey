// Tutorial 6 — writing components: the Component interface, Base, painting
// from bound properties, and joining the focus and input system.
//
//	cd docs/learn/examples/06-custom-components && go run .
//
// Walkthrough: docs/learn/06-custom-components.md
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// ---- meter: the smallest useful component ----
//
// Embedding Base supplies Arrange, Bounds, and the universal layout
// attributes (Width, Margin, Grid.Row, …), so a meter only has to say
// how big it wants to be and how to paint itself.
type meter struct {
	gooey.Base
	value *prop.Property[int]
	max   int
}

func (m *meter) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}

// Render paints THIS component only, into the bounds Arrange assigned.
// value.Get() here is what makes the property a paint dependency: the
// Composer runs Render inside the component's paint node, so any Set on
// value repaints this meter and nothing else. Nobody declares
// "AffectsRender" — reading it is the declaration.
func (m *meter) Render(f *gooey.Frame) {
	b := m.Bounds()
	filled := 0
	if m.max > 0 {
		filled = m.value.Get() * b.W / m.max
	}
	filled = max(0, min(filled, b.W))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", b.W-filled)
	f.Cells.SetString(b.X, b.Y, bar, render.Style{Fg: render.RGB(120, 200, 140)})
}

// ---- stepper: a component that joins the input system ----
//
// FocusState makes it a tab stop and keeps the focused flag in a source
// property, so reading IsFocused() during Render makes focus ordinary
// paint damage — moving focus repaints exactly the two components involved.
type stepper struct {
	gooey.Base
	gooey.FocusState
	value *prop.Property[int]
	label *prop.Property[string]
}

func (s *stepper) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(len([]rune(s.label.Get()))+10, avail.W), H: min(1, avail.H)}
}

func (s *stepper) Render(f *gooey.Frame) {
	b := s.Bounds()
	st := render.Style{Fg: render.RGB(255, 170, 60)}
	if s.IsFocused() {
		st.Reverse = true
	}
	text := fmt.Sprintf("◂ %3d ▸ %s", s.value.Get(), s.label.Get())
	f.Cells.SetString(b.X, b.Y, text, st)
}

// HandleKey receives keys while this component has focus. Returning true
// consumes the event: it stops bubbling to ancestors, which is why these
// arrows never reach the framework's spatial focus navigation.
func (s *stepper) HandleKey(ev input.KeyEvent) bool {
	switch ev {
	case input.Named(input.KeyLeft):
		s.value.Set(s.value.Get() - 1)
		return true
	case input.Named(input.KeyRight):
		s.value.Set(s.value.Get() + 1)
		return true
	}
	return false
}

// HandleMouse is optional. The framework has already moved focus here by
// the time a press arrives (focus-follows-click), so this only has to
// decide what a click means.
func (s *stepper) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MouseClick {
		s.value.Set(s.value.Get() + 1)
		return true
	}
	return false
}

func main() {
	var app *gooey.App

	level := prop.NewSource(6)
	other := prop.NewSource(0)
	readout := prop.NewComputed(func() string {
		return fmt.Sprintf("level = %d    other = %d", level.Get(), other.Get())
	})

	ctx := &markup.Context{
		Values: map[string]any{
			"Level": level, "Other": other, "Readout": readout,
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Components: map[string]markup.Builder{
			// markup.Bound[T] is the same resolver the built-in
			// elements use: it hands back the viewmodel's own handle,
			// and a wrong type is a load error naming both sides.
			"Meter": func(e markup.Element, c *markup.Context) (gooey.Component, error) {
				v, err := markup.Bound[int](e, c, "Value")
				if err != nil {
					return nil, err
				}
				m, err := strconv.Atoi(e.Attrs["Max"])
				if err != nil {
					return nil, fmt.Errorf("<Meter Max=%q>: %w", e.Attrs["Max"], err)
				}
				return &meter{value: v, max: m}, nil
			},
			// Label goes through BoundText rather than e.Attrs, so it
			// accepts a literal, an interpolated "Ch. {{.N}}", or a
			// value-namespace call — the same latitude a <Text> has.
			"Stepper": func(e markup.Element, c *markup.Context) (gooey.Component, error) {
				v, err := markup.Bound[int](e, c, "Value")
				if err != nil {
					return nil, err
				}
				label, err := markup.BoundText(e, c, "Label")
				if err != nil {
					return nil, err
				}
				return &stepper{value: v, label: label}, nil
			},
		},
	}

	// The App is the run loop. Custom components need nothing special from
	// it: they are ordinary tree members, painted through their own
	// nodes and offered input like any built-in.
	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
