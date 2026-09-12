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
//
// A SCOPED service is NARROWED, not refused: the walk is rooted at the
// grant's island, so a guest sees its own subtree and has no way to
// learn the shape of the rest of the page. Hiding is the point — a guest
// that could enumerate the host's tree could pick targets for every
// other verb and discover the app's structure through the refusals.
func (s *Service) Tree(depth int) (*Node, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	root := c.Root()
	if root == nil {
		return nil, preconditionf("the composition has no root")
	}
	if s.scoped() {
		root = s.islandRoot()
		if root == nil {
			return nil, s.islandGone()
		}
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

// islandRect resolves a scoped session's island to its bounds, in
// ABSOLUTE screen cells.
//
// Three callers wanted the same four steps — islandRoot, the nil check,
// the gooey.Bounded assertion, Bounds() — and wrote them out separately,
// with denial messages that had already drifted apart ("its screen region
// cannot be read" against "its size cannot be read") for what is one
// rule. The duplication is the reason the rule could drift: there was no
// single place for "what does this island occupy" to be answered.
// islandGoneFmt is the denial every island-addressed call gives when the
// grant names an element the running tree no longer has.
//
// A shared FORMAT rather than five copies of one sentence: it was written
// out at each site and had begun to drift, and one site appends a clause
// — which stays a deliberate difference only while the shared half is
// literally shared. It is a format string and not a constructor because
// deniedf returns *Error, whose Kind callers switch on; wrapping it would
// change the type for the sake of tidiness.
const islandGoneFmt = "this session is scoped to island %q, which names no element in the running tree"

func (s *Service) islandGone() *Error { return deniedf(islandGoneFmt, s.grant.Island) }

func (s *Service) islandRect() (gooey.Rect, error) {
	root := s.islandRoot()
	if root == nil {
		return gooey.Rect{}, s.islandGone()
	}
	b, ok := root.(gooey.Bounded)
	if !ok {
		return gooey.Rect{}, preconditionf("element %q exposes no bounds, so its screen region cannot be read", s.grant.Island)
	}
	return b.Bounds(), nil
}

// Screen reads the retained cell plane as of the last composed frame.
// It NEVER composes a frame of its own: doing so would mark dirty nodes
// clean and steal the repaint from the app's own next frame — the
// damage count the framework guarantees. styled asks for the ANSI
// escape stream a terminal would need to show the screen; plain is one
// line per row, trailing blanks trimmed.
// A SCOPED service reads only its island's rectangle, in BOTH forms.
// The crop is the same hiding rule the tree walk uses, and the styled
// form is cropped rather than refused: refusing one flag value while
// narrowing the other would be an API shape driven by which helper
// happened to exist, not by what a guest may see.
//
// Composer.Snapshot really is the whole cell plane by construction, but
// nothing needed a new encoder — the island's cells copy into a fresh
// render.Buffer of the island's size and the ordinary one-shot Flush
// encodes that. What a guest receives is a self-contained escape stream
// for a screen whose entire content is its island, which is exactly the
// fiction the rest of the scoping maintains.
func (s *Service) Screen(styled bool) (string, error) {
	c, err := s.composer()
	if err != nil {
		return "", err
	}
	if s.scoped() {
		r, err := s.islandRect()
		if err != nil {
			return "", err
		}
		if styled {
			return croppedStyled(c.Cells(), r, c.Caps().Color)
		}
		return cropped(c.Cells(), r), nil
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

// cropped renders one rectangle of the cell plane as plain text, one
// line per row with trailing blanks trimmed — the same shape Screen's
// unscoped plain form has, over a window instead of the plane. Cells
// outside the buffer are blanks: an island arranged partly offscreen
// reads as short rows, never as a bounds panic.
func cropped(buf *render.Buffer, r gooey.Rect) string {
	if r.W <= 0 || r.H <= 0 {
		return ""
	}
	lines := make([]string, 0, r.H)
	for y := r.Y; y < r.Y+r.H; y++ {
		row := make([]rune, 0, r.W)
		for x := r.X; x < r.X+r.W; x++ {
			ch := ' '
			if x >= 0 && y >= 0 && x < buf.W && y < buf.H {
				if got := buf.At(x, y).Rune; got != 0 {
					ch = got
				}
			}
			row = append(row, ch)
		}
		lines = append(lines, strings.TrimRight(string(row), " "))
	}
	return strings.Join(lines, "\n")
}

// croppedStyled renders one rectangle of the cell plane as a
// self-contained ANSI stream — what a terminal of exactly that size
// would need to show the island, and nothing about the rest of the page.
//
// It copies into a fresh Buffer rather than teaching the encoder about
// rectangles, because a sub-buffer IS the right model here: the guest's
// screen is its island, so the stream it gets should be homed at 0,0 and
// as wide as the island, not a set of absolute cursor moves that betray
// where on the host's page the island sits.
func croppedStyled(buf *render.Buffer, r gooey.Rect, depth render.ColorDepth) (string, error) {
	if r.W <= 0 || r.H <= 0 {
		return "", nil
	}
	sub := render.NewBuffer(r.W, r.H)
	for y := 0; y < r.H; y++ {
		for x := 0; x < r.W; x++ {
			ch, st := ' ', render.Style{}
			if sx, sy := r.X+x, r.Y+y; sx >= 0 && sy >= 0 && sx < buf.W && sy < buf.H {
				c := buf.At(sx, sy)
				st = c.Style
				if c.Rune != 0 {
					ch = c.Rune
				}
			}
			sub.Set(x, y, ch, st)
		}
	}
	var sb strings.Builder
	if err := render.Flush(&sb, sub, depth); err != nil {
		return "", err
	}
	return sb.String(), nil
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

// ScreenSize is the size of the surface this session may see, in cells,
// plus the terminal's cell metrics in pixels.
//
// CELLS AND PIXELS BOTH, because the two callers are different and
// neither can derive the other: coordinates for SendMouse are cells, and
// the graphics layer sizes a picture in pixels (term.Caps.CellW/CellH).
// A client that had to ask twice would ask once and guess the rest.
type ScreenSize struct {
	Cols, Rows   int
	X, Y         int
	CellW, CellH int
}

// ScreenSize reports the screen this session is allowed to see.
//
// It exists because the only way to learn the screen was to INFER it
// from the root component's arranged bounds (issue #204), or to read it
// off screen_text.
//
// #204 and an earlier draft of this comment justified the tool by saying
// the root-bounds inference "equals the terminal only while the root
// happens to fill it — give the root a margin, a fixed Width or a
// non-stretch alignment". That is FALSE. Frame arranges the root with
// Arrange(Rect{0, 0, c.cols, c.rows}) and Base.Arrange stores what it is
// handed, so the root reports the screen whatever it declares; margin,
// size and alignment are applied by MeasureChild/ArrangeChild, the
// sandwich the root — being nobody's child — never passes through. A
// root declaring Margin, Width, Height, HAlign and VAlign together was
// measured, and it reported the full terminal (mcp.TestTheRootAlwaysFillsTheScreen pins
// that, so this paragraph fails rather than rots if the root ever starts
// honouring its own size).
//
// The reasons that survive measurement:
//
//   - A SCOPED session's island genuinely is not the screen. That is the
//     case where the inference returns a wrong answer rather than an
//     unproven one, and it is what this tool is really for.
//   - screen_text's lines are trailing-trimmed, so the width it implies
//     is the longest PAINTED line, not the terminal's.
//   - Both workarounds cost a whole tree or a whole screen to learn two
//     integers.
//   - Neither carries the cell metrics at all.
//
// A SCOPED SESSION IS TOLD ITS ISLAND'S SIZE, which is the same fiction
// Screen maintains by cropping to the island: a guest's whole screen is
// its island. Answering with the terminal would break it in the
// direction that costs something — a guest told the screen is 60x14 when
// it may only touch a 60x3 border computes coordinates for cells it
// cannot reach, and SendMouse answers those with silence rather than an
// error.
//
// X and Y carry the island's ORIGIN, and they are what make that fiction
// usable rather than merely comfortable. SendPointer (control/input.go)
// takes ABSOLUTE screen cells and mayPoint refuses anything landing
// outside the island, so size alone is not enough to act: an island at
// y=1 h=3 is told rows=3, and a guest that believes its rows run 0..2
// has one refused row and one unreachable one. The size says how big the
// region is; the origin is how the guest turns a position inside it into
// the coordinate SendPointer accepts. Disclosing it costs nothing that is
// not already disclosed — a scoped tree_snapshot returns the island
// root's bounds in absolute coordinates, and the residual "a guest can
// infer host geometry" is already booked in
// docs/specs/2026-08-14-island-grants.md.
//
// For an UNSCOPED session the origin is (0,0): the screen is the region.
//
// The CELL METRICS are not scoped, because they are a property of the
// terminal rather than of the region: a pixel is the same size inside an
// island as outside it.
//
// They are ZERO when nobody has measured them, and a client must branch
// on that rather than divide by it. The converse does NOT hold and the
// schema says so: a non-zero pair may be a real probe OR App's
// substituted term.DefaultCellW/H, which it fills in for a pixel-plane
// app. So 0 means "certainly unmeasured"; non-zero means "usable", not
// "measured". Reporting which would need a provenance bit the caps
// struct does not carry. The probe that fills them is opt-in
// (gooey.WithCapabilityProbe — "a round trip that only graphics apps
// need"), and App's own backfill to term.DefaultCellW/H fires only for a
// pixel-plane app (app.go, `c.CellW <= 0 && a.pixelPlane(c)`), so an
// ordinary cell-plane app reports 0/0. Substituting the defaults here
// would answer a question nobody asked the terminal — the same
// make-it-up-so-the-field-is-populated move this tool exists to replace,
// since inventing 10x20 is not better than the root-bounds inference.
func (s *Service) ScreenSize() (ScreenSize, error) {
	c, err := s.composer()
	if err != nil {
		return ScreenSize{}, err
	}
	caps := c.Caps()
	size := ScreenSize{CellW: caps.CellW, CellH: caps.CellH}
	if s.scoped() {
		r, err := s.islandRect()
		if err != nil {
			return ScreenSize{}, err
		}
		size.Cols, size.Rows = r.W, r.H
		size.X, size.Y = r.X, r.Y
		return size, nil
	}
	// c.Cells(), NOT Composer.Size(), and the difference is only visible
	// in the window this tool exists to be correct in.
	//
	// Size() returns the cols/rows the composer was last TOLD; Cells() is
	// the plane as of the last composed frame. Across a pending resize
	// they differ — and screen_text renders that same buffer, so taking
	// Size() here would let a client read a width from screen_size that
	// the screen_text it fetched in the same breath does not have. A
	// review asked for Size() as the tidier source; it is the wrong one,
	// and an earlier review asked for exactly this instead. The invariant
	// is not "buffer tracks cols/rows" — it is that these two tools
	// answer from one place.
	buf := c.Cells()
	size.Cols, size.Rows = buf.W, buf.H
	return size, nil
}
