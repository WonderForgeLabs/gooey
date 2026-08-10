package mcp

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Serializing the tree without reflection.
//
// The structure comes from the framework's own interfaces — the same ones
// the Composer and the FocusManager walk: Container for children,
// Attacher for the non-visual attachments, Bounded for the arranged rect,
// HasLayout for the FrameworkElement properties, Focusable for whether a
// widget is a tab stop. Anything implementing them serializes, including
// widgets this package has never heard of.
//
// The interesting per-widget fields come from a type switch over the
// built-in components. An unknown widget still produces a useful node —
// its %T, its bounds, its layout, its children — it just has no props.
// That is the deliberate ceiling: a third-party widget's fields cannot be
// discovered without reflection, and when markup-declared properties
// (docs/specs/2026-08-10-markup-declared-properties.md) land, x:Property
// will be the declaration that lets them serialize without one.
//
// Every Get() below happens outside any computed evaluation, on the UI
// goroutine, so it reads a value and records NOTHING. That is the
// call-site rule doing its job: the same Get inside a Render would be a
// subscription, and a snapshot that subscribed would wire the MCP server
// into the damage graph and repaint the app every time an agent looked
// at it.

// walk serializes one widget and its subtree. depth 0 means unlimited.
func (s *Server) walk(w gooey.Widget, names map[gooey.Widget]string, fm *gooey.FocusManager, depth, level int) map[string]any {
	n := map[string]any{"type": fmt.Sprintf("%T", w)}
	if name := names[w]; name != "" {
		n["name"] = name
	}
	if b, ok := w.(gooey.Bounded); ok {
		r := b.Bounds()
		n["bounds"] = map[string]any{"x": r.X, "y": r.Y, "w": r.W, "h": r.H}
	}
	if hl, ok := w.(gooey.HasLayout); ok {
		if l := layoutOf(hl); len(l) > 0 {
			n["layout"] = l
		}
	}
	if f, ok := w.(gooey.Focusable); ok && f.AcceptsFocus() {
		n["focusable"] = true
	}
	if fm != nil {
		if fm.Focused() == w {
			n["focused"] = true
		}
		if fm.Hovered() == w {
			n["hovered"] = true
		}
	}
	if p := widgetProps(w); len(p) > 0 {
		n["props"] = p
	}

	if depth > 0 && level >= depth {
		if c, ok := w.(gooey.Container); ok && len(c.ChildWidgets()) > 0 {
			n["childrenElided"] = len(c.ChildWidgets())
		}
		return n
	}
	if a, ok := w.(gooey.Attacher); ok {
		var at []any
		for _, x := range a.Attachments() {
			at = append(at, s.walk(x, names, fm, depth, level+1))
		}
		if len(at) > 0 {
			n["attached"] = at
		}
	}
	if c, ok := w.(gooey.Container); ok {
		var kids []any
		for _, ch := range c.ChildWidgets() {
			if ch == nil {
				continue
			}
			kids = append(kids, s.walk(ch, names, fm, depth, level+1))
		}
		if len(kids) > 0 {
			n["children"] = kids
		}
	}
	return n
}

// widgetProps is the type switch: what is worth knowing about each
// built-in widget beyond its bounds.
func widgetProps(w gooey.Widget) map[string]any {
	switch t := w.(type) {
	case *gooey.Text:
		return map[string]any{"text": str(t.Content)}
	case *gooey.Button:
		return map[string]any{"content": str(t.Content), "hasCommand": t.Click != nil}
	case *gooey.Checkbox:
		return map[string]any{"label": str(t.Label), "checked": t.IsChecked()}
	case *gooey.TextBox:
		return map[string]any{"text": str(t.Text), "prompt": str(t.Prompt), "caret": t.Caret()}
	case *gooey.Border:
		return map[string]any{"title": str(t.Title)}
	case *gooey.Gauge:
		p := map[string]any{"label": str(t.Label)}
		if t.Value != nil {
			p["value"] = t.Value.Get()
		}
		return p
	case *gooey.Sparkline:
		if t.Values == nil {
			return nil
		}
		return map[string]any{"points": len(t.Values.Get())}
	case *gooey.ColorPicker:
		return map[string]any{"value": hexColor(t.Color()), "channel": t.Channel()}
	case *gooey.Grid:
		return map[string]any{"rows": len(t.Rows), "cols": len(t.Cols)}
	case *gooey.VStack:
		return map[string]any{"gap": t.Gap}
	case *gooey.HStack:
		return map[string]any{"gap": t.Gap}
	case *gooey.KeyBinding:
		return map[string]any{"gesture": t.Gesture.String(), "hasCommand": t.Command != nil}
	case *gooey.Timer:
		p := map[string]any{"interval": t.Interval.String(), "hasTick": t.Tick != nil}
		if t.Enabled != nil {
			p["enabled"] = t.Enabled.Get()
		}
		return p
	}
	return nil
}

// layoutOf reports only the layout fields that were actually set. A node
// carrying every zero-valued FrameworkElement property would bury the two
// that matter in fifteen that do not.
func layoutOf(hl gooey.HasLayout) map[string]any {
	l := hl.LayoutProps()
	m := map[string]any{}
	if l.Width != 0 {
		m["width"] = l.Width
	}
	if l.Height != 0 {
		m["height"] = l.Height
	}
	if l.Margin != (gooey.Thickness{}) {
		m["margin"] = []int{l.Margin.L, l.Margin.T, l.Margin.R, l.Margin.B}
	}
	if l.HAlign != gooey.AlignStretch {
		m["hAlign"] = alignName(l.HAlign)
	}
	if l.VAlign != gooey.AlignStretch {
		m["vAlign"] = alignName(l.VAlign)
	}
	if l.Visibility != gooey.Visible {
		m["visibility"] = visibilityName(l.Visibility)
	}
	if l.Row != 0 {
		m["gridRow"] = l.Row
	}
	if l.Col != 0 {
		m["gridCol"] = l.Col
	}
	if l.RowSpan != 0 {
		m["gridRowSpan"] = l.RowSpan
	}
	if l.ColSpan != 0 {
		m["gridColSpan"] = l.ColSpan
	}
	if l.Left != 0 {
		m["canvasLeft"] = l.Left
	}
	if l.Top != 0 {
		m["canvasTop"] = l.Top
	}
	return m
}

func alignName(a gooey.Align) string {
	switch a {
	case gooey.AlignStart:
		return "Start"
	case gooey.AlignCenter:
		return "Center"
	case gooey.AlignEnd:
		return "End"
	}
	return "Stretch"
}

func visibilityName(v gooey.Visibility) string {
	switch v {
	case gooey.Hidden:
		return "Hidden"
	case gooey.Collapsed:
		return "Collapsed"
	}
	return "Visible"
}

// names inverts the markup context's Named table so the walk can label a
// widget in one map read. Widgets are pointers, so they are comparable
// and usable as keys.
func names(ctx *markup.Context) map[gooey.Widget]string {
	out := map[gooey.Widget]string{}
	if ctx == nil {
		return out
	}
	for n, w := range ctx.Named {
		out[w] = n
	}
	return out
}

func str(p *prop.Property[string]) string {
	if p == nil {
		return ""
	}
	return p.Get()
}
