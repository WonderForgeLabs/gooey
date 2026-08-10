package mcp

import (
	"encoding/json"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
)

// Rendering the tree snapshot.
//
// The walk itself — interfaces for structure, a type switch for the
// interesting per-component fields, declared (<x:Property>) surfaces
// with current values — lives in the shared control package now; what
// stays here is the shape this tool has always answered with: a nested
// JSON object where a field is present exactly when it says something
// (a name only when named, a layout only when something was set, flags
// only when true). Sparse output is part of the tool's contract — a
// node carrying every zero-valued field would bury the two that matter
// in fifteen that do not.

// renderNode is one control.Node as tree_snapshot has always spelled it.
func renderNode(n *control.Node) map[string]any {
	m := map[string]any{"type": n.Type}
	if n.Name != "" {
		m["name"] = n.Name
	}
	if n.Bounds != nil {
		m["bounds"] = map[string]any{"x": n.Bounds.X, "y": n.Bounds.Y, "w": n.Bounds.W, "h": n.Bounds.H}
	}
	if n.Layout != nil {
		if l := renderLayout(n.Layout); len(l) > 0 {
			m["layout"] = l
		}
	}
	if n.Focusable {
		m["focusable"] = true
	}
	if n.Focused {
		m["focused"] = true
	}
	if n.Hovered {
		m["hovered"] = true
	}
	if len(n.Props) > 0 {
		props := make(map[string]any, len(n.Props))
		for k, v := range n.Props {
			props[k] = valueAny(v)
		}
		m["props"] = props
	}
	if n.Declared != nil {
		m["control"] = n.Control
		m["declared"] = renderDeclared(n.Declared)
	}
	if n.ChildrenElided > 0 {
		m["childrenElided"] = n.ChildrenElided
	}
	if len(n.Attached) > 0 {
		at := make([]any, 0, len(n.Attached))
		for _, x := range n.Attached {
			at = append(at, renderNode(x))
		}
		m["attached"] = at
	}
	if len(n.Children) > 0 {
		kids := make([]any, 0, len(n.Children))
		for _, ch := range n.Children {
			kids = append(kids, renderNode(ch))
		}
		m["children"] = kids
	}
	return m
}

// valueAny turns a typed control.Value into the JSON-native form this
// surface renders: durations as their String() form, colors as #rrggbb,
// everything else as itself.
func valueAny(v control.Value) any {
	switch v.Kind {
	case control.KindString:
		return v.Str
	case control.KindInt:
		return v.Int
	case control.KindBool:
		return v.Bool
	case control.KindFloat:
		return v.Float
	case control.KindDuration:
		return v.Duration.String()
	case control.KindColor:
		return hexColor(v.Color)
	case control.KindAny:
		return json.RawMessage(v.JSON)
	}
	return nil
}

// renderDeclared serializes a control instance's declared surface: for
// each <x:Property>, its name, declared type, and — for the types with a
// markup literal — the current value. Type="any" handles have no
// representable value, so they report the %T of what they hold, the same
// descriptor ceiling list_values applies to off-table handles.
func renderDeclared(ds []control.DeclaredValue) []map[string]any {
	out := make([]map[string]any, 0, len(ds))
	for _, d := range ds {
		e := map[string]any{"name": d.Name, "type": d.Type.String()}
		if d.Value != nil {
			e["value"] = valueAny(*d.Value)
		} else if d.GoType != "" {
			e["goType"] = d.GoType
		}
		out = append(out, e)
	}
	return out
}

// renderLayout reports only the layout fields that were actually set.
func renderLayout(l *gooey.Layout) map[string]any {
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
