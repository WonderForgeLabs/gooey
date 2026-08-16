package main

// The source picker: `b` opens an overlay listing every checkout the
// browser can resolve demos against (source.go), enter switches to the
// selection, esc puts everything back.
//
// It is built on the MenuBar overlay recipe, because that recipe is the
// house answer to "modal list above the page":
//
//   - Document order is z-order, so the picker is the Grid's LAST child;
//     its popup paints above both panes and the Composer's restore pass
//     repaints whatever it covered when it closes.
//   - The picker itself is a CONTAINER (never pre-clears) spanning the
//     page, Collapsed while closed — which is what keeps it out of tab
//     order (FocusManager.move skips Collapsed subtrees) and out of
//     hit-testing (hitTest returns nil at a Collapsed node). The popup
//     child is the LEAF, so its pre-clear paints exactly the box.
//   - While open, keys are modal: the picker takes focus (it is in the
//     focus order regardless of visibility — the walk asks AcceptsFocus,
//     not Visibility) and swallows everything it does not handle, so `q`
//     cannot quit the app under an open picker. The pointer is captured,
//     MenuBar-style: clicks on rows choose, clicks outside dismiss, and
//     the popup would otherwise be unreachable anyway — capture routes
//     events that hit-testing never sees.
//   - Dismissing restores focus to whatever had it at open.

import (
	"fmt"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type sourcePicker struct {
	gooey.Base
	gooey.FocusState

	// choose is called with the picked source after the popup has
	// dismissed — the switch itself belongs to main, not the widget.
	choose func(source)

	mgr     *gooey.FocusManager
	popup   *sourcePopup
	kids    []gooey.Component
	restore gooey.Component

	openP *prop.Property[bool]
	selP  *prop.Property[int]      // index into srcsP, never into rows
	srcsP *prop.Property[[]source] // what the open popup lists
	curID string                   // id() of the active source, for the ● marker
}

func newSourcePicker(choose func(source)) *sourcePicker {
	p := &sourcePicker{
		choose: choose,
		openP:  prop.NewSource(false),
		selP:   prop.NewSource(0),
		srcsP:  prop.NewSource([]source(nil)),
	}
	p.popup = &sourcePopup{picker: p}
	p.kids = []gooey.Component{p.popup}
	p.LayoutProps().Visibility = gooey.Collapsed
	return p
}

// SetFocusManager receives the input tree (gooey.FocusHost) — the seam
// focus restore and pointer capture go through.
func (p *sourcePicker) SetFocusManager(fm *gooey.FocusManager) { p.mgr = fm }

func (p *sourcePicker) ChildComponents() []gooey.Component { return p.kids }

// HitTestTransparent: the picker spans the page invisibly; only its
// popup child owns cells. Moot while open (capture routes everything
// here), load-bearing the frame it is dismissed on.
func (p *sourcePicker) HitTestTransparent() bool { return true }

func (p *sourcePicker) IsOpen() bool { return p.openP.Get() }

// Open shows the popup over srcs, with the active source (by id)
// pre-selected and marked. Runs on the UI goroutine, from a command.
func (p *sourcePicker) Open(srcs []source, curID string) {
	p.curID = curID
	sel := 0
	for i, s := range srcs {
		if s.id() == curID {
			sel = i
			break
		}
	}
	p.srcsP.Set(srcs)
	p.selP.Set(sel)
	p.openP.Set(true)
	p.LayoutProps().Visibility = gooey.Visible
	if p.mgr != nil {
		p.restore = p.mgr.Focused()
		p.mgr.SetFocus(p)
		p.mgr.CaptureMouse(p)
	}
}

// Dismiss closes the popup, releases the pointer, and hands focus back.
func (p *sourcePicker) Dismiss() {
	if !p.IsOpen() {
		return
	}
	p.openP.Set(false)
	p.LayoutProps().Visibility = gooey.Collapsed
	if p.mgr != nil {
		if p.mgr.Captured() == gooey.Component(p) {
			p.mgr.ReleaseCapture()
		}
		if p.restore != nil && p.mgr.Focused() == gooey.Component(p) {
			p.mgr.SetFocus(p.restore)
		}
	}
	p.restore = nil
}

func (p *sourcePicker) Measure(avail gooey.Size) gooey.Size { return avail }

// Arrange centers the popup, sized to its rows and clamped to the page.
// The open flag is read here in layout — a plain read, recorded nowhere.
func (p *sourcePicker) Arrange(r gooey.Rect) {
	p.Base.Arrange(r)
	l := p.popup.LayoutProps()
	if !p.openP.Get() {
		l.Visibility = gooey.Collapsed
		gooey.ArrangeChild(p.popup, gooey.Rect{X: r.X, Y: r.Y, W: 0, H: 0})
		return
	}
	l.Visibility = gooey.Visible
	pr := p.popupRect(r)
	gooey.MeasureChild(p.popup, gooey.Size{W: pr.W, H: pr.H})
	gooey.ArrangeChild(p.popup, pr)
}

func (p *sourcePicker) popupRect(page gooey.Rect) gooey.Rect {
	rows := sourceRows(p.srcsP.Get())
	w := len([]rune(" sources ")) + 4
	for _, row := range rows {
		if rw := len([]rune(row.text(p.curID))) + 4; rw > w {
			w = rw
		}
	}
	w = min(w, page.W-2)
	h := min(len(rows)+2, page.H-2)
	if w < 8 {
		w = min(8, page.W)
	}
	if h < 3 {
		h = min(3, page.H)
	}
	return gooey.Rect{X: page.X + (page.W-w)/2, Y: page.Y + max(0, (page.H-h)/3), W: w, H: h}
}

func (p *sourcePicker) Render(*gooey.Frame) {}

// selectable index step: the selection indexes the source slice, so
// header rows never need skipping — same two-coordinate rule as the
// demo list.
func (p *sourcePicker) moveSel(d int) {
	n := len(p.srcsP.Get())
	if n == 0 {
		return
	}
	if i := clampIdx(p.selP.Get()+d, n); i != p.selP.Get() {
		p.selP.Set(i)
	}
}

func (p *sourcePicker) activate() {
	srcs := p.srcsP.Get()
	if len(srcs) == 0 {
		p.Dismiss()
		return
	}
	s := srcs[clampIdx(p.selP.Get(), len(srcs))]
	p.Dismiss()
	if p.choose != nil {
		p.choose(s)
	}
}

// HandleKey while open is modal: what it does not handle it swallows, so
// the page's own gestures (q!) cannot fire under the popup.
func (p *sourcePicker) HandleKey(ev input.KeyEvent) bool {
	if !p.IsOpen() {
		return false
	}
	switch ev {
	case input.Rune('j'), input.Named(input.KeyDown):
		p.moveSel(+1)
	case input.Rune('k'), input.Named(input.KeyUp):
		p.moveSel(-1)
	case input.Named(input.KeyHome):
		p.moveSel(-len(p.srcsP.Get()))
	case input.Named(input.KeyEnd):
		p.moveSel(+len(p.srcsP.Get()))
	case input.Named(input.KeyEnter):
		p.activate()
	case input.Named(input.KeyEsc), input.Rune('b'), input.Rune('q'):
		p.Dismiss()
	}
	return true
}

// HandleMouse sees every pointer event while open (capture). Clicks on a
// source row choose it; a click anywhere else dismisses without
// activating what is underneath — the pointer never reaches it.
func (p *sourcePicker) HandleMouse(ev input.MouseEvent) bool {
	if !p.IsOpen() {
		return false
	}
	switch ev.Kind {
	case input.WheelUp:
		p.moveSel(-1)
		return true
	case input.WheelDown:
		p.moveSel(+1)
		return true
	case input.MousePress:
		return true
	case input.MouseClick:
		if i, ok := p.popup.sourceAt(ev.X, ev.Y); ok {
			p.selP.Set(i)
			p.activate()
			return true
		}
		b := p.popup.Bounds()
		if ev.X >= b.X && ev.X < b.X+b.W && ev.Y >= b.Y && ev.Y < b.Y+b.H {
			return true // popup furniture: border, header row
		}
		p.Dismiss()
		return true
	}
	return true
}

// sourceRow mirrors the demo list's row model: group headers interleaved
// with entries, selection indexing the source slice only.
type sourceRow struct {
	header string
	src    int // index into the sources; -1 for a header
	s      source
}

func (r sourceRow) text(curID string) string {
	if r.src < 0 {
		return r.header
	}
	mark := "  "
	if r.s.id() == curID {
		mark = "● "
	}
	name := r.s.Name
	if r.s.Dirty {
		name += " *"
	}
	if r.s.Head != "" {
		return fmt.Sprintf("%s%s — %s", mark, name, r.s.Head)
	}
	return mark + name
}

func sourceRows(ss []source) []sourceRow {
	var out []sourceRow
	group := ""
	for i, s := range ss {
		g := "worktrees"
		if s.Root == "" && !s.Ephemeral {
			g = "branches"
		}
		if g != group {
			group = g
			out = append(out, sourceRow{header: g, src: -1})
		}
		out = append(out, sourceRow{src: i, s: s})
	}
	return out
}

// sourcePopup is the box: a leaf, so its paint node pre-clears exactly
// the popup rectangle — that is what makes it an overlay rather than a
// see-through frame over the panes below.
type sourcePopup struct {
	gooey.Base
	picker *sourcePicker
	top    int // first visible row, kept so the selection stays in view
}

func (pp *sourcePopup) Measure(avail gooey.Size) gooey.Size { return avail }

// rowsWindow is which rows are visible: the popup keeps a scroll window
// like the ItemsView's, small enough to hand-roll — sources are dozens,
// not thousands.
func (pp *sourcePopup) rowsWindow(rows []sourceRow, sel, h int) int {
	// Row index of the selected source.
	selRow := 0
	for i, r := range rows {
		if r.src == sel {
			selRow = i
			break
		}
	}
	top := pp.top
	if selRow < top {
		top = selRow
	}
	if selRow >= top+h {
		top = selRow - h + 1
	}
	pp.top = clampIdx(top, max(1, len(rows)-h+1))
	return pp.top
}

func (pp *sourcePopup) Render(f *gooey.Frame) {
	// The open flag is read FIRST and unconditionally: a dependency is
	// recorded by the Get that actually runs, and this node's
	// subscription to openP is what turns a Dismiss into a scheduled
	// frame (whose bounds sweep then collapses the popup and restores
	// what it covered). The app-side half of the same guarantee is the
	// hint computed reading IsOpen — see main.go — which covers the
	// FIRST open, before this node has ever evaluated.
	pp.picker.openP.Get()
	b := pp.Bounds()
	if b.W < 2 || b.H < 2 {
		return
	}
	st := render.Style{Fg: render.RGB(120, 90, 220)}
	components.DrawBoxRunes(f.Cells, b, st)
	// The heading takes `accent` while the chrome above takes `st` —
	// which is exactly why DrawBoxTitle takes its own style rather than
	// being a parameter of DrawBoxRunes. popupRect's floor of
	// len(" sources ")+4 == 13 is the same budget the helper enforces
	// (W-6 >= 7), so at the popup's natural width this writes precisely
	// what the hand-rolled SetString did; when page.W-2 squeezes the
	// popup narrower, the helper clips instead of writing past the far
	// corner and out of this node's damage rect.
	components.DrawBoxTitle(f.Cells, b, "sources", accent)

	srcs := pp.picker.srcsP.Get()
	rows := sourceRows(srcs)
	if len(rows) == 0 {
		f.Cells.SetString(b.X+2, b.Y+1, clip("no sources — not a git repository?", b.W-3), dim)
		return
	}
	sel := clampIdx(pp.picker.selP.Get(), len(srcs))
	h := b.H - 2
	top := pp.rowsWindow(rows, sel, h)
	for y := 0; y < h && top+y < len(rows); y++ {
		r := rows[top+y]
		if r.src < 0 {
			f.Cells.SetString(b.X+2, b.Y+1+y, clip(r.header, b.W-3), dim)
			continue
		}
		st := render.Style{}
		if r.src == sel {
			st.Reverse = true
			for x := b.X + 1; x < b.X+b.W-1; x++ {
				f.Cells.Set(x, b.Y+1+y, ' ', st)
			}
		}
		label := r.text(pp.picker.curID)
		if r.s.Root == "" && !r.s.Ephemeral {
			f.Cells.SetString(b.X+2, b.Y+1+y, clip(label, b.W-3), st)
			continue
		}
		// Worktree rows: the name in full strength, the subject dimmed —
		// unless selected, where Reverse already carries the emphasis.
		if st.Reverse {
			f.Cells.SetString(b.X+2, b.Y+1+y, clip(label, b.W-3), st)
			continue
		}
		name, subject, cut := strings.Cut(label, " — ")
		f.Cells.SetString(b.X+2, b.Y+1+y, clip(name, b.W-3), st)
		if cut {
			if x := b.X + 2 + len([]rune(name)); x < b.X+b.W-1 {
				f.Cells.SetString(x, b.Y+1+y, clip(" — "+subject, b.X+b.W-1-x), dim)
			}
		}
	}
}

// sourceAt maps a screen cell to a source index through the same window
// Render painted — clicks land on what the user sees.
func (pp *sourcePopup) sourceAt(x, y int) (int, bool) {
	b := pp.Bounds()
	if x <= b.X || x >= b.X+b.W-1 || y <= b.Y || y >= b.Y+b.H-1 {
		return 0, false
	}
	rows := sourceRows(pp.picker.srcsP.Get())
	i := pp.top + (y - b.Y - 1)
	if i < 0 || i >= len(rows) || rows[i].src < 0 {
		return 0, false
	}
	return rows[i].src, true
}
