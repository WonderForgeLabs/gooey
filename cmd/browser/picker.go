package main

// The source picker: `b` opens an overlay listing every checkout the
// browser can resolve demos against (source.go), enter switches to the
// selection, esc puts everything back.
//
// The overlay MECHANICS are not here any more. components.Popup was
// extracted from four hand-rolled copies of them, and this file was one
// of the four — its doc comment says so by name. What the primitive now
// owns, and this file no longer spells out:
//
//   - the open property, and the SURFACE that carries its subscription.
//     A Collapsed component is not paintable (layout.go:224), so a
//     closed popup that collapses itself never runs its Render, never
//     reads the open property, and the first Open schedules no frame.
//     The old fix was to make an always-painted node read IsOpen — the
//     `hint` computed in main.go still says so. The primitive's surface
//     stays Visible and is arranged to a ZERO RECT instead, so its node
//     evaluates from frame one and no external carrier is needed.
//   - focus save/restore and pointer capture at open and dismiss;
//   - the modal key grammar: esc dismisses, and everything this file's
//     HandleKey declines is swallowed rather than bubbled, so `q` cannot
//     quit the app under an open picker.
//
// What stays here is the part that is about SOURCES: the row model, the
// scroll window, the box's own drawing, and the mouse grammar. The
// picker keeps its own Visibility — Collapsed while closed — because
// that is what keeps it out of tab order (FocusManager.move skips
// Collapsed subtrees; SetFocus does not, which is how the primitive can
// still focus it). It is bound to the popup's open state rather than
// assigned, so a dismissal the primitive performs on its own — esc,
// a press outside — collapses the picker in the same frame.

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

	pop  *components.Popup
	kids []gooey.Component

	selP  *prop.Property[int]      // index into srcsP, never into rows
	srcsP *prop.Property[[]source] // what the open popup lists
	curID string                   // id() of the active source, for the ● marker
	top   int                      // first visible row, kept so the selection stays in view
}

func newSourcePicker(choose func(source)) *sourcePicker {
	p := &sourcePicker{
		choose: choose,
		selP:   prop.NewSource(0),
		srcsP:  prop.NewSource([]source(nil)),
	}
	p.pop = components.NewPopup(p, p.drawPopup)
	p.pop.Modal = true // an open picker swallows what it does not understand
	// Document order is z-order and the surface is the LAST (only) child,
	// so the box paints above both panes; the Composer's restore pass
	// repaints what it covered when it goes away.
	p.kids = []gooey.Component{p.pop.Surface()}
	p.LayoutProps().BindVisibilityFunc(func() gooey.Visibility {
		if p.pop.IsOpen() {
			return gooey.Visible
		}
		return gooey.Collapsed
	})
	return p
}

// SetFocusManager receives the input tree (gooey.FocusHost) — the seam
// focus restore and pointer capture go through. It belongs to the popup.
func (p *sourcePicker) SetFocusManager(fm *gooey.FocusManager) { p.pop.SetFocusManager(fm) }

func (p *sourcePicker) ChildComponents() []gooey.Component { return p.kids }

// HitTestTransparent: the picker spans the page invisibly; only its
// surface owns cells. Moot while open (capture routes everything here),
// load-bearing the frame it is dismissed on.
func (p *sourcePicker) HitTestTransparent() bool { return true }

func (p *sourcePicker) IsOpen() bool { return p.pop.IsOpen() }

// Open shows the popup over srcs, with the active source (by id)
// pre-selected and marked. Runs on the UI goroutine, from a command.
//
// The restore target is whatever holds focus right now — the demo list,
// since `b` is a key binding. Popup's key-open convention (pass nil) is
// for an owner that already had focus; this one does not.
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
	var restore gooey.Component
	if m := p.pop.Manager(); m != nil {
		restore = m.Focused()
	}
	p.pop.Open(restore)
}

// Dismiss closes the popup, releases the pointer, and hands focus back.
func (p *sourcePicker) Dismiss() { p.pop.Dismiss() }

func (p *sourcePicker) Measure(avail gooey.Size) gooey.Size { return avail }

// Arrange centers the surface, sized to its rows and clamped to the
// page. Reads here happen in layout, outside any evaluation, so they
// record no dependencies — the surface's own Render carries those.
func (p *sourcePicker) Arrange(r gooey.Rect) {
	p.Base.Arrange(r)
	if !p.pop.IsOpen() {
		p.pop.ArrangeSurface(false, r)
		return
	}
	p.pop.ArrangeSurface(true, p.popupRect(r))
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

// HandleKey handles the picker's own gestures and hands everything else
// to the popup, whose Modal flag swallows it — the page's `q` must not
// fire under the box. Esc lands there too, and dismisses.
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
	case input.Rune('b'), input.Rune('q'):
		p.Dismiss()
	default:
		return p.pop.HandleKey(ev)
	}
	return true
}

// HandleMouse sees every pointer event while open (the popup took the
// capture). Clicks on a source row choose it; a click anywhere else
// dismisses without activating what is underneath — the pointer never
// reaches it.
//
// This is deliberately NOT Popup.HandleMouse: the primitive's grammar
// dismisses on PRESS, and a press-dismissed popup is closed by the time
// the click arrives, so the residue would fall through to the pane
// below. The click grammar this file has always had is kept.
func (p *sourcePicker) HandleMouse(ev input.MouseEvent) bool {
	if !p.IsOpen() {
		return false
	}
	switch ev.Kind {
	case input.WheelUp:
		p.moveSel(-1)
	case input.WheelDown:
		p.moveSel(+1)
	case input.MouseClick:
		if i, ok := p.sourceAt(ev.X, ev.Y); ok {
			p.selP.Set(i)
			p.activate()
			return true
		}
		b := p.pop.SurfaceBounds()
		if ev.X >= b.X && ev.X < b.X+b.W && ev.Y >= b.Y && ev.Y < b.Y+b.H {
			return true // popup furniture: border, header row
		}
		p.Dismiss()
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

// rowsWindow is which rows are visible: the popup keeps a scroll window
// like the ItemsView's, small enough to hand-roll — sources are dozens,
// not thousands.
func (p *sourcePicker) rowsWindow(rows []sourceRow, sel, h int) int {
	// Row index of the selected source.
	selRow := 0
	for i, r := range rows {
		if r.src == sel {
			selRow = i
			break
		}
	}
	top := p.top
	if selRow < top {
		top = selRow
	}
	if selRow >= top+h {
		top = selRow - h + 1
	}
	p.top = clampIdx(top, max(1, len(rows)-h+1))
	return p.top
}

// drawPopup paints the box. It runs inside the SURFACE's paint node, so
// every property it reads is that node's dependency: moving the
// selection repaints the popup and nothing else.
func (p *sourcePicker) drawPopup(f *gooey.Frame, b gooey.Rect) {
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

	srcs := p.srcsP.Get()
	rows := sourceRows(srcs)
	if len(rows) == 0 {
		f.Cells.SetString(b.X+2, b.Y+1, clip("no sources — not a git repository?", b.W-3), dim)
		return
	}
	sel := clampIdx(p.selP.Get(), len(srcs))
	h := b.H - 2
	top := p.rowsWindow(rows, sel, h)
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
		label := r.text(p.curID)
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
// drawPopup painted — clicks land on what the user sees.
func (p *sourcePicker) sourceAt(x, y int) (int, bool) {
	b := p.pop.SurfaceBounds()
	if x <= b.X || x >= b.X+b.W-1 || y <= b.Y || y >= b.Y+b.H-1 {
		return 0, false
	}
	rows := sourceRows(p.srcsP.Get())
	i := p.top + (y - b.Y - 1)
	if i < 0 || i >= len(rows) || rows[i].src < 0 {
		return 0, false
	}
	return rows[i].src, true
}
