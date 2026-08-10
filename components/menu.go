package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// MenuItem is one entry in a Menu. Action follows the house command
// model: absent is inert (the item paints normally and activating it
// just closes the menu), a condition that says no paints Dim and
// refuses activation. Gesture is a DISPLAY hint in the markup gesture
// syntax ("ctrl+s"); showing it does not bind the key — declare a
// KeyBinding for that, the way the rest of the framework already
// spells it.
type MenuItem struct {
	Text      string
	Gesture   string
	Action    gooey.Action
	Separator bool
}

// Menu is one titled dropdown on a MenuBar.
type Menu struct {
	Title string
	Items []MenuItem
}

// MenuBar is the top menu row: titles across one line, and a dropdown
// overlay below the open title.
//
// Z-ORDER: the dropdown must paint above the page content, and in gooey
// z-order IS document order — so declare the MenuBar as the LAST child
// of its container, positioned onto the top row (in a Grid, the element
// order and Grid.Row are independent, which is exactly what this
// needs). The dropdown is a child of the bar arranged BELOW the bar's
// own bounds; being late in document order is what puts it above the
// content it covers, and the Composer's restore pass repaints that
// content when the menu closes or moves.
//
// FOCUS: the bar is a focus stop. Opening remembers what had focus —
// for a mouse open, the component focus-follows-click just took it from
// — and dismissing (esc, activation, click elsewhere) gives it back.
//
// MOUSE: while open, the bar holds the pointer capture, so every
// pointer event routes here no matter what it is over: clicks on items
// activate, motion tracks the highlight, and a press anywhere else
// dismisses without reaching — or activating — whatever is underneath.
// Capture is also what makes the dropdown clickable at all: it hangs
// outside the bar's bounds, where hit-testing cannot see it.
//
// KEYS while open are modal: arrows navigate, enter activates, esc
// dismisses and restores focus, tab dismisses and moves on, and
// everything else is swallowed so page gestures cannot fire under an
// open menu.
type MenuBar struct {
	gooey.Base
	gooey.FocusState
	Menus []Menu
	Style *prop.Property[render.Style]

	mgr     *gooey.FocusManager
	popup   *menuPopup
	kids    []gooey.Component
	restore gooey.Component

	curP  *prop.Property[int]  // highlighted title
	openP *prop.Property[bool] // a dropdown is showing
	selP  *prop.Property[int]  // highlighted item in the open menu
}

// SetFocusManager receives the input tree (gooey.FocusHost) — the seam
// the bar restores focus and takes pointer capture through.
func (m *MenuBar) SetFocusManager(fm *gooey.FocusManager) { m.mgr = fm }

func (m *MenuBar) ChildComponents() []gooey.Component {
	m.ensurePopup()
	return m.kids
}

func (m *MenuBar) ensurePopup() *menuPopup {
	if m.popup == nil {
		m.popup = &menuPopup{bar: m}
		m.popup.LayoutProps().Visibility = gooey.Collapsed
		m.kids = []gooey.Component{m.popup}
	}
	return m.popup
}

func (m *MenuBar) cur() *prop.Property[int] {
	if m.curP == nil {
		m.curP = prop.NewSource(0)
	}
	return m.curP
}

func (m *MenuBar) open() *prop.Property[bool] {
	if m.openP == nil {
		m.openP = prop.NewSource(false)
	}
	return m.openP
}

func (m *MenuBar) sel() *prop.Property[int] {
	if m.selP == nil {
		m.selP = prop.NewSource(0)
	}
	return m.selP
}

// IsOpen reports whether a dropdown is showing. Read from a Render it
// is a paint dependency like any other property.
func (m *MenuBar) IsOpen() bool { return m.open().Get() }

func (m *MenuBar) curIdx() int {
	if len(m.Menus) == 0 {
		return 0
	}
	return clamp(m.cur().Get(), 0, len(m.Menus)-1)
}

// titleSpan is the cell range of title i on the bar row: each title is
// " Title " and they sit flush against each other from the left edge.
func (m *MenuBar) titleSpan(i int) (x, w int) {
	x = m.Bounds().X
	for j := 0; j < i; j++ {
		x += len([]rune(m.Menus[j].Title)) + 2
	}
	return x, len([]rune(m.Menus[i].Title)) + 2
}

func (m *MenuBar) titleAt(x, y int) (int, bool) {
	b := m.Bounds()
	if y != b.Y {
		return 0, false
	}
	for i := range m.Menus {
		tx, tw := m.titleSpan(i)
		if x >= tx && x < tx+tw {
			return i, true
		}
	}
	return 0, false
}

// popupRect is where the open menu's dropdown goes: under the open
// title, sized to its items plus a box border. It may extend past the
// bar's own bounds — that is the point of an overlay — and the buffer
// clips whatever falls off screen.
func (m *MenuBar) popupRect() gooey.Rect {
	b := m.Bounds()
	i := m.curIdx()
	menu := m.Menus[i]
	tx, _ := m.titleSpan(i)
	w := 4 // border + padding
	for _, it := range menu.Items {
		iw := len([]rune(it.Text)) + 4
		if it.Gesture != "" {
			iw += len([]rune(it.Gesture)) + 2
		}
		if iw > w {
			w = iw
		}
	}
	return gooey.Rect{X: tx, Y: b.Y + 1, W: w, H: len(menu.Items) + 2}
}

func (m *MenuBar) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}

// Arrange places the bar on its row and the dropdown below the open
// title. The open flag is read here in layout — a plain read, recorded
// nowhere — and the popup's Visibility is flipped as a plain field; the
// Composer's per-frame sweep is what turns that into damage, exactly as
// it does for any runtime visibility change.
func (m *MenuBar) Arrange(r gooey.Rect) {
	m.Base.Arrange(r)
	p := m.ensurePopup()
	l := p.LayoutProps()
	if m.open().Get() && len(m.Menus) > 0 && len(m.Menus[m.curIdx()].Items) > 0 {
		l.Visibility = gooey.Visible
		pr := m.popupRect()
		gooey.MeasureChild(p, gooey.Size{W: pr.W, H: pr.H})
		gooey.ArrangeChild(p, pr)
	} else {
		l.Visibility = gooey.Collapsed
		gooey.ArrangeChild(p, gooey.Rect{X: r.X, Y: r.Y, W: 0, H: 0})
	}
}

// Render paints the bar row only — the dropdown is the popup child's
// own paint node, so navigating an open menu repaints the dropdown and
// leaves the bar alone.
func (m *MenuBar) Render(f *gooey.Frame) {
	b := m.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	st := getSty(m.Style)
	open := m.IsOpen()
	cur := m.curIdx()
	focused := m.IsFocused()
	for x := b.X; x < b.X+b.W; x++ {
		f.Cells.Set(x, b.Y, ' ', st)
	}
	x := b.X
	for i, menu := range m.Menus {
		ts := st
		if i == cur && (open || focused) {
			ts.Reverse = true
			if open {
				ts.Bold = true
			}
		}
		f.Cells.SetString(x, b.Y, clipRunes(" "+menu.Title+" ", b.X+b.W-x), ts)
		x += len([]rune(menu.Title)) + 2
		if x >= b.X+b.W {
			break
		}
	}
}

// Open drops the menu at index i. Restore is what gets focus back when
// the menu dismisses; nil means focus stays on the bar.
func (m *MenuBar) Open(i int, restore gooey.Component) {
	if len(m.Menus) == 0 {
		return
	}
	m.cur().Set(clamp(i, 0, len(m.Menus)-1))
	m.sel().Set(m.firstItem(m.curIdx()))
	m.open().Set(true)
	m.restore = restore
	if m.mgr != nil {
		m.mgr.SetFocus(m)
		m.mgr.CaptureMouse(m)
	}
}

// Dismiss closes the dropdown, releases the pointer, and hands focus
// back to whatever had it when the menu opened — provided nothing moved
// it elsewhere in the meantime.
func (m *MenuBar) Dismiss() {
	if !m.IsOpen() {
		return
	}
	m.open().Set(false)
	if m.mgr != nil {
		if m.mgr.Captured() == gooey.Component(m) {
			m.mgr.ReleaseCapture()
		}
		if m.restore != nil && m.mgr.Focused() == gooey.Component(m) {
			m.mgr.SetFocus(m.restore)
		}
	}
	m.restore = nil
}

// firstItem is the first activatable index — separators are furniture.
func (m *MenuBar) firstItem(menu int) int {
	for i, it := range m.Menus[menu].Items {
		if !it.Separator {
			return i
		}
	}
	return 0
}

// moveSel steps the item highlight by d, skipping separators, wrapping.
func (m *MenuBar) moveSel(d int) {
	items := m.Menus[m.curIdx()].Items
	n := len(items)
	if n == 0 {
		return
	}
	i := clamp(m.sel().Get(), 0, n-1)
	for step := 0; step < n; step++ {
		i = ((i+d)%n + n) % n
		if !items[i].Separator {
			m.sel().Set(i)
			return
		}
	}
}

func (m *MenuBar) switchMenu(d int) {
	n := len(m.Menus)
	if n == 0 {
		return
	}
	m.cur().Set(((m.curIdx()+d)%n + n) % n)
	m.sel().Set(m.firstItem(m.curIdx()))
}

// activate runs item i of the open menu. A separator does nothing; a
// disabled item refuses and the menu stays open — Dim is not just a
// look; an item with no action closes the menu and nothing more.
func (m *MenuBar) activate(i int) {
	items := m.Menus[m.curIdx()].Items
	if i < 0 || i >= len(items) || items[i].Separator {
		return
	}
	it := items[i]
	if it.Action != nil && !it.Action.CanExecute() {
		return
	}
	m.Dismiss()
	if gooey.CanExecute(it.Action) {
		it.Action.Run()
	}
}

// pressRestore is what should get focus back after a mouse-opened menu:
// focus-follows-click has already moved focus to the bar by the time
// the press bubbles here, so the component to give it back to is the
// one the manager remembers losing it.
func (m *MenuBar) pressRestore() gooey.Component {
	if m.mgr == nil {
		return nil
	}
	if f := m.mgr.Focused(); f != nil && f != gooey.Component(m) {
		return f
	}
	return m.mgr.PreviouslyFocused()
}

func (m *MenuBar) HandleKey(ev input.KeyEvent) bool {
	if len(m.Menus) == 0 {
		return false
	}
	if m.IsOpen() {
		return m.handleOpenKey(ev)
	}
	switch ev {
	case input.Named(input.KeyLeft):
		// The rocker rule: an arrow is consumed only when it moves
		// something, so a one-menu bar does not trap focus navigation.
		if len(m.Menus) < 2 {
			return false
		}
		m.cur().Set(((m.curIdx()-1)%len(m.Menus) + len(m.Menus)) % len(m.Menus))
		return true
	case input.Named(input.KeyRight):
		if len(m.Menus) < 2 {
			return false
		}
		m.cur().Set((m.curIdx() + 1) % len(m.Menus))
		return true
	case input.Named(input.KeyDown), input.Named(input.KeyEnter), input.Rune(' '):
		// Key-open: the bar itself has focus, so there is nothing to
		// restore — esc simply leaves focus where it already is.
		m.Open(m.curIdx(), nil)
		return true
	}
	return false
}

func (m *MenuBar) handleOpenKey(ev input.KeyEvent) bool {
	switch ev {
	case input.Named(input.KeyEsc):
		m.Dismiss()
		return true
	case input.Named(input.KeyTab):
		// Tab leaves menu mode: close, restore, and let the key travel
		// on so focus moves from wherever it was restored to.
		m.Dismiss()
		return false
	case input.Named(input.KeyLeft):
		m.switchMenu(-1)
		return true
	case input.Named(input.KeyRight):
		m.switchMenu(1)
		return true
	case input.Named(input.KeyUp):
		m.moveSel(-1)
		return true
	case input.Named(input.KeyDown):
		m.moveSel(1)
		return true
	case input.Named(input.KeyHome):
		m.sel().Set(m.firstItem(m.curIdx()))
		return true
	case input.Named(input.KeyEnd):
		items := m.Menus[m.curIdx()].Items
		for i := len(items) - 1; i >= 0; i-- {
			if !items[i].Separator {
				m.sel().Set(i)
				break
			}
		}
		return true
	case input.Named(input.KeyEnter), input.Rune(' '):
		m.activate(m.sel().Get())
		return true
	}
	// Modal: an open menu swallows what it does not understand, so page
	// gestures cannot fire underneath it.
	return true
}

// itemAt maps a screen cell into the open dropdown's item list.
func (m *MenuBar) itemAt(x, y int) (int, bool) {
	if !m.IsOpen() {
		return 0, false
	}
	pb := m.ensurePopup().Bounds()
	if x < pb.X || x >= pb.X+pb.W || y <= pb.Y || y >= pb.Y+pb.H-1 {
		return 0, false
	}
	return y - pb.Y - 1, true
}

func (m *MenuBar) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.MousePress:
		if i, ok := m.titleAt(ev.X, ev.Y); ok {
			if m.IsOpen() && i == m.curIdx() {
				m.Dismiss()
				return true
			}
			if m.IsOpen() {
				m.switchMenu(i - m.curIdx())
				return true
			}
			m.Open(i, m.pressRestore())
			return true
		}
		if !m.IsOpen() {
			return false
		}
		if i, ok := m.itemAt(ev.X, ev.Y); ok {
			m.sel().Set(clamp(i, 0, max(0, len(m.Menus[m.curIdx()].Items)-1)))
			m.activate(i)
			return true
		}
		// A press anywhere else dismisses AND is consumed: whatever is
		// under the pointer must not also receive it — that is what the
		// capture is for.
		m.Dismiss()
		return true
	case input.MouseRelease, input.MouseClick:
		// The gestures act on press; the rest of the pair is swallowed
		// while the menu is (or was just) holding the pointer.
		return m.IsOpen()
	}
	return m.IsOpen()
}

// HandleMouseMove tracks the highlight under the pointer while open —
// items in the dropdown, titles along the bar.
func (m *MenuBar) HandleMouseMove(ev input.MouseEvent) bool {
	if !m.IsOpen() {
		return false
	}
	if i, ok := m.itemAt(ev.X, ev.Y); ok {
		items := m.Menus[m.curIdx()].Items
		if i >= 0 && i < len(items) && !items[i].Separator && m.sel().Get() != i {
			m.sel().Set(i)
		}
		return true
	}
	if i, ok := m.titleAt(ev.X, ev.Y); ok && i != m.curIdx() {
		m.switchMenu(i - m.curIdx())
	}
	return true
}

// menuPopup is the dropdown: a leaf child of the bar, so its paint node
// pre-clears and covers its rectangle — the overlay contract — and
// navigating repaints only it. It is Collapsed while the menu is
// closed; the Composer's visibility sweep and restore pass handle the
// appear/vanish transitions.
type menuPopup struct {
	gooey.Base
	bar *MenuBar
}

func (p *menuPopup) Measure(avail gooey.Size) gooey.Size { return avail }

func (p *menuPopup) Render(f *gooey.Frame) {
	b := p.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	bar := p.bar
	if !bar.IsOpen() || len(bar.Menus) == 0 {
		return
	}
	menu := bar.Menus[bar.curIdx()]
	sel := clamp(bar.sel().Get(), 0, max(0, len(menu.Items)-1))
	st := getSty(bar.Style)

	// The box.
	for x := b.X + 1; x < b.X+b.W-1; x++ {
		f.Cells.Set(x, b.Y, '─', st)
		f.Cells.Set(x, b.Y+b.H-1, '─', st)
	}
	for y := b.Y + 1; y < b.Y+b.H-1; y++ {
		f.Cells.Set(b.X, y, '│', st)
		f.Cells.Set(b.X+b.W-1, y, '│', st)
	}
	f.Cells.Set(b.X, b.Y, '╭', st)
	f.Cells.Set(b.X+b.W-1, b.Y, '╮', st)
	f.Cells.Set(b.X, b.Y+b.H-1, '╰', st)
	f.Cells.Set(b.X+b.W-1, b.Y+b.H-1, '╯', st)

	inner := b.W - 2
	for i, it := range menu.Items {
		y := b.Y + 1 + i
		if y >= b.Y+b.H-1 {
			break
		}
		if it.Separator {
			f.Cells.Set(b.X, y, '├', st)
			for x := b.X + 1; x < b.X+b.W-1; x++ {
				f.Cells.Set(x, y, '─', st)
			}
			f.Cells.Set(b.X+b.W-1, y, '┤', st)
			continue
		}
		is := st
		// The CanExecute read happens while painting, so a condition
		// flipping repaints the open dropdown and the item goes dim (or
		// comes back) with no event anywhere.
		if it.Action != nil && !it.Action.CanExecute() {
			is.Dim = true
		}
		if i == sel {
			is.Reverse = true
		}
		line := " " + it.Text
		if it.Gesture != "" {
			pad := inner - len([]rune(line)) - len([]rune(it.Gesture)) - 1
			if pad < 1 {
				pad = 1
			}
			line += spaces(pad) + it.Gesture + " "
		}
		if n := inner - len([]rune(line)); n > 0 {
			line += spaces(n)
		}
		f.Cells.SetString(b.X+1, y, clipRunes(line, inner), is)
	}
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]rune, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
