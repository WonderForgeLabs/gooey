// Tutorial 6 — writing widgets: the Widget interface, Base, painting
// from bound properties, and joining the focus and input system.
//
//	cd examples/06-custom-widgets && go run .
//
// Walkthrough: docs/learn/06-custom-widgets.md
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// ---- meter: the smallest useful widget ----
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

// Render paints THIS widget only, into the bounds Arrange assigned.
// value.Get() here is what makes the property a paint dependency: the
// Composer runs Render inside the widget's paint node, so any Set on
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

// ---- stepper: a widget that joins the input system ----
//
// FocusState makes it a tab stop and keeps the focused flag in a source
// property, so reading IsFocused() during Render makes focus ordinary
// paint damage — moving focus repaints exactly the two widgets involved.
type stepper struct {
	gooey.Base
	gooey.FocusState
	value *prop.Property[int]
	label string
}

func (s *stepper) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(len([]rune(s.label))+10, avail.W), H: min(1, avail.H)}
}

func (s *stepper) Render(f *gooey.Frame) {
	b := s.Bounds()
	st := render.Style{Fg: render.RGB(255, 170, 60)}
	if s.IsFocused() {
		st.Reverse = true
	}
	text := fmt.Sprintf("◂ %3d ▸ %s", s.value.Get(), s.label)
	f.Cells.SetString(b.X, b.Y, text, st)
}

// HandleKey receives keys while this widget has focus. Returning true
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
	running := true

	level := prop.NewSource(6)
	other := prop.NewSource(0)
	readout := prop.NewComputed(func() string {
		return fmt.Sprintf("level = %d    other = %d", level.Get(), other.Get())
	})

	// intAttr resolves an attribute that must be a bound int property.
	intAttr := func(c *markup.Context, e markup.Element, name string) (*prop.Property[int], error) {
		v, err := c.BindingValue(e.Attrs[name])
		if err != nil {
			return nil, err
		}
		p, ok := v.(*prop.Property[int])
		if !ok {
			return nil, fmt.Errorf("<%s %s>: got %T, want *prop.Property[int]", e.Name, name, v)
		}
		return p, nil
	}

	ctx := &markup.Context{
		Values: map[string]any{
			"Level": level, "Other": other, "Readout": readout,
			"Quit": gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Widgets: map[string]markup.Builder{
			"Meter": func(e markup.Element, c *markup.Context) (gooey.Widget, error) {
				v, err := intAttr(c, e, "Value")
				if err != nil {
					return nil, err
				}
				m, err := strconv.Atoi(e.Attrs["Max"])
				if err != nil {
					return nil, fmt.Errorf("<Meter Max=%q>: %w", e.Attrs["Max"], err)
				}
				return &meter{value: v, max: m}, nil
			},
			"Stepper": func(e markup.Element, c *markup.Context) (gooey.Widget, error) {
				v, err := intAttr(c, e, "Value")
				if err != nil {
					return nil, err
				}
				return &stepper{value: v, label: e.Attrs["Label"]}, nil
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
	screen.EnableMouse()

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
