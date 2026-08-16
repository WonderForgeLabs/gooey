package components

import (
	"unicode"

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
//
// Text may carry a MNEMONIC marker: an underscore before a letter
// ("E_xit") names the item's accelerator, XAML's AccessText convention —
// underscore rather than ampersand because these strings live in XML
// attributes, where "&" is an entity and "_" is just a character.
// Without a marker the first letter is the implicit accelerator. "__"
// renders a literal underscore; only the first marker counts. While the
// menu is open, typing an item's accelerator activates it.
type MenuItem struct {
	Text      string
	Gesture   string
	Action    gooey.Action
	Separator bool
}

// Menu is one titled dropdown on a MenuBar. Title takes the same
// mnemonic marker as MenuItem.Text ("_File"): alt+letter opens this
// menu from anywhere on the page, marker or first letter.
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
// dismisses and restores focus, tab dismisses and moves on, a plain
// letter activates the item wearing it as its accelerator, alt+letter
// switches menus, and everything else is swallowed so page gestures
// cannot fire under an open menu.
//
// MNEMONICS: every title and item has an accelerator — marked with an
// underscore in the string ("_File", "E_xit") or defaulting to the
// first letter — rendered underlined, always. While the bar is closed,
// alt+letter opens the matching menu from anywhere on the page (the
// gooey.MnemonicHandler seam; any KeyBinding on the same gesture
// outranks it, per dispatch order).
type MenuBar struct {
	gooey.Base
	gooey.FocusState
	Menus []Menu
	Style *prop.Property[render.Style]

	pop  *Popup
	kids []gooey.Component

	curP *prop.Property[int] // highlighted title
	selP *prop.Property[int] // highlighted item in the open menu
}

// SetFocusManager receives the input tree (gooey.FocusHost) — forwarded
// to the popup, which is the seam the bar restores focus and takes
// pointer capture through.
func (m *MenuBar) SetFocusManager(fm *gooey.FocusManager) { m.popup().SetFocusManager(fm) }

func (m *MenuBar) ChildComponents() []gooey.Component {
	m.popup()
	return m.kids
}

// popup is the bar's Popup primitive: lifecycle, focus save/restore,
// capture, and the modal fall-throughs. The dropdown's cells are the
// primitive's surface, drawn by drawDropdown; everything menu-shaped
// (mnemonics, gesture hints, item semantics) stays here.
func (m *MenuBar) popup() *Popup {
	if m.pop == nil {
		m.pop = NewPopup(m, m.drawDropdown)
		m.pop.Modal = true // an open menu swallows what it does not understand
		m.kids = []gooey.Component{m.pop.Surface()}
	}
	return m.pop
}

func (m *MenuBar) cur() *prop.Property[int] {
	if m.curP == nil {
		m.curP = prop.NewSource(0)
	}
	return m.curP
}

func (m *MenuBar) sel() *prop.Property[int] {
	if m.selP == nil {
		m.selP = prop.NewSource(0)
	}
	return m.selP
}

// IsOpen reports whether a dropdown is showing. Read from a Render it
// is a paint dependency like any other property.
func (m *MenuBar) IsOpen() bool { return m.popup().IsOpen() }

func (m *MenuBar) curIdx() int {
	if len(m.Menus) == 0 {
		return 0
	}
	return clamp(m.cur().Get(), 0, len(m.Menus)-1)
}

// titleSpan is the cell range of title i on the bar row: each title is
// " Title " and they sit flush against each other from the left edge.
// Widths are of the DISPLAY text — the mnemonic marker is syntax, not
// cells.
func (m *MenuBar) titleSpan(i int) (x, w int) {
	x = m.Bounds().X
	for j := 0; j < i; j++ {
		t, _, _ := splitMnemonic(m.Menus[j].Title)
		x += len([]rune(t)) + 2
	}
	t, _, _ := splitMnemonic(m.Menus[i].Title)
	return x, len([]rune(t)) + 2
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
		text, _, _ := splitMnemonic(it.Text)
		iw := len([]rune(text)) + 4
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
// nowhere — and the surface's appear/vanish transitions are the bounds
// sweep's job (see Popup.ArrangeSurface).
func (m *MenuBar) Arrange(r gooey.Rect) {
	m.Base.Arrange(r)
	p := m.popup()
	show := p.IsOpen() && len(m.Menus) > 0 && len(m.Menus[m.curIdx()].Items) > 0
	pr := gooey.Rect{X: r.X, Y: r.Y}
	if show {
		pr = m.popupRect()
	}
	p.ArrangeSurface(show, pr)
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
		text, _, pos := splitMnemonic(menu.Title)
		f.Cells.SetString(x, b.Y, clipRunes(" "+text+" ", b.X+b.W-x), ts)
		// The accelerator letter is ALWAYS underlined — a terminal cannot
		// see a held ALT (no key-up events), so "show while ALT is down"
		// is not implementable, and always-on is the honest convention.
		// Static per title: no property, no extra damage.
		if ax := x + 1 + pos; pos >= 0 && ax < b.X+b.W {
			as := ts
			as.Underline = true
			f.Cells.Set(ax, b.Y, []rune(text)[pos], as)
		}
		x += len([]rune(text)) + 2
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
	m.popup().Open(restore)
}

// Dismiss closes the dropdown, releases the pointer, and hands focus
// back to whatever had it when the menu opened — provided nothing moved
// it elsewhere in the meantime.
func (m *MenuBar) Dismiss() { m.popup().Dismiss() }

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

// titleWithAccel finds the menu whose accelerator is r. First match
// wins — two titles sharing a letter is an authoring mistake the
// framework resolves deterministically rather than reports.
func (m *MenuBar) titleWithAccel(r rune) (int, bool) {
	r = unicode.ToLower(r)
	for i := range m.Menus {
		if _, a, pos := splitMnemonic(m.Menus[i].Title); pos >= 0 && a == r {
			return i, true
		}
	}
	return 0, false
}

// itemWithAccel finds the open menu's item whose accelerator is r.
func (m *MenuBar) itemWithAccel(r rune) (int, bool) {
	r = unicode.ToLower(r)
	for i, it := range m.Menus[m.curIdx()].Items {
		if it.Separator {
			continue
		}
		if _, a, pos := splitMnemonic(it.Text); pos >= 0 && a == r {
			return i, true
		}
	}
	return 0, false
}

// HandleMnemonic is the bar's page-scoped accelerator (see
// gooey.MnemonicHandler): alt+letter opens the matching menu from
// anywhere on the page, no matter what holds focus. The dispatcher only
// offers keys the focused chain declined, so any KeyBinding on the same
// alt gesture wins.
//
// Focus has NOT moved when an accelerator fires — unlike a mouse open,
// where focus-follows-click has already run — so the component to
// restore on dismiss is simply whatever is focused right now.
func (m *MenuBar) HandleMnemonic(ev input.KeyEvent) bool {
	if ev.Key != input.KeyRune || ev.Mods != input.ModAlt || len(m.Menus) == 0 {
		return false
	}
	i, ok := m.titleWithAccel(ev.Rune)
	if !ok {
		return false
	}
	if m.IsOpen() {
		// Reachable only when focus left the bar while a menu was open;
		// the modal path below handles the focused-bar case.
		m.switchMenu(i - m.curIdx())
		return true
	}
	var restore gooey.Component
	if mgr := m.popup().Manager(); mgr != nil {
		if f := mgr.Focused(); f != nil && f != gooey.Component(m) {
			restore = f
		}
	}
	m.Open(i, restore)
	return true
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
	// Accelerators inside the modal: alt+letter switches to the matching
	// menu, a plain letter jumps to and activates the matching item — a
	// disabled match moves the highlight and refuses, exactly like enter
	// on it. Both are consumed whether or not they matched, because the
	// menu is modal either way.
	if ev.Key == input.KeyRune && ev.Mods == input.ModAlt {
		if i, ok := m.titleWithAccel(ev.Rune); ok {
			m.switchMenu(i - m.curIdx())
		}
		return true
	}
	if ev.Key == input.KeyRune && ev.Mods == 0 {
		if i, ok := m.itemWithAccel(ev.Rune); ok {
			m.sel().Set(i)
			m.activate(i)
		}
		return true
	}
	// The popup's fall-through: esc dismisses, and Modal swallows the
	// rest — an open menu is modal, so page gestures cannot fire
	// underneath it.
	return m.popup().HandleKey(ev)
}

// itemAt maps a screen cell into the open dropdown's item list.
func (m *MenuBar) itemAt(x, y int) (int, bool) {
	if !m.IsOpen() {
		return 0, false
	}
	pb := m.popup().SurfaceBounds()
	if x < pb.X || x >= pb.X+pb.W || y <= pb.Y || y >= pb.Y+pb.H-1 {
		return 0, false
	}
	return y - pb.Y - 1, true
}

func (m *MenuBar) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MousePress {
		if i, ok := m.titleAt(ev.X, ev.Y); ok {
			if m.IsOpen() && i == m.curIdx() {
				m.Dismiss()
				return true
			}
			if m.IsOpen() {
				m.switchMenu(i - m.curIdx())
				return true
			}
			m.Open(i, m.popup().MouseOpenRestore())
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
	}
	// The popup's fall-through: a press anywhere the bar did not claim
	// dismisses AND is consumed — whatever is under the pointer must not
	// also receive it, which is what the capture is for — and the
	// release/click residue of that gesture is swallowed with it.
	return m.popup().HandleMouse(ev)
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

// drawDropdown paints the open dropdown into the popup surface — the
// primitive's leaf child of the bar, whose paint node pre-clears and
// covers its rectangle (the overlay contract), so navigating repaints
// only it. The selection and CanExecute reads happen inside that node,
// which is what keeps the damage pins: a highlight move or a condition
// flip repaints the dropdown alone.
func (m *MenuBar) drawDropdown(f *gooey.Frame, b gooey.Rect) {
	if len(m.Menus) == 0 {
		return
	}
	menu := m.Menus[m.curIdx()]
	sel := clamp(m.sel().Get(), 0, max(0, len(menu.Items)-1))
	st := getSty(m.Style)

	// The box — the same rounded outline a Border paints, from the same
	// helper. The separator rows below are this dropdown's own chrome
	// and stay here.
	DrawBoxRunes(f.Cells, b, st)

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
		text, _, pos := splitMnemonic(it.Text)
		line := " " + text
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
		// Same convention as the bar: the accelerator letter is always
		// underlined. Typing it activates the item while the menu is open.
		if pos >= 0 && 1+pos < inner {
			as := is
			as.Underline = true
			f.Cells.Set(b.X+2+pos, y, []rune(text)[pos], as)
		}
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
