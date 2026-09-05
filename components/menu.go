package components

import (
	"image"
	"unicode"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
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
// Checked, when non-nil, makes the item a CHECK ITEM: it renders
// "[x] Text" or "[ ] Text" and the box is read while the dropdown
// paints, so the check is a paint dependency like any other. That is
// what lets an accelerator and a menu item show one state instead of
// two — the key binding Sets the property, the menu renders the same
// property, and there is no third place for them to disagree.
//
// It is deliberately NOT an Action side effect and NOT a bool field.
// A bool field would be plain Go state: the dropdown reads it while
// painting, records no dependency, and a flip from a key binding while
// the menu is open would leave the old box on screen with nothing to
// notice. The handle is what makes the read a subscription.
//
// Checking is not toggling. The item's Action still owns what the
// activation DOES; Checked only says what the box shows. An item whose
// Action does not write the property it displays is a lie the framework
// cannot catch, so write the toggle once and point both at it.
//
// Icon and IconRune are the item's leading picture, in the two tiers
// this framework always has — and here, uniquely, THE TWO TIERS DRAW
// DIFFERENT THINGS rather than the same thing at different fidelity.
//
// Everywhere else the fallback is graphics.DrawHalfblock, which scales
// an image to cols×rows*2. A dropdown row is ONE CELL TALL, so that is
// two vertical samples for the whole glyph: #400 measured two clearly
// different icons coming back as the same uniform '▀', rgb(105,69,24)
// against rgb(83,55,19) — two states, one appearance, 22/255 apart in a
// single channel. Widening the box does not help, because it is still
// one '▀' per cell. So an item's cell-plane representation is a RUNE and
// not a scaled-down picture, and IconRune is not decoration.
//
// The gutter is reserved UNCONDITIONALLY when any item in the menu
// carries either — see Menu.iconLead. A dropdown one cell narrower
// without a graphics protocol would reflow when the capability probe
// answered, which happens after the first frame; buttonchrome.go
// reserves its pill rows the same way and for the same reason.
//
// Both are plain fields read while painting, like Text and unlike
// Checked. An icon that changed would need the handle treatment; none
// does, and a *prop.Property[image.Image] on every item to serve a case
// nobody has is the wrong trade.
type MenuItem struct {
	Text      string
	Gesture   string
	Action    gooey.Action
	Checked   *prop.Property[bool]
	Icon      image.Image
	IconRune  rune
	Separator bool
}

// Menu is one titled dropdown on a MenuBar. Title takes the same
// mnemonic marker as MenuItem.Text ("_File"): alt+letter opens this
// menu from anywhere on the page, marker or first letter.
type Menu struct {
	Title string
	Items []MenuItem
}

// iconWidth is the reserved icon gutter, in CELLS: two for the picture
// plus one separating it from the check box or the label.
//
// Two rather than one because a one-cell box is 10x20 pixels, and svg
// fits to the narrow side — half of that box is letterboxing. Two cells
// is the smallest box a one-row icon is legible in (#400).
const iconWidth = 3

// iconLead is the icon gutter this menu reserves, 0 when no item in it
// carries an icon of either tier.
//
// PER MENU, not per item, so every label in one dropdown starts at the
// same column — the same reason lead() asks the menu rather than the
// item. And it counts EITHER tier: reserving only when an image is
// present would make the width depend on which fields the app filled
// in, and reserving only when a protocol exists would make it depend on
// the terminal. Neither is a property of the menu.
func (m Menu) iconLead() int {
	for _, it := range m.Items {
		if it.Icon != nil || it.IconRune != 0 {
			return iconWidth
		}
	}
	return 0
}

// lead is the width of the column before an item's text: 4 cells
// ("[x] ") for a menu holding any check item, 1 (a plain space) for one
// holding none.
//
// It is a property of the MENU and not of the item, which is what makes
// a menu with one check item align its plain items with it instead of
// stepping them one cell left. Real menus do this; a menu that does not
// reads as broken.
func (m Menu) lead() int {
	for _, it := range m.Items {
		if it.Checked != nil {
			return 4
		}
	}
	return 1
}

// iconGutter is the item's icon column as CELLS, always exactly
// iconLead() columns wide and empty when this menu reserves none.
//
// pixel says whether the frame can place an image. When it can, the
// gutter is blank here and drawDropdown places the picture over those
// cells; when it cannot, the rune goes in. An item with no icon in a
// menu that has one gets blanks either way — the column is the menu's,
// not the item's.
//
// PADDED IN COLUMNS, not runes, which is the whole reason this is a
// function rather than string(it.IconRune)+"  ". An emoji is one rune
// and TWO cells, so a rune-counted pad would push every label in the
// dropdown one cell right of where popupRect measured for it.
func (m Menu) iconGutter(it MenuItem, pixel bool) string {
	w := m.iconLead()
	if w == 0 {
		return ""
	}
	if pixel && it.Icon != nil {
		return spaces(w)
	}
	if it.IconRune == 0 {
		return spaces(w)
	}
	// PADDED IN COLUMNS and exactly, so there is nothing left to clip:
	// a rune is at most two cells and the gutter is three, so the pad is
	// always at least one and the result is always exactly w columns.
	// clipCols here would be a branch no input can take.
	g := string(it.IconRune)
	return g + spaces(w-render.StringWidth(g))
}

// checkBox is the item's leading column. The spelling is Checkbox's —
// "[x]" and "[ ]" — reused rather than reinvented so a check means the
// same thing everywhere in the framework, and ASCII so it survives a pty
// transcript, which is the only way a menu gets verified at all.
func (m Menu) checkBox(it MenuItem) string {
	w := m.lead()
	if it.Checked == nil {
		return spaces(w)
	}
	if it.Checked.Get() {
		return "[x] "
	}
	return "[ ] "
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
		x += render.StringWidth(t) + 2
	}
	t, _, _ := splitMnemonic(m.Menus[i].Title)
	return x, render.StringWidth(t) + 2
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
	lead := menu.lead()
	for _, it := range menu.Items {
		text, _, _ := splitMnemonic(it.Text)
		// border (2) + the lead column + one trailing cell. The lead is
		// read from the menu rather than assumed to be 1, or a menu with
		// check items sizes itself three cells too narrow and clips every
		// label it holds. The text is measured in COLUMNS, so a CJK or
		// emoji label sizes to the cells it will actually occupy.
		iw := render.StringWidth(text) + lead + menu.iconLead() + 3
		if it.Gesture != "" {
			iw += render.StringWidth(it.Gesture) + 2
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
		f.Cells.SetString(x, b.Y, clipCols(" "+text+" ", b.X+b.W-x), ts)
		// The accelerator letter is ALWAYS underlined — a terminal cannot
		// see a held ALT (no key-up events), so "show while ALT is down"
		// is not implementable, and always-on is the honest convention.
		// Static per title: no property, no extra damage.
		if pos >= 0 {
			// The column, not the rune index — see mnemonicCol. Split
			// out of the one-line `if ax := …; pos >= 0 && …` it used
			// to be, because computing the column means slicing
			// []rune(text)[:pos], which panics on the pos < 0 that the
			// second half of that condition was there to reject.
			if ax := x + 1 + mnemonicCol(text, pos); ax < b.X+b.W {
				underlineAt(f, ax, b.Y, ts)
			}
		}
		x += render.StringWidth(text) + 2
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

// OpenIndex is which menu is showing, or -1 when none is.
//
// -1 RATHER THAN A SECOND CALL TO IsOpen, because the pair is what an
// app would have to write anyway and a zero index is a real answer: a
// bar that reported 0 for "closed" and 0 for "the first menu is open"
// would need every caller to remember to ask twice. The clamp is the
// same one curIdx applies, so this cannot report a menu that does not
// exist.
//
// EXPOSED because everything an app needs to decorate a dropdown was
// private and reachable only by reconstructing it. #400's reporter
// recovered this index by walking the title widths and matching the
// dropdown's left edge — duplicating titleSpan and splitMnemonic's
// marker handling in application code, against arithmetic this package
// is free to change. There is no new state here; there was no way to
// read the state there already was.
//
// Read it from a Render and it is a paint dependency like any other
// property, exactly as IsOpen is.
func (m *MenuBar) OpenIndex() int {
	if !m.IsOpen() {
		return -1
	}
	return m.curIdx()
}

// DropdownBounds is where the open dropdown was arranged, or the zero
// Rect when no menu is open.
//
// The zero Rect for closed rather than the rect the menu WOULD occupy:
// a caller placing pixels into a returned rect must not be handed a
// live-looking answer for a surface that is not on screen, and the zero
// value is the one every Rect check already treats as nothing.
//
// This is the same arithmetic drawDropdown paints into — it is popupRect
// — which is precisely why it is worth exporting and why its tests read
// the rect back off the painted cells instead of comparing it to
// popupRect. An accessor checked against the function behind it is
// correct by construction and says nothing about where the dropdown went.
func (m *MenuBar) DropdownBounds() gooey.Rect {
	if !m.IsOpen() {
		return gooey.Rect{}
	}
	return m.popupRect()
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
		// The check box is read HERE, inside the dropdown's own paint
		// node, so the handle becomes a dependency of exactly this node:
		// toggling a check while the menu is open repaints the dropdown
		// and nothing else, and toggling it while the menu is closed
		// repaints nothing at all.
		// THE SAME GUARD Image, ColorPicker and buttonchrome ask, and it
		// decides WHICH THING IS DRAWN rather than at what fidelity —
		// see MenuItem.Icon. The gutter's width is already reserved
		// either way, so this cannot move anything.
		pixel := f.Graphics != nil && f.CellW > 0 && f.CellH > 0
		lead := menu.iconGutter(it, pixel) + menu.checkBox(it)
		line := lead + text
		if it.Gesture != "" {
			pad := inner - render.StringWidth(line) - render.StringWidth(it.Gesture) - 1
			if pad < 1 {
				pad = 1
			}
			line += spaces(pad) + it.Gesture + " "
		}
		if n := inner - render.StringWidth(line); n > 0 {
			line += spaces(n)
		}
		f.Cells.SetString(b.X+1, y, clipCols(line, inner), is)
		// Same convention as the bar: the accelerator letter is always
		// underlined. Typing it activates the item while the menu is open.
		// The underline offset is measured from the LEAD, not assumed to
		// be one cell in: with a check column the accelerator letter is
		// three cells further right, and underlining the old position
		// would put the rule under the check box.
		//
		// Both halves are COLUMNS, not runes. The lead is measured
		// because a check glyph may be wide, and the offset within the
		// text is mnemonicCol rather than pos because everything left of
		// the accelerator may be. With a one-cell lead this is the same
		// b.X+2+mc the width-only version painted, which is why the two
		// agree everywhere ASCII.
		if at := render.StringWidth(lead) + mnemonicCol(text, pos); pos >= 0 && at < inner {
			underlineAt(f, b.X+1+at, y, is)
		}
		// AFTER the row's cells, not before: SetString above would
		// otherwise overwrite nothing visible but would leave the
		// placement's cells outside the flush that carries it. The
		// image goes in the first iconWidth-1 columns of the gutter,
		// leaving its trailing separator column blank.
		if pixel && it.Icon != nil {
			f.Place(graphics.Placement{
				Img: it.Icon, Col: b.X + 1, Row: y, Cols: iconWidth - 1, Rows: 1,
			})
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
