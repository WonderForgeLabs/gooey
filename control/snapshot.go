package control

import (
	"fmt"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Serializing the tree without reflection.
//
// The structure comes from the framework's own interfaces — the same
// ones the Composer and the FocusManager walk: Container for children,
// Attacher for the non-visual attachments, Bounded for the arranged
// rect, HasLayout for the FrameworkElement properties, Focusable for
// whether a component is a tab stop. Anything implementing them
// serializes, including components this package has never heard of.
//
// The interesting per-component fields come from a type switch over the
// built-in components. An unknown component still produces a useful
// node — its %T, its bounds, its layout, its children — it just has no
// props. That is the deliberate ceiling: an arbitrary Go component's
// fields cannot be discovered without reflection, and stay undiscovered.
// Markup-built controls are the exception the framework declares its way
// out of: their <x:Property> surface is retained in
// markup.Context.Declared and serializes with current values.

// Node is one component in the live tree — the in-process TreeNode.
type Node struct {
	// Type is the Go type, e.g. "*components.Button". Diagnostic
	// identity; the durable identity is Name.
	Type string
	// Name is the Name= identity from markup, empty if unnamed.
	Name string
	// Bounds is the arranged rect, nil when the component exposes none.
	Bounds *gooey.Rect
	// Layout is a copy of the FrameworkElement surface, nil when the
	// component carries none or when every field is the default.
	Layout    *gooey.Layout
	Focusable bool
	Focused   bool
	Hovered   bool
	// Props is the type-switched interesting fields of known component
	// kinds, as typed values.
	Props    map[string]Value
	Attached []*Node
	Children []*Node
	// ChildrenElided is how many children a depth limit hid.
	ChildrenElided int
	// Declared is the markup-declared (<x:Property>) surface of the
	// control instance rooted at this node, with current values.
	Declared []DeclaredValue
	// Control is the markup file the declarations came from.
	Control string
}

// DeclaredValue is one markup-declared dependency property as a
// snapshot reports it: the declaration plus the instance's CURRENT
// value.
type DeclaredValue struct {
	Name string
	Type Kind
	// Value is the current value for kinds with a markup literal; nil
	// for KindAny handles, whose ceiling is the descriptor.
	Value *Value
	// GoType is the %T of what an off-table handle holds. Diagnostic.
	GoType string
}

// Tree serializes the live component tree. depth 0 means unlimited.
func (s *Service) Tree(depth int) (*Node, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	root := c.Root()
	if root == nil {
		return nil, preconditionf("the composition has no root")
	}
	return s.walk(root, treeNames(s.bind), c.Focus(), depth, 1), nil
}

func (s *Service) walk(w gooey.Component, names map[gooey.Component]string, fm *gooey.FocusManager, depth, level int) *Node {
	n := &Node{Type: fmt.Sprintf("%T", w), Name: names[w]}
	if b, ok := w.(gooey.Bounded); ok {
		r := b.Bounds()
		n.Bounds = &r
	}
	if hl, ok := w.(gooey.HasLayout); ok {
		if l := hl.LayoutProps(); l != nil && !defaultLayout(l) {
			cp := *l
			n.Layout = &cp
		}
	}
	if f, ok := w.(gooey.Focusable); ok && f.AcceptsFocus() {
		n.Focusable = true
	}
	if fm != nil {
		n.Focused = fm.Focused() == w
		n.Hovered = fm.Hovered() == w
	}
	n.Props = componentProps(w)
	if s.bind != nil {
		if ds, ok := s.bind.Declared[w]; ok {
			n.Control = ds.Control
			n.Declared = declaredValues(ds)
		}
	}

	if depth > 0 && level >= depth {
		if c, ok := w.(gooey.Container); ok {
			n.ChildrenElided = len(c.ChildComponents())
		}
		return n
	}
	if a, ok := w.(gooey.Attacher); ok {
		for _, x := range a.Attachments() {
			n.Attached = append(n.Attached, s.walk(x, names, fm, depth, level+1))
		}
	}
	if c, ok := w.(gooey.Container); ok {
		for _, ch := range c.ChildComponents() {
			if ch == nil {
				continue
			}
			n.Children = append(n.Children, s.walk(ch, names, fm, depth, level+1))
		}
	}
	return n
}

// componentProps is the type switch: what is worth knowing about each
// built-in component beyond its bounds.
func componentProps(w gooey.Component) map[string]Value {
	switch t := w.(type) {
	case *components.Text:
		return map[string]Value{"text": StringValue(str(t.Content))}
	case *components.Button:
		return map[string]Value{
			"content":    StringValue(str(t.Content)),
			"hasCommand": BoolValue(t.Click != nil),
		}
	case *components.Checkbox:
		return map[string]Value{
			"label":   StringValue(str(t.Label)),
			"checked": BoolValue(t.IsChecked()),
		}
	case *components.TextBox:
		return map[string]Value{
			"text":   StringValue(str(t.Text)),
			"prompt": StringValue(str(t.Prompt)),
			"caret":  IntValue(int64(t.Caret())),
		}
	case *components.Border:
		return map[string]Value{"title": StringValue(str(t.Title))}
	case *components.Gauge:
		p := map[string]Value{"label": StringValue(str(t.Label))}
		if t.Value != nil {
			p["value"] = IntValue(int64(t.Value.Get()))
		}
		return p
	case *components.Sparkline:
		if t.Values == nil {
			return nil
		}
		return map[string]Value{"points": IntValue(int64(len(t.Values.Get())))}
	case *components.ColorPicker:
		return map[string]Value{
			"value":   ColorValue(t.Color()),
			"channel": IntValue(int64(t.Channel())),
		}
	case *components.Grid:
		return map[string]Value{
			"rows": IntValue(int64(len(t.Rows))),
			"cols": IntValue(int64(len(t.Cols))),
		}
	case *components.VStack:
		return map[string]Value{"gap": IntValue(int64(t.Gap))}
	case *components.HStack:
		return map[string]Value{"gap": IntValue(int64(t.Gap))}
	case *gooey.KeyBinding:
		return map[string]Value{
			"gesture":    StringValue(t.Gesture.String()),
			"hasCommand": BoolValue(t.Command != nil),
		}
	case *components.Timer:
		p := map[string]Value{
			"interval": DurationValue(t.Interval),
			"hasTick":  BoolValue(t.Tick != nil),
		}
		if t.Enabled != nil {
			p["enabled"] = BoolValue(t.Enabled.Get())
		}
		return p
	}
	return nil
}

// declaredValues serializes a control instance's declared surface. The
// Gets here are outside any evaluation: reads, not subscriptions.
func declaredValues(ds markup.DeclaredSurface) []DeclaredValue {
	out := make([]DeclaredValue, 0, len(ds.Props))
	for _, p := range ds.Props {
		d := DeclaredValue{Name: p.Name, Type: KindOf(p.Type)}
		set := func(v Value) { d.Value = &v }
		switch h := p.Handle.(type) {
		case *prop.Property[string]:
			set(StringValue(h.Get()))
		case *prop.Property[int]:
			set(IntValue(int64(h.Get())))
		case *prop.Property[bool]:
			set(BoolValue(h.Get()))
		case *prop.Property[float64]:
			set(FloatValue(h.Get()))
		case *prop.Property[time.Duration]:
			set(DurationValue(h.Get()))
		case *prop.Property[render.Color]:
			set(ColorValue(h.Get()))
		case *prop.Property[any]:
			d.GoType = fmt.Sprintf("%T", h.Get())
		default:
			d.GoType = fmt.Sprintf("%T", p.Handle)
		}
		out = append(out, d)
	}
	return out
}

// Screen reads the retained cell plane as of the last composed frame.
// It NEVER composes a frame of its own: doing so would mark dirty nodes
// clean and steal the repaint from the app's own next frame — the
// damage count the framework guarantees. styled asks for the ANSI
// escape stream a terminal would need to show the screen; plain is one
// line per row, trailing blanks trimmed.
func (s *Service) Screen(styled bool) (string, error) {
	c, err := s.composer()
	if err != nil {
		return "", err
	}
	if styled {
		var sb strings.Builder
		// Snapshot, not Flush: Flush sends the difference since the last
		// frame, and a screenshot wants the screen.
		if err := c.Snapshot(&sb); err != nil {
			return "", err
		}
		return sb.String(), nil
	}
	buf := c.Cells()
	lines := make([]string, 0, buf.H)
	for y := 0; y < buf.H; y++ {
		row := make([]rune, 0, buf.W)
		for x := 0; x < buf.W; x++ {
			r := buf.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			row = append(row, r)
		}
		lines = append(lines, strings.TrimRight(string(row), " "))
	}
	return strings.Join(lines, "\n"), nil
}

// defaultLayout reports whether every EXPLICIT layout field is at its
// framework default — the "report only what was set" convention. It
// checks the exported fields one by one because Layout also caches a
// measurement internally, and a cached measurement is not something the
// author set.
func defaultLayout(l *gooey.Layout) bool {
	return l.Width == 0 && l.Height == 0 &&
		l.Margin == (gooey.Thickness{}) &&
		l.HAlign == gooey.AlignStretch && l.VAlign == gooey.AlignStretch &&
		l.Visibility == gooey.Visible &&
		l.Row == 0 && l.Col == 0 && l.RowSpan == 0 && l.ColSpan == 0 &&
		l.Left == 0 && l.Top == 0
}

// treeNames inverts the markup context's Named table so the walk can
// label a component in one map read.
func treeNames(ctx *markup.Context) map[gooey.Component]string {
	out := map[gooey.Component]string{}
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
