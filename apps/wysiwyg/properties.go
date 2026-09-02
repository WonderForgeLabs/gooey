package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/paint"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// THE PROPERTY BROWSER'S EDITING SURFACE — the thing that floats over the
// selected row.
//
// WHAT IT REPLACES, and why the replacement had to be a component rather
// than a rearrangement of the markup. The pane used to carry ONE TextBox
// in a fixed track at the bottom of the panel: enter on a row loaded its
// name and value down there, roughly forty rows from the row you were
// looking at. That bottom track was itself a bug fix — as a plain VStack
// the pane could not be edited at all, because ItemsView measures greedily,
// took every row, and left the input arranged at W:0 H:0 below the panel
// (the comment in wysiwyg.gooey still records it) — so the fix was real
// and the ergonomics were still wrong.
//
// THE POSITION THIS COMPONENT TAKES IS NOT A TRACK. It occupies the SAME
// grid cell as the list and arranges its own children by explicit
// geometry, which is why the greedy-measure trap cannot come back: there
// is no sibling competing for rows, and an editor's rect is computed from
// the row it belongs to rather than from what a stack had left over.
// TestTheEditorFloatsOverTheSelectedRow is the pin.
//
// WHY A ROW CANNOT SIMPLY HOST THE EDITOR. ItemsView is ONE focus stop by
// design — rows are painted from a template and are not focusable children
// (AcceptsFocus, components/itemsview.go). So an in-cell caret is not a
// matter of putting a TextBox in the row template; it is an overlay
// positioned over the selected row's arranged rect. That is also what
// Visual Studio's grid actually does: the grid paints, and an edit control
// floats over the cell.
//
// THE OVERLAYS, and why there are three rather than one:
//
//   - text is a real components.TextBox, because a caret is component
//     state and nothing drawn by a func can have one. It floats over the
//     VALUE CELL and takes focus while it is open.
//   - cp is a real components.ColorPicker, for the same reason: channel
//     bars, a pixel tier, and its own key handling already exist.
//   - the Popup's surface is a draw func, and everything that is a
//     PICTURE OF A LIST goes through it — the dropdown, the binding
//     picker, the track editor, the chord capture, the stepper.
//
// WHY components.Popup at all, when only some of the modes use its
// surface: it owns the open property, pointer capture, the outside-press
// dismissal grammar, and — the subtle one — the SUBSCRIPTION CARRIER. A
// Collapsed surface never evaluates its Render, so its node would have no
// edge from the open property and the first Open would schedule no frame.
// The primitive solves that by keeping the surface visible at a zero
// rect. This component needs the same guarantee for the two overlays that
// are NOT the surface, so Render below reads mode unconditionally: that
// read is what turns a mode change into a scheduled frame, and layout
// (which runs every frame) is what moves the overlays.
//
// WHAT IT DOES NOT USE is Popup's focus save/restore, and that is
// deliberate. Popup.Open moves focus to the OWNER; this owner is not a
// focus stop, so the call is a no-op and focus stays on the list — which
// is what keeps the selected row lit while its dropdown is open, exactly
// as a property grid behaves. The keyboard is claimed instead by
// PreviewKey, which tunnels from the root DOWN to the focused component
// and therefore beats both the ItemsView's own arrow handling and the
// page-root KeyBindings on bare letters (q, x, d, esc).
type valueEditor struct {
	gooey.Base

	ed     *editor
	list   *components.ItemsView
	text   *components.TextBox
	cp     *components.ColorPicker
	pop    *components.Popup
	kids   []gooey.Component
	attach []gooey.Component
	mgr    *gooey.FocusManager

	// mode is the editorKind currently open, editNone when closed. A
	// SOURCE PROPERTY, and the only reason it is one is damage: Render
	// reads it, so opening and closing an editor is ordinary paint
	// damage on this one component and the frame it schedules is what
	// re-runs layout and moves the overlays.
	mode *prop.Property[int]
	// pick is the cursor inside whatever is floating — the highlighted
	// option in a dropdown, the selected track in the track editor. A
	// property because the surface's draw func reads it, so moving the
	// cursor repaints the surface and nothing else.
	pick *prop.Property[int]
	// col is the ColorPicker's handle, and it is written back to the
	// document on every change, so a colour edit is live.
	col *prop.Property[render.Color]

	// name is the attribute being edited and body says whether it is the
	// element's body rather than an entry in Attrs.
	//
	// NOT a *node. Undo replaces ed.root wholesale with a fresh deep
	// copy, so every node pointer from the previous state dangles: an
	// editor that captured its target when it opened would write into a
	// detached tree after a ctrl+z and lose the edit silently. The target
	// is re-resolved through ed.target() at every write.
	name string
	body bool
	// on is the node the editor OPENED on, held for identity only and
	// never dereferenced — the distinction that makes it safe to keep a
	// pointer at all under the rule above.
	//
	// It exists because re-resolving the target is necessary and not
	// sufficient. ed.target() answers "what is selected NOW", so if the
	// selection moved while an editor was open, a correct re-resolve
	// lands the write on the WRONG ELEMENT — with the editor still
	// showing the old one's value. That is reachable today: the caret
	// editor is deliberately non-modal so the TextBox can see runes, so
	// a page-root KeyBinding (ctrl+n, which is Next Element) fires
	// underneath it. Measured before the fix: open the caret on a
	// <Button>'s Content, press ctrl+n, type — and the <Text> that ctrl+n
	// selected got Content written to it.
	//
	// Comparing identity also catches the tree being replaced wholesale
	// (an undo restores a deep copy, so every pointer differs), which is
	// the same failure by another route and wants the same answer: the
	// editor is stale, retire it.
	//
	// row is the SAME guard one level down, and it is needed because the
	// element is not the only thing that can move out from under an open
	// editor. The overlay's POSITION comes from the row index — Arrange
	// reads ed.attrSel — while its WRITE TARGET is the attribute name
	// captured here at open. Those diverge whenever the selection moves
	// within the same element, and the caret modes make that reachable
	// with the mouse: they return from Open before pop.Open(nil), so
	// they never capture the pointer, so a press on another attribute row
	// is consumed by the ItemsView, which moves attrSel — and the
	// outside-press dismissal that closes every floated mode never runs.
	// Measured before the fix: open the caret on a <Button>'s Content,
	// click the Chrome row, type — the box visibly relocates onto Chrome
	// and the keystroke is written to Content.
	//
	// Cancel inherits it too, because currentValue reads whichever row is
	// selected NOW, so esc would compare against the wrong row's value.
	on *node
	// row is the attribute row index the editor opened on, -1 when
	// closed. Read the paragraph above for why identity alone is not
	// enough.
	row int
	// undo is the value the row held when the editor opened, for esc.
	undo string
	// tracks is the working copy for the KindGridLens editor.
	tracks []components.GridLen
	// anchor and float are where the row was and where the editor went,
	// recorded in Arrange for the damage and geometry tests. Plain
	// fields: layout bookkeeping must not be damage.
	anchor gooey.Rect
	float  gooey.Rect
}

// ValueEditorBuilder registers <ValueEditor>. It takes exactly two
// children, in order: the list it floats over, and the TextBox it uses
// for caret editing.
//
// Both stay in MARKUP rather than being constructed here, because both
// carry bindings — Items, Selected, Activate on one; Text and Changed on
// the other — and a binding written in Go is a binding no one can see
// from the page. What the component supplies is the geometry and the
// keyboard, which are the parts markup has no way to say.
func ValueEditorBuilder(ed *editor) markup.Builder {
	return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
		kids, attach, err := markup.BuildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		if len(kids) != 2 {
			return nil, fmt.Errorf("markup: <ValueEditor> takes exactly two children — the "+
				"<ItemsView> it floats over and the <TextBox> it edits with — got %d", len(kids))
		}
		list, ok := kids[0].(*components.ItemsView)
		if !ok {
			return nil, fmt.Errorf("markup: <ValueEditor>'s first child must be the "+
				"<ItemsView> it floats over, got %T", kids[0])
		}
		text, ok := kids[1].(*components.TextBox)
		if !ok {
			return nil, fmt.Errorf("markup: <ValueEditor>'s second child must be the "+
				"<TextBox> it edits with, got %T", kids[1])
		}
		return newValueEditor(ed, list, text, attach), nil
	}
}

func newValueEditor(ed *editor, list *components.ItemsView, text *components.TextBox,
	attach []gooey.Component) *valueEditor {

	p := &valueEditor{
		ed: ed, list: list, text: text, attach: attach, row: -1,
		mode: prop.NewSource(int(editNone)),
		pick: prop.NewSource(0),
		col:  prop.NewSource(render.RGB(128, 128, 128)),
	}
	p.cp = &components.ColorPicker{Value: p.col}
	p.pop = components.NewPopup(p, p.draw)
	// The list has to be underneath everything, and being FIRST in
	// document order is what does that — the popup surface is a
	// gooey.Overlay and paints in the layer above the page regardless.
	// Its being LAST of the three only ranks it among overlays, and even
	// that decides nothing here: the three are never open at once. The
	// ordering that matters is list first.
	p.kids = []gooey.Component{list, text, p.cp, p.pop.Surface()}
	ed.props = p
	return p
}

func (p *valueEditor) ChildComponents() []gooey.Component { return p.kids }

// Attachments carries the non-visual children through. A KeyBinding
// written inside <ValueEditor> has to reach the framework, and dropping
// it would be silent.
func (p *valueEditor) Attachments() []gooey.Component { return p.attach }

func (p *valueEditor) SetFocusManager(fm *gooey.FocusManager) {
	p.mgr = fm
	p.pop.SetFocusManager(fm)
}

// Mode is the editor currently open, editNone when closed. Read from a
// Render it is a paint dependency like any other property.
func (p *valueEditor) Mode() editorKind { return editorKind(p.mode.Get()) }

// FloatBounds is where the editor is currently drawn and AnchorBounds is
// the row it belongs to — the two rects the geometry tests compare, and
// the answer to "did the editor actually land on the row the user
// selected". Both are zero while nothing is open.
func (p *valueEditor) FloatBounds() gooey.Rect  { return p.float }
func (p *valueEditor) AnchorBounds() gooey.Rect { return p.anchor }

// LAYOUT.
//
// Measure hands the list everything, which is what it wants — ItemsView
// measures greedily and there is nothing here competing with it. The
// overlays are measured in Arrange, against rects derived from where the
// list actually put its rows, because a row's position is not knowable
// until the list has been arranged.

func (p *valueEditor) Measure(avail gooey.Size) gooey.Size {
	gooey.MeasureChild(p.list, avail)
	return avail
}

func (p *valueEditor) Arrange(b gooey.Rect) {
	p.Base.Arrange(b)
	gooey.ArrangeChild(p.list, b)

	// Reads in layout run outside any evaluation, so none of this
	// records a dependency — the carrier is Render's read of mode.
	mode := editorKind(p.mode.Get())
	row, ok := p.list.RowBounds(p.ed.attrSel.Get())
	// A stale editor is HIDDEN here and retired in PreviewKey/Write —
	// hidden rather than closed because closing Sets a property, and
	// layout must not write to the graph it is being laid out from.
	if mode == editNone || p.stale() || !ok {
		// A row that scrolled out of the window has NO honest rect, so
		// nothing is placed. The alternative — floating the editor at
		// the zero rect — puts a live caret in the pane's top-left
		// corner over a row it does not belong to, which is the silent
		// failure this whole component exists to remove.
		p.hideAll(b)
		return
	}
	p.anchor = row

	switch mode {
	case editCaret, editRename:
		p.float = p.valueCell(row)
		p.show(p.text, p.float)
		p.hide(p.cp, b)
		p.pop.ArrangeSurface(false, b)
	case editColor:
		p.float = components.PlacePopup(row, gooey.Size{W: colorW, H: colorH}, b, components.PopupBelow)
		p.show(p.cp, p.float)
		p.hide(p.text, b)
		p.pop.ArrangeSurface(false, b)
	case editStepper:
		// INLINE, per the rule that a number needs ◂ and ▸ rather than a
		// surface: the popup mechanism is reused, but placed ON the value
		// cell instead of under the row.
		p.float = p.valueCell(row)
		p.hide(p.text, b)
		p.hide(p.cp, b)
		p.pop.ArrangeSurface(true, p.float)
	default:
		p.float = components.PlacePopup(row, p.surfaceSize(), b, components.PopupBelow)
		p.hide(p.text, b)
		p.hide(p.cp, b)
		p.pop.ArrangeSurface(true, p.float)
	}
}

// hideAll retires every overlay. Collapsed rather than zero-sized, and
// that difference is the tab order: FocusManager.move skips a component
// inside a Collapsed subtree, so a closed editor is not a stop the user
// tabs into and finds nothing at. Order() still contains it, which is
// what lets SetFocus reach it in the same event that opens it, before any
// frame has run.
func (p *valueEditor) hideAll(b gooey.Rect) {
	p.float = gooey.Rect{}
	p.anchor = gooey.Rect{}
	p.hide(p.text, b)
	p.hide(p.cp, b)
	p.pop.ArrangeSurface(false, b)
}

func (p *valueEditor) hide(c gooey.Component, b gooey.Rect) {
	if l := gooey.LayoutOf(c); l != nil {
		l.Visibility = gooey.Collapsed
	}
	gooey.ArrangeChild(c, gooey.Rect{X: b.X, Y: b.Y})
}

func (p *valueEditor) show(c gooey.Component, r gooey.Rect) {
	if l := gooey.LayoutOf(c); l != nil {
		l.Visibility = gooey.Visible
	}
	gooey.MeasureChild(c, gooey.Size{W: r.W, H: r.H})
	gooey.ArrangeChild(c, r)
}

// valueCell is where the VALUE lives inside a row, derived from the row's
// own arranged subtree rather than from the column widths written in the
// markup.
//
// Deriving it is the point. The row template's widths are in
// wysiwyg.gooey; a constant here that added them up would be right until
// somebody widened the NAME column, and then the caret would appear one
// cell off with nothing failing. The last leaf of the row IS the value
// cell, because that is what the template's last <Text> is.
func (p *valueEditor) valueCell(row gooey.Rect) gooey.Rect {
	x := row.X
	if lb, ok := valueCellBounds(p.list, p.ed.attrSel.Get()); ok {
		if lb.W > 0 && lb.X >= row.X && lb.X < row.X+row.W {
			x = lb.X
		}
	}
	return gooey.Rect{X: x, Y: row.Y, W: max(1, row.X+row.W-x), H: max(1, row.H)}
}

// valueCellBounds is the rect of the deepest LAST child of realized row i
// — the row template's final cell, which is the value.
//
// The row is found by matching RowBounds rather than by indexing into
// ChildComponents, because the realized window starts at a scroll offset
// this package has no business knowing. Two rows never share a rect, so
// the match is exact.
func valueCellBounds(list *components.ItemsView, i int) (gooey.Rect, bool) {
	rb, ok := list.RowBounds(i)
	if !ok {
		return gooey.Rect{}, false
	}
	for _, k := range list.ChildComponents() {
		b, ok := k.(gooey.Bounded)
		if !ok || b.Bounds() != rb {
			continue
		}
		// Descend through the row's FIRST child, which is the template.
		// A selectable row's LAST child is the view's own selection
		// highlight — an overlay covering the whole row — so taking the
		// row's last child would put the caret back at the row's left
		// edge with nothing failing.
		row, ok := k.(gooey.Container)
		if !ok || len(row.ChildComponents()) == 0 {
			return gooey.Rect{}, false
		}
		leaf, ok := deepestLast(row.ChildComponents()[0]).(gooey.Bounded)
		if !ok {
			return gooey.Rect{}, false
		}
		return leaf.Bounds(), true
	}
	return gooey.Rect{}, false
}

func deepestLast(c gooey.Component) gooey.Component {
	for {
		cont, ok := c.(gooey.Container)
		if !ok {
			return c
		}
		kids := cont.ChildComponents()
		if len(kids) == 0 || kids[len(kids)-1] == nil {
			return c
		}
		c = kids[len(kids)-1]
	}
}

// surfaceSize is how big the floated picture needs to be, per mode. It is
// computed from the CONTENT — the longest option, the number of tracks —
// so a dropdown of two never reserves the height of a dropdown of twenty.
// PlacePopup clamps it into the pane, so an oversized answer degrades to
// a clipped list rather than to a popup off the edge of the screen.
func (p *valueEditor) surfaceSize() gooey.Size {
	switch p.Mode() {
	case editChoice, editBinding:
		opts := p.options()
		w := 0
		for _, o := range opts {
			w = max(w, len([]rune(optionLabel(o))))
		}
		return gooey.Size{W: w + 4, H: len(opts) + 2}
	case editTracks:
		return gooey.Size{W: trackW, H: len(p.tracks) + 4}
	case editGesture:
		return gooey.Size{W: gestureW, H: 4}
	}
	return gooey.Size{}
}

// Sizes that are not derived from content. The colour picker's are the
// component's own geometry (three bars, a blank, a readout), and the two
// widths below are the widest line each surface can draw.
const (
	colorW   = 30
	colorH   = 5
	trackW   = 30
	gestureW = 32
)

// PAINT.
//
// Render paints nothing. What it does is READ mode, unconditionally and
// before anything else, and that read is the whole job: it is the edge
// from the open/close property into this paint node, which is what makes
// opening an editor schedule a frame at all. Two of the three overlays —
// the TextBox and the ColorPicker — are Collapsed while closed and so
// evaluate nothing of their own; without this read, opening one would
// dirty nothing and the editor would appear on whatever frame something
// else happened to schedule.
//
// It is the same problem components.Popup solves for its own surface by
// keeping it at a zero rect, and the reason that trick does not extend to
// these two is the tab order: a zero-rect focus stop is still a stop.
func (p *valueEditor) Render(f *gooey.Frame) {
	p.mode.Get()
}

// draw is the Popup surface: the dropdown, the binding picker, the track
// editor, the chord capture and the stepper. It runs inside the surface's
// own paint node, so every property it reads becomes that node's
// dependency — moving the cursor in a dropdown repaints the dropdown and
// nothing else.
func (p *valueEditor) draw(f *gooey.Frame, b gooey.Rect) {
	// HOISTED ABOVE THE SWITCH, and above any early return, because a
	// dependency is recorded by the Get that actually RUNS.
	//
	// Three of these surfaces show the document's own value — the
	// stepper's number, the track editor's rows, the dropdown's current
	// entry — and they read it through ed.attrRows(), which is plain Go
	// state and therefore invisible to the property graph. A computed
	// that reads no property records no dependency and caches forever.
	// ed.rev ticks on every edit, so this read is what makes a live
	// write (the stepper's ▸, the track editor's k) repaint the surface
	// showing it.
	//
	// HONESTY ABOUT WHAT PINS IT: nothing does, today, and the reason is
	// worth recording rather than papering over. A commit ends in
	// ed.rebuild(), which currently dirties the page root — 19
	// components on a shell of 264, measured — and once the root
	// repaints the z-ordered pass carries every overlay above it. So a
	// surface with NO subscription still shows the right value, and a
	// damage assertion cannot tell the two apart.
	//
	// That makes this read insurance rather than a mechanism the suite
	// observes: it is correct on its own terms, and it is what keeps
	// these surfaces correct the day rebuild() gets cheaper — which is
	// a change somebody will make for good reasons, with nothing to
	// warn them that three editors were living off its side effect.
	// TestAFloatedSurfaceFollowsTheDocumentItIsShowing asserts the
	// OUTCOME (the surface shows the current value), which is what a
	// user can see; it does not and cannot assert the route.
	p.ed.rev.Get()

	st := p.ed.ctx.Styles["sel"]
	switch p.Mode() {
	case editStepper:
		p.drawStepper(f, b)
	case editChoice, editBinding:
		p.drawOptions(f, b, st)
	case editTracks:
		p.drawTracks(f, b, st)
	case editGesture:
		p.drawGesture(f, b, st)
	}
}

func (p *valueEditor) drawStepper(f *gooey.Frame, b gooey.Rect) {
	st := p.ed.ctx.Styles["ok"]
	st.Reverse = true
	line := fmt.Sprintf("◂ %s ▸", p.currentValue())
	f.Cells.SetString(b.X, b.Y, pad(line, b.W), st)
}

func (p *valueEditor) drawOptions(f *gooey.Frame, b gooey.Rect, st render.Style) {
	components.DrawBoxRunes(f.Cells, b, st)
	opts := p.options()
	sel := clampInt(p.pick.Get(), 0, max(0, len(opts)-1))
	for i, o := range opts {
		y := b.Y + 1 + i
		if y >= b.Y+b.H-1 {
			break
		}
		is := st
		is.Bold = false
		if i == sel {
			is.Reverse = true
		}
		f.Cells.SetString(b.X+1, y, pad(" "+optionLabel(o), b.W-2), is)
	}
}

func (p *valueEditor) drawTracks(f *gooey.Frame, b gooey.Rect, st render.Style) {
	components.DrawBoxRunes(f.Cells, b, st)
	sel := clampInt(p.pick.Get(), 0, max(0, len(p.tracks)-1))
	for i, l := range p.tracks {
		y := b.Y + 1 + i
		if y >= b.Y+b.H-2 {
			break
		}
		is := st
		is.Bold = false
		if i == sel {
			is.Reverse = true
		}
		f.Cells.SetString(b.X+1, y, pad(fmt.Sprintf(" %d  %-5s %s", i, lensKind(l), lensText(l)), b.W-2), is)
	}
	hint := p.ed.ctx.Styles["dim"]
	f.Cells.SetString(b.X+1, b.Y+b.H-2, pad(" ◂▸ size  k kind  a add  x del", b.W-2), hint)
}

func (p *valueEditor) drawGesture(f *gooey.Frame, b gooey.Rect, st render.Style) {
	components.DrawBoxRunes(f.Cells, b, st)
	// NO "here is what I caught" BRANCH, and there cannot be one.
	// captureGesture writes the chord and Closes in the same event, so
	// no frame is ever composed with a captured value to show — the
	// field this used to read was set and cleared between renders and
	// the branch was dead code that read as a feature. The committed
	// chord is visible where it belongs, in the row. Found in review
	// of #388.
	f.Cells.SetString(b.X+1, b.Y+1, pad(" press the chord you want", b.W-2), st)
	f.Cells.SetString(b.X+1, b.Y+2, pad(" esc cancels", b.W-2), p.ed.ctx.Styles["dim"])
}

// optionLabel renders the empty string — UNSET — as a word rather than as
// a blank line, because a dropdown whose first entry is invisible reads
// as a rendering bug.
func optionLabel(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func pad(s string, w int) string {
	r := []rune(s)
	if w <= 0 {
		return ""
	}
	if len(r) >= w {
		return string(r[:w])
	}
	return s + strings.Repeat(" ", w-len(r))
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// OPENING AND CLOSING.

// options is what a floated list is choosing between: the row's cycle,
// which is its finite value set preceded by UNSET where unset is legal.
// Same list, same source, as the value the loader will accept.
func (p *valueEditor) options() []string {
	r, ok := p.ed.selectedRow()
	if !ok {
		return nil
	}
	return r.cycle()
}

func (p *valueEditor) currentValue() string {
	r, ok := p.ed.selectedRow()
	if !ok {
		return ""
	}
	return r.value
}

// Open is enter on a row, and it DISPATCHES BY KIND — which is what
// "behave like the Visual Studio property grid" decomposes into.
//
// There is no default arm. A Kind the table does not cover reports
// itself on the status line rather than falling into a text box, because
// a text box is how the editor comes to offer a value the loader
// rejects.
func (p *valueEditor) Open() {
	r, ok := p.ed.selectedRow()
	if !ok {
		return
	}
	e, ok := r.editorFor()
	if !ok {
		p.ed.status.Set(fmt.Sprintf("✗ no editor for kind %q (%s): the per-Kind table in "+
			"editors.go has no entry for it", r.kind, r.name))
		return
	}
	p.name, p.body, p.undo = r.name, r.body, r.value
	p.on, p.row = p.ed.sel, p.ed.attrSel.Get()
	p.ed.editName.Set(r.name)
	p.ed.editValue.Set(r.value)
	p.ed.describe(r)

	switch e {
	case editCaret, editRename:
		p.mode.Set(int(e))
		if e == editRename {
			// THE WARNING editors.go PROMISES, which was not being
			// emitted. editRename behaved identically to editCaret in
			// all three places that branch on it, so the constant
			// recorded a decision the code never made and its doc
			// described a behaviour nothing implemented — the worst of
			// the three states, because a reader believes it.
			//
			// markup.KindIdentity's own doc says a consumer "must decide
			// what a rename means rather than defaulting to a text box".
			// The decision here IS a text box, which is fine; saying so
			// out loud is what separates a decision from an oversight,
			// and the user is the one who needs to hear it because
			// nothing else will tell them what the rename broke until
			// the document fails to load. Found in review of #388.
			p.ed.status.Set("⚠ " + r.name + " is this element's ADDRESS — " +
				"renaming it breaks every binding, handler and Find that " +
				"names the old one")
		}
		if p.mgr != nil {
			p.mgr.SetFocus(p.text)
			p.text.SetCaret(len([]rune(r.value)))
		}
		return
	case editChoice, editBinding:
		p.pick.Set(indexOf(r.cycle(), r.value))
	case editStepper:
		p.pick.Set(0)
	case editColor:
		c, err := paint.ParseColor(r.value)
		if err != nil {
			c = render.RGB(128, 128, 128)
		}
		p.col.Set(c)
	case editTracks:
		ls, err := components.ParseGridLens(r.value)
		if err != nil || len(ls) == 0 {
			ls = []components.GridLen{components.Star(1)}
		}
		p.tracks = ls
		p.pick.Set(0)
	case editGesture:
		p.pick.Set(0)
	}
	p.mode.Set(int(e))
	// nil restore: focus never left the list, so there is nothing to give
	// back. See the type comment.
	p.pop.Open(nil)
}

// OpenAsText is the escape hatch, reachable from every row: the raw value
// in a caret editor whatever the Kind. A per-Kind editor must not be the
// only way in — KindStyle and KindCommand are BindsEither, so their
// finite lists are the common case and not the whole grammar.
func (p *valueEditor) OpenAsText() {
	r, ok := p.ed.selectedRow()
	if !ok {
		return
	}
	p.name, p.body, p.undo = r.name, r.body, r.value
	p.on, p.row = p.ed.sel, p.ed.attrSel.Get()
	p.ed.editName.Set(r.name)
	p.ed.editValue.Set(r.value)
	p.ed.describe(r)
	p.mode.Set(int(editCaret))
	if p.mgr != nil {
		p.mgr.SetFocus(p.text)
		p.text.SetCaret(len([]rune(r.value)))
	}
}

// stale reports that the element this editor opened on is no longer the
// selected one — so anything it commits would land somewhere the user is
// not looking. Identity only; p.on is never dereferenced.
//
// A plain read, safe from layout as well as from an event handler.
func (p *valueEditor) stale() bool {
	if p.on == nil {
		return false
	}
	return p.ed.sel != p.on || p.ed.attrSel.Get() != p.row
}

// retire abandons a stale editor: close it, and FORGET the pending edit
// rather than committing or restoring it.
//
// Not Cancel, and the difference is the whole point. Cancel puts back the
// value the row held when the editor opened — but that value belongs to
// an element that is no longer selected, so writing it now would corrupt
// whatever the selection moved TO. There is nothing safe to do with a
// pending edit whose subject has gone; dropping it is the only honest
// answer, and the document keeps whatever was already committed to the
// original element.
func (p *valueEditor) retire() {
	p.on, p.row = nil, -1
	p.name, p.body, p.undo = "", false, ""
	p.Close()
}

// Close retires the editor and hands the keyboard back to the list.
func (p *valueEditor) Close() {
	p.on, p.row = nil, -1
	p.name, p.body = "", false
	if p.Mode() == editNone {
		return
	}
	p.mode.Set(int(editNone))
	p.pop.Dismiss()
	if p.mgr != nil && p.mgr.Focused() == p.text {
		p.mgr.SetFocus(p.list)
	}
}

// Cancel is esc: close, and put back what the row held when it opened.
func (p *valueEditor) Cancel() {
	if p.Mode() == editNone {
		return
	}
	if p.currentValue() != p.undo {
		p.Write(p.undo)
	}
	p.ed.editValue.Set(p.undo)
	p.Close()
}

// Write is THE MUTATION SEAM for the property browser: every value this
// pane commits goes through here, and nothing else in it touches the
// document.
//
// Two things it guarantees, both of which were mistakes waiting to happen
// in the code it replaces:
//
// THE TARGET IS RE-RESOLVED, never cached. Undo replaces ed.root wholesale
// with a fresh deep copy, so a *node captured when the editor opened
// dangles after a ctrl+z: the write lands in a detached tree and is lost
// with no error anywhere.
//
// IT ENDS IN ed.rebuild(), which is the choke point undo hooks — an edit
// that skips it is invisible to undo, to the preview, to the outline and
// to the CODE tab. The body is a FIELD rather than a map entry, so ""
// clears it by assignment rather than by delete, and that is chosen by
// the row's FLAG rather than by comparing the name: an element that ever
// grew a real attribute spelled like BodyRowName would otherwise write to
// the wrong place.
func (p *valueEditor) Write(v string) {
	// THE GUARD THAT MATTERS. Re-resolving is necessary and not
	// sufficient: ed.target() answers "what is selected NOW", so once
	// the selection has moved a correct re-resolve puts this write on the
	// wrong element. Refuse, and retire the editor that no longer has a
	// subject.
	if p.stale() {
		p.retire()
		return
	}
	_, _, target := p.ed.target()
	if target == nil || p.name == "" {
		return
	}
	switch {
	case p.body:
		target.Body = v
	case v == "":
		delete(target.Attrs, p.name)
	default:
		target.Attrs[p.name] = v
	}
	p.ed.rebuild()
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return 0
}

// THE KEYBOARD.
//
// PreviewKey rather than HandleKey, and the difference decides whether
// any of this works. Dispatch TUNNELS from the root down to the focused
// component before it bubbles, so a preview handler on this component
// sees a key BEFORE the ItemsView's own arrow handling and before the
// page-root KeyBindings — which are bare letters (q quits, x deletes, d
// toggles mode) and would otherwise fire while the user is picking a
// track kind. A bubble-phase handler could not have either: the list is
// focused, so it would consume the arrows first.
//
// While nothing is open this returns false for everything, so the pane
// behaves exactly as it did.
func (p *valueEditor) PreviewKey(ev input.KeyEvent) bool {
	mode := p.Mode()
	if mode == editNone {
		return false
	}
	// The element moved out from under this editor. Drop it and let the
	// key through — the user is somewhere else now, and swallowing keys
	// on behalf of an editor that is no longer on screen would be a dead
	// keyboard with nothing to explain it.
	if p.stale() {
		p.retire()
		return false
	}
	if mode == editGesture {
		return p.captureGesture(ev)
	}
	switch ev {
	case input.Named(input.KeyEsc):
		p.Cancel()
		return true
	case input.Named(input.KeyEnter):
		p.commit(mode)
		return true
	}
	switch mode {
	case editCaret, editRename:
		// The caret owns everything else — the TextBox is focused and
		// commits live through its Changed binding. NOT swallowed: the
		// TextBox has to see the runes, and being focused it already
		// consumes them before any page-root binding.
		return false
	case editStepper:
		p.stepperKey(ev)
	case editChoice, editBinding:
		p.listKey(ev)
	case editColor:
		p.colorKey(ev)
	case editTracks:
		p.trackKey(ev)
	}
	// MODAL, and this is the load-bearing return.
	//
	// Every mode above holds the keyboard while it is open, so a key it
	// does not understand must be SWALLOWED rather than allowed to keep
	// going. The page root binds bare letters — q quits, x deletes the
	// selected element, d flips design/live — and none of those may fire
	// while the user is picking a track kind or a colour channel.
	// Returning the handler's own verdict instead would let exactly that
	// happen, and `x` would delete the element being edited.
	return true
}

// commit is enter, per mode. The live modes have already written; the
// list modes write the option under the cursor.
func (p *valueEditor) commit(mode editorKind) {
	switch mode {
	case editChoice, editBinding:
		opts := p.options()
		if len(opts) > 0 {
			p.Write(opts[clampInt(p.pick.Get(), 0, len(opts)-1)])
		}
	}
	p.Close()
}

func (p *valueEditor) listKey(ev input.KeyEvent) bool {
	n := len(p.options())
	if n == 0 {
		return false
	}
	switch ev {
	case input.Named(input.KeyUp):
		p.pick.Set((p.pick.Get() - 1 + n) % n)
		return true
	case input.Named(input.KeyDown):
		p.pick.Set((p.pick.Get() + 1) % n)
		return true
	}
	return false
}

// stepperKey is ◂ and ▸ on a number, written back on every press so the
// document follows the key rather than waiting for enter.
func (p *valueEditor) stepperKey(ev input.KeyEvent) bool {
	d := 0
	switch ev {
	case input.Named(input.KeyLeft), input.Rune('-'):
		d = -1
	case input.Named(input.KeyRight), input.Rune('+'):
		d = 1
	case input.Named(input.KeyUp):
		d = 10
	case input.Named(input.KeyDown):
		d = -10
	default:
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(p.currentValue()))
	if err != nil {
		n = 0
	}
	p.Write(strconv.Itoa(n + d))
	return true
}

// colorKey forwards to the ColorPicker's own key handling and then writes
// the hex back — so the channel bars, the pixel tier and the readout are
// the component's, and the document follows every press.
//
// Lower case, because "#rrggbb" is the spelling the rest of the repo
// writes a colour in and a value that is displayed must be pasteable back
// into an attribute.
func (p *valueEditor) colorKey(ev input.KeyEvent) bool {
	if !p.cp.HandleKey(ev) {
		return false
	}
	p.Write(strings.ToLower(p.cp.Hex()))
	return true
}

// trackKey is the grid-track editor, and the reason it writes on EVERY
// keystroke rather than on enter is the whole point of it existing.
// Editing Rows="1,1*,1" as text means typing a spec whose effect you
// cannot see; beside a drawn grid overlay, the number and the space it
// produces are on screen together, and that only works if the document
// follows the key.
func (p *valueEditor) trackKey(ev input.KeyEvent) bool {
	n := len(p.tracks)
	if n == 0 {
		return false
	}
	i := clampInt(p.pick.Get(), 0, n-1)
	switch ev {
	case input.Named(input.KeyUp):
		p.pick.Set((i - 1 + n) % n)
		return true
	case input.Named(input.KeyDown):
		p.pick.Set((i + 1) % n)
		return true
	case input.Named(input.KeyLeft), input.Rune('-'):
		return p.adjustTrack(i, -1)
	case input.Named(input.KeyRight), input.Rune('+'):
		return p.adjustTrack(i, 1)
	case input.Rune('k'):
		p.tracks[i] = nextLensKind(p.tracks[i])
		p.Write(lensSpec(p.tracks))
		return true
	case input.Rune('a'):
		p.tracks = append(p.tracks, components.Star(1))
		p.pick.Set(len(p.tracks) - 1)
		p.Write(lensSpec(p.tracks))
		return true
	case input.Rune('x'):
		// A grid with no tracks is a grid with one star track
		// (components.Grid's own default), so removing the last one
		// would be a no-op the user reads as a broken key. Refuse it and
		// say why.
		if n == 1 {
			p.ed.status.Set("✗ a grid needs at least one track")
			return true
		}
		p.tracks = append(p.tracks[:i:i], p.tracks[i+1:]...)
		p.pick.Set(clampInt(i, 0, len(p.tracks)-1))
		p.Write(lensSpec(p.tracks))
		return true
	}
	return false
}

func (p *valueEditor) adjustTrack(i, d int) bool {
	l, moved := adjustLens(p.tracks[i], d)
	if !moved {
		p.ed.status.Set("✗ an Auto track has no size — press k to make it Fixed or Star")
		return true
	}
	p.tracks[i] = l
	p.Write(lensSpec(p.tracks))
	return true
}

// captureGesture takes the chord the user actually pressed rather than
// asking them to spell it. input.KeyEvent.String and input.ParseGesture
// round-trip, so what is captured is what the loader will parse.
//
// Esc is the one key it cannot capture, because esc is how you get out.
// The caret escape hatch reaches the value for anyone who genuinely needs
// to bind it.
func (p *valueEditor) captureGesture(ev input.KeyEvent) bool {
	if ev == input.Named(input.KeyEsc) {
		p.Cancel()
		return true
	}
	g := ev.String()
	if _, err := input.ParseGesture(g); err != nil {
		p.ed.status.Set("✗ that key has no gesture spelling: " + err.Error())
		return true
	}
	p.Write(g)
	p.Close()
	return true
}

// MOUSE. The list's own hit-testing still owns the rows; this is only the
// popup's dismissal grammar — a press outside what the surface claimed
// closes the editor and is consumed, so it does not also activate
// whatever was underneath.
func (p *valueEditor) HandleMouse(ev input.MouseEvent) bool {
	if p.Mode() == editNone {
		return false
	}
	if ev.Kind == input.MousePress {
		if !inRect(p.float, ev.X, ev.Y) {
			p.Close()
		}
		// A PRESS INSIDE IS CONSUMED AND CHANGES NOTHING, and it must not
		// reach Popup.HandleMouse, which dismisses on ANY press rather
		// than only on one outside itself. Delegating it tore the
		// editor's two halves apart: the popup closed, p.mode stayed
		// set, and PreviewKey gates on the mode alone — so an invisible
		// editor went on swallowing esc, enter and every arrow with
		// nothing on screen to explain why. stale() does not cover it
		// either, since the row is still exactly where it was.
		//
		// Consumed rather than passed through, because the rows keep
		// their own hit-testing: a press that lands on one never reaches
		// here, so anything that does hit the editor's chrome, and
		// chrome is not a dismissal. Found in review of #388.
		return true
	}
	return p.pop.HandleMouse(ev)
}

func inRect(r gooey.Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}
