package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The standard page these tests share: full-width content on row 1, a
// focusable button on row 2, and the MenuBar declared LAST — document
// order is z-order, so being last is what puts its dropdown above the
// content it drops over.
func menuPage(saved *int, can *prop.Property[bool]) (*MenuBar, *Button, gooey.Component) {
	save := gooey.NewCommand(func() { *saved++ })
	var saveAction gooey.Action = save
	if can != nil {
		saveAction = save.When(can)
	}
	bar := &MenuBar{Menus: []Menu{
		{Title: "File", Items: []MenuItem{
			{Text: "Save", Gesture: "ctrl+s", Action: saveAction},
			{Separator: true},
			{Text: "Quit", Action: gooey.Command(func() {})},
		}},
		{Title: "Edit", Items: []MenuItem{
			{Text: "Copy", Action: gooey.Command(func() {})},
		}},
	}}
	under := gooey.L(&Text{Content: Str(strings.Repeat("#", 30))}, gooey.Layout{Top: 1})
	btn := &Button{Content: Str("elsewhere"), Click: gooey.Command(func() {})}
	page := &Canvas{Children: []gooey.Component{
		under,
		gooey.L(btn, gooey.Layout{Top: 3, Left: 25}), // clear of the File dropdown
		bar, // last = on top
	}}
	return bar, btn, page
}

func TestMenuOpenPaintsBarAndDropdown(t *testing.T) {
	bar, _, page := menuPage(new(int), nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()

	if !c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter on the focused bar was not consumed")
	}
	_, painted := c.Frame()
	if painted != 2 {
		t.Fatalf("opening painted %d components, want 2 (bar highlight + dropdown)", painted)
	}
	if !bar.IsOpen() {
		t.Fatal("the menu did not open")
	}
	if got := row(c.Cells(), 1); !strings.HasPrefix(got, "╭") {
		t.Fatalf("row 1 = %q, want the dropdown frame over the content", got)
	}
	if got := row(c.Cells(), 2); !strings.Contains(got, "Save") || !strings.Contains(got, "ctrl+s") {
		t.Fatalf("row 2 = %q, want the item with its gesture hint", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

func TestMenuNavigateRepaintsDropdownOnly(t *testing.T) {
	bar, _, page := menuPage(new(int), nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	c.HandleKey(input.Named(input.KeyDown)) // over the separator to Quit
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("moving the item highlight painted %d components, want 1 (the dropdown)", painted)
	}
	if got := row(c.Cells(), 4); !strings.Contains(got, "Quit") {
		t.Fatalf("row 4 = %q, want Quit", got)
	}
	// The separator was skipped, not selected.
	if got := bar.sel().Get(); got != 2 {
		t.Fatalf("selection = %d, want 2 (the separator is furniture)", got)
	}
}

// Esc closes the dropdown, and the vacated cells repaint from what was
// beneath — the restore half of the z-ordered pass.
func TestMenuEscClosesAndRestoresContentBeneath(t *testing.T) {
	bar, _, page := menuPage(new(int), nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	if !c.HandleKey(input.Named(input.KeyEsc)) {
		t.Fatal("esc was not consumed by the open menu")
	}
	c.Frame()
	if bar.IsOpen() {
		t.Fatal("esc did not close the menu")
	}
	if got := row(c.Cells(), 1); got != strings.Repeat("#", 30) {
		t.Fatalf("row 1 after dismiss = %q — the content beneath did not restore", got)
	}
	if got := c.Focus().Captured(); got != nil {
		t.Fatalf("the pointer is still captured by %T after dismiss", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

// A mouse-opened menu remembers whom focus-follows-click stole focus
// from, and esc gives it back.
func TestMenuMouseOpenEscRestoresFocus(t *testing.T) {
	bar, btn, page := menuPage(new(int), nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(btn)
	c.Frame()

	// Press on the "File" title: focus-follows-click moves focus to the
	// bar BEFORE the press bubbles, then the bar opens and captures.
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 2, Y: 0})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: 2, Y: 0})
	c.Frame()
	if !bar.IsOpen() {
		t.Fatal("clicking the title did not open the menu")
	}
	if c.Focus().Focused() != gooey.Component(bar) {
		t.Fatal("the open menu bar does not hold focus")
	}

	c.HandleKey(input.Named(input.KeyEsc))
	c.Frame()
	if got := c.Focus().Focused(); got != gooey.Component(btn) {
		t.Fatalf("focus after dismiss is %T, want the button that had it before the click", got)
	}
}

// While open, the bar holds the pointer capture: a press anywhere else
// dismisses the menu, is consumed, and must NOT reach — or activate —
// what is underneath.
func TestMenuClickElsewhereDismissesWithoutActivating(t *testing.T) {
	clicked := 0
	bar, btn, page := menuPage(new(int), nil)
	btn.Click = gooey.Command(func() { clicked++ })
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()
	if c.Focus().Captured() != gooey.Component(bar) {
		t.Fatal("the open bar does not hold the pointer capture")
	}

	// Press on the button, well outside the dropdown.
	bb := btn.Bounds()
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: bb.X + 1, Y: bb.Y})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: bb.X + 1, Y: bb.Y})
	if bar.IsOpen() {
		t.Fatal("a press elsewhere did not dismiss the menu")
	}
	if clicked != 0 {
		t.Fatal("the press that dismissed the menu leaked to the button underneath")
	}
}

func TestMenuEnterActivatesTheItem(t *testing.T) {
	saved := 0
	bar, _, page := menuPage(&saved, nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter)) // open, Save highlighted
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter)) // activate
	c.Frame()
	if saved != 1 {
		t.Fatalf("Save ran %d times, want 1", saved)
	}
	if bar.IsOpen() {
		t.Fatal("activation did not close the menu")
	}
}

// A disabled item paints Dim and refuses activation; the menu stays
// open, because nothing happened.
func TestMenuDisabledItemPaintsDimAndRefuses(t *testing.T) {
	saved := 0
	can := prop.NewSource(false)
	bar, _, page := menuPage(&saved, can)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	if !c.Cells().At(1, 2).Style.Dim {
		t.Fatal("an item whose condition says no is not dim")
	}
	c.HandleKey(input.Named(input.KeyEnter))
	if saved != 0 || !bar.IsOpen() {
		t.Fatalf("a disabled item activated (ran %d, open %v)", saved, bar.IsOpen())
	}

	// The condition is read while painting the dropdown, so the flip
	// repaints exactly it.
	can.Set(true)
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("enabling the item painted %d components, want 1 (the dropdown)", painted)
	}
	if c.Cells().At(1, 2).Style.Dim {
		t.Fatal("an enabled item is still dim")
	}
}

// Switching menus while open moves the dropdown: the old rectangle is
// restored from what was beneath, the new one paints over its spot.
func TestMenuSwitchTitleMovesTheDropdown(t *testing.T) {
	bar, _, page := menuPage(new(int), nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	c.HandleKey(input.Named(input.KeyRight)) // Edit
	c.Frame()
	if got := bar.curIdx(); got != 1 {
		t.Fatalf("open menu index = %d, want 1", got)
	}
	got := row(c.Cells(), 1)
	if !strings.HasPrefix(got, "######") {
		t.Fatalf("row 1 = %q — the cells the File dropdown vacated did not restore", got)
	}
	if !strings.Contains(got, "╭") {
		t.Fatalf("row 1 = %q, want the Edit dropdown frame", got)
	}
	if got := row(c.Cells(), 2); !strings.Contains(got, "Copy") {
		t.Fatalf("row 2 = %q, want the Edit menu's item", got)
	}
}

// Arrows on a closed, focused bar move the title highlight; up/down at
// the ends of nothing fall through as everywhere else. A closed bar
// with one menu consumes no arrows at all (the rocker rule).
func TestMenuClosedBarArrowsMoveTheHighlight(t *testing.T) {
	bar, _, page := menuPage(new(int), nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()

	if !c.HandleKey(input.Named(input.KeyRight)) {
		t.Fatal("right on the closed bar was not consumed")
	}
	if bar.curIdx() != 1 {
		t.Fatalf("highlight = %d, want 1", bar.curIdx())
	}
	if bar.IsOpen() {
		t.Fatal("moving the highlight opened a menu")
	}

	one := &MenuBar{Menus: []Menu{{Title: "Only", Items: []MenuItem{{Text: "x"}}}}}
	if one.HandleKey(input.Named(input.KeyLeft)) {
		t.Fatal("a one-menu bar consumed an arrow it cannot use")
	}
}

// splitMnemonic: the underscore marker names the accelerator, "__" is a
// literal underscore, and an unmarked string defaults to its first
// letter.
func TestMenuMnemonicParsing(t *testing.T) {
	for _, tc := range []struct {
		in, text string
		accel    rune
		pos      int
	}{
		{"_File", "File", 'f', 0},
		{"E_xit", "Exit", 'x', 1},
		{"Save", "Save", 's', 0},
		{"_a_b", "a_b", 'a', 0}, // first marker wins, later underscores are literal
		{"__x", "_x", 'x', 1},   // escaped underscore, implicit accel skips it
		{"...", "...", 0, -1},   // nothing to accelerate with
		{"", "", 0, -1},
	} {
		text, accel, pos := splitMnemonic(tc.in)
		if text != tc.text || accel != tc.accel || pos != tc.pos {
			t.Errorf("splitMnemonic(%q) = (%q, %q, %d), want (%q, %q, %d)",
				tc.in, text, string(accel), pos, tc.text, string(tc.accel), tc.pos)
		}
	}
}

// alt+letter opens the matching menu from ANYWHERE on the page: the
// button holds focus, the bar is a sibling, and the accelerator still
// lands — the gooey.MnemonicHandler phase, not the focused chain.
// Dismissing restores focus to what had it when the accelerator fired.
func TestMenuAltMnemonicOpensFromAnywhere(t *testing.T) {
	bar, btn, page := menuPage(new(int), nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(btn)
	c.Frame()

	if !c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'f', Mods: input.ModAlt}) {
		t.Fatal("alt+f was not consumed")
	}
	if !bar.IsOpen() || bar.curIdx() != 0 {
		t.Fatalf("alt+f: open=%v cur=%d, want the File menu open", bar.IsOpen(), bar.curIdx())
	}
	if c.Focus().Focused() != gooey.Component(bar) {
		t.Fatal("the accelerator-opened menu does not hold focus")
	}
	// The usual pins, plus the component that lost focus: 3 repaints.
	if _, painted := c.Frame(); painted != 3 {
		t.Fatalf("alt-open painted %d components, want 3 (focus loser + bar + dropdown)", painted)
	}

	c.HandleKey(input.Named(input.KeyEsc))
	c.Frame()
	if got := c.Focus().Focused(); got != gooey.Component(btn) {
		t.Fatalf("focus after dismiss is %T, want the button that had it when alt+f fired", got)
	}
}

// While open, a plain letter activates the item wearing it as its
// accelerator, and alt+letter switches menus.
func TestMenuOpenLettersJumpAndActivate(t *testing.T) {
	saved := 0
	bar, _, page := menuPage(&saved, nil)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter)) // open File
	c.Frame()

	if !c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'e', Mods: input.ModAlt}) {
		t.Fatal("alt+e on the open menu was not consumed")
	}
	if bar.curIdx() != 1 || !bar.IsOpen() {
		t.Fatalf("alt+e: cur=%d open=%v, want the Edit menu open", bar.curIdx(), bar.IsOpen())
	}
	c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'f', Mods: input.ModAlt}) // back to File
	c.Frame()

	if !c.HandleKey(input.Rune('s')) {
		t.Fatal("the item letter was not consumed")
	}
	if saved != 1 {
		t.Fatalf("Save ran %d times, want 1", saved)
	}
	if bar.IsOpen() {
		t.Fatal("letter activation did not close the menu")
	}
}

// A letter that matches a DISABLED item moves the highlight and
// refuses, exactly like enter on it; a letter matching nothing is
// swallowed — the menu is modal either way.
func TestMenuOpenLetterOnDisabledItemRefuses(t *testing.T) {
	saved := 0
	can := prop.NewSource(false)
	bar, _, page := menuPage(&saved, can)
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	if !c.HandleKey(input.Rune('s')) || saved != 0 || !bar.IsOpen() {
		t.Fatalf("disabled item letter: saved=%d open=%v, want refused and still open", saved, bar.IsOpen())
	}
	if !c.HandleKey(input.Rune('z')) {
		t.Fatal("an unmatched letter leaked out of the modal menu")
	}
}

// A KeyBinding on the same alt gesture beats the mnemonic: bindings run
// in the bubble phase, accelerators only on what the whole chain
// declined.
func TestMenuMnemonicLosesToKeyBinding(t *testing.T) {
	fired := 0
	bar, btn, page := menuPage(new(int), nil)
	page.(*Canvas).Attach(&gooey.KeyBinding{
		Gesture: input.KeyEvent{Key: input.KeyRune, Rune: 'f', Mods: input.ModAlt},
		Command: gooey.Command(func() { fired++ }),
	})
	c := gooey.NewComposer(page, 40, 8)
	c.Focus().SetFocus(btn)
	c.Frame()

	if !c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'f', Mods: input.ModAlt}) {
		t.Fatal("alt+f was not consumed")
	}
	if fired != 1 {
		t.Fatalf("the root KeyBinding fired %d times, want 1", fired)
	}
	if bar.IsOpen() {
		t.Fatal("the mnemonic opened the menu past a KeyBinding on the same gesture")
	}
}

// Accelerator letters render underlined — always, marked or implicit —
// on the bar and in the open dropdown. Static chrome: no extra damage.
func TestMenuMnemonicUnderlines(t *testing.T) {
	bar := &MenuBar{Menus: []Menu{
		{Title: "_File", Items: []MenuItem{{Text: "_Save", Action: gooey.Command(func() {})}}},
		{Title: "E_xit", Items: []MenuItem{{Text: "x"}}},
	}}
	page := &Canvas{Children: []gooey.Component{bar}}
	c := gooey.NewComposer(page, 40, 8)
	c.Frame()

	// " File  Exit " — F at x=1, x at x=8 (second span starts at 6).
	if got := row(c.Cells(), 0); !strings.HasPrefix(got, " File  Exit") {
		t.Fatalf("bar row = %q — the markers leaked into the display text", got)
	}
	if !c.Cells().At(1, 0).Style.Underline {
		t.Fatal("the File accelerator is not underlined")
	}
	if c.Cells().At(2, 0).Style.Underline {
		t.Fatal("a non-accelerator cell is underlined")
	}
	if !c.Cells().At(8, 0).Style.Underline {
		t.Fatal("the marked Exit accelerator is not underlined")
	}

	c.Focus().SetFocus(bar)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()
	if got := row(c.Cells(), 2); !strings.Contains(got, "Save") {
		t.Fatalf("dropdown row = %q, want the Save item", got)
	}
	if !c.Cells().At(2, 2).Style.Underline {
		t.Fatal("the item accelerator in the open dropdown is not underlined")
	}
}

// The pin for the composer half of this wave, from the Canvas side: a
// later sibling (the overlay) turning Hidden must repaint what it was
// covering — the inverse of TestCanvasOverlapRepaintRepaintsTheOccluderAbove.
func TestHidingAnOverlayRestoresWhatWasBeneath(t *testing.T) {
	under := &Text{Content: Str("UNDERNEATH")}
	over := &Text{Content: Str("XXXX")}
	page := &Canvas{Children: []gooey.Component{under, over}}
	c := gooey.NewComposer(page, 12, 3)
	c.Frame()
	if got := row(c.Cells(), 0); got != "XXXXRNEATH" {
		t.Fatalf("row 0 = %q, want the overlay on top", got)
	}

	over.LayoutProps().Visibility = gooey.Hidden
	_, painted := c.Frame()
	if painted != 2 {
		t.Fatalf("hiding the overlay painted %d components, want 2 (restored leaf + swept canvas)", painted)
	}
	if got := row(c.Cells(), 0); got != "UNDERNEATH" {
		t.Fatalf("row 0 after hiding = %q — the occluded content did not restore", got)
	}

	over.LayoutProps().Visibility = gooey.Visible
	if _, painted = c.Frame(); painted != 1 {
		t.Fatalf("showing it again painted %d components, want 1 (the overlay)", painted)
	}
	if got := row(c.Cells(), 0); got != "XXXXRNEATH" {
		t.Fatalf("row 0 after showing = %q", got)
	}
}
