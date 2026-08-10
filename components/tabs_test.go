package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The damage pins in this file are the Tabs contract: a tab switch
// repaints exactly the strip, the outgoing page (whose cells the
// visibility sweep erases), and the incoming page; an idle frame
// repaints nothing.

func twoTabs(sel *prop.Property[int]) *Tabs {
	return &Tabs{
		Items: []TabItem{
			{Header: Str("one"), Content: &Text{Content: Str("page one")}},
			{Header: Str("two"), Content: &Text{Content: Str("page two")}},
		},
		Selected: sel,
	}
}

func TestTabsSwitchRepaintsExactlyStripAndPages(t *testing.T) {
	sel := prop.NewSource(0)
	tabs := twoTabs(sel)
	c := gooey.NewComposer(tabs, 20, 4)
	c.Frame()
	if got := row(c.Cells(), 1); !strings.Contains(got, "page one") {
		t.Fatalf("row 1 = %q, want page one", got)
	}
	if got := row(c.Cells(), 0); !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("strip = %q, want both headers", got)
	}

	sel.Set(1)
	// Exactly three nodes: the strip (its Render read Selected), the
	// outgoing page (bumped by its bounds collapsing to zero; its cells
	// are erased by the sweep, its Render is skipped), and the incoming
	// page. Nothing else in the composition is touched.
	if _, painted := c.Frame(); painted != 3 {
		t.Fatalf("a tab switch painted %d components, want 3 (strip + outgoing + incoming)", painted)
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, "page two") || strings.Contains(got, "one") {
		t.Fatalf("row 1 after switch = %q, want page two with page one erased", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("an idle frame painted %d components, want 0", painted)
	}
}

// Inside a page layout the switch additionally forces the restore pass
// over the vacated rect — the same one repaint the literal visibility
// sweep costs (count parity with markup's bound-Visibility tests) — and
// a sibling outside the Tabs stays clean.
func TestTabsSwitchLeavesNeighboursClean(t *testing.T) {
	sel := prop.NewSource(0)
	tabs := twoTabs(sel)
	neighbour := &Text{Content: Str("elsewhere")}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tabs, neighbour}}, 20, 5)
	c.Frame()

	sel.Set(1)
	// strip + outgoing + incoming + the root restore over the vacated
	// rect (chrome-only, paints no cells). The neighbour is not among
	// them: its row survives untouched.
	if _, painted := c.Frame(); painted != 4 {
		t.Fatalf("a switch in a stack painted %d components, want 4", painted)
	}
	if got := row(c.Cells(), 2); got != "elsewhere" {
		t.Fatalf("neighbour row = %q, want untouched", got)
	}
}

func TestTabsFocusMoveRepaintsTwo(t *testing.T) {
	tabs := twoTabs(prop.NewSource(0))
	other := &Checkbox{Checked: prop.NewSource(false), Label: Str("x")}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tabs, other}}, 20, 5)
	c.Frame()

	c.Focus().FocusNext()
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("a focus move painted %d components, want 2", painted)
	}
}

func TestTabsHoverRepaintsOnlyTheStrip(t *testing.T) {
	tabs := twoTabs(prop.NewSource(0))
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tabs, &Text{Content: Str("n")}}}, 20, 5)
	c.Frame()

	tabs.SetHovered(true)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("hover painted %d components, want 1", painted)
	}
}

// Left/right on the focused strip follow the rocker rule: consumed only
// when the selection moves, so an end-of-travel arrow keeps bubbling
// and moves focus instead of dead-ending.
func TestTabsArrowsFollowTheRockerRule(t *testing.T) {
	sel := prop.NewSource(0)
	tabs := twoTabs(sel)
	tabs.SetFocused(true)

	if tabs.HandleKey(input.Named(input.KeyLeft)) {
		t.Fatal("left at the first tab was consumed; it should bubble")
	}
	if !tabs.HandleKey(input.Named(input.KeyRight)) || sel.Get() != 1 {
		t.Fatalf("right did not move the selection: sel=%d", sel.Get())
	}
	if tabs.HandleKey(input.Named(input.KeyRight)) {
		t.Fatal("right at the last tab was consumed; it should bubble")
	}
	if !tabs.HandleKey(input.Named(input.KeyHome)) || sel.Get() != 0 {
		t.Fatalf("home did not jump to the first tab: sel=%d", sel.Get())
	}

	// An unfocused strip leaves the arrows entirely alone.
	tabs.SetFocused(false)
	if tabs.HandleKey(input.Named(input.KeyRight)) {
		t.Fatal("an unfocused strip consumed an arrow")
	}
}

// Ctrl+PgUp/PgDn cycle (wrapping) and arrive by bubbling, so they work
// while focus is anywhere inside the Tabs subtree — a component in a
// page, not just the strip.
func TestTabsCtrlPageKeysCycleFromInsideAPage(t *testing.T) {
	sel := prop.NewSource(0)
	inner := &Checkbox{Checked: prop.NewSource(false), Label: Str("in page one")}
	tabs := &Tabs{
		Items: []TabItem{
			{Header: Str("one"), Content: inner},
			{Header: Str("two"), Content: &Text{Content: Str("page two")}},
		},
		Selected: sel,
	}
	c := gooey.NewComposer(tabs, 24, 4)
	c.Frame()
	if !c.Focus().SetFocus(inner) {
		t.Fatal("could not focus the page content")
	}

	next := input.KeyEvent{Key: input.KeyPageDown, Mods: input.ModCtrl}
	prev := input.KeyEvent{Key: input.KeyPageUp, Mods: input.ModCtrl}
	if !c.Focus().Dispatch(next) || sel.Get() != 1 {
		t.Fatalf("ctrl+pgdn from page content did not switch: sel=%d", sel.Get())
	}
	if !c.Focus().Dispatch(next) || sel.Get() != 0 {
		t.Fatalf("ctrl+pgdn did not wrap: sel=%d", sel.Get())
	}
	if !c.Focus().Dispatch(prev) || sel.Get() != 1 {
		t.Fatalf("ctrl+pgup did not wrap backwards: sel=%d", sel.Get())
	}
}

// Switching away from a page whose descendant holds focus rescues focus
// onto the strip — a collapsed component must not keep the keyboard.
func TestTabsSwitchRescuesFocusFromTheOutgoingPage(t *testing.T) {
	sel := prop.NewSource(0)
	inner := &Checkbox{Checked: prop.NewSource(false), Label: Str("focus me")}
	tabs := &Tabs{
		Items: []TabItem{
			{Header: Str("one"), Content: &VStack{Children: []gooey.Component{inner}}},
			{Header: Str("two"), Content: &Text{Content: Str("page two")}},
		},
		Selected: sel,
	}
	c := gooey.NewComposer(tabs, 24, 4)
	c.Frame()
	if !c.Focus().SetFocus(inner) {
		t.Fatal("could not focus the page content")
	}

	if !tabs.Select(1) {
		t.Fatal("Select(1) reported no move")
	}
	if got := c.Focus().Focused(); got != gooey.Component(tabs) {
		t.Fatalf("focus after switching away = %T, want the Tabs strip", got)
	}
}

// A hidden page's focus stops are unreachable: tab traversal from the
// strip lands in the ACTIVE page, never in a collapsed one.
func TestTabsHiddenPageLeavesFocusOrder(t *testing.T) {
	sel := prop.NewSource(1)
	hiddenStop := &Checkbox{Checked: prop.NewSource(false), Label: Str("hidden")}
	visibleStop := &Checkbox{Checked: prop.NewSource(false), Label: Str("shown")}
	tabs := &Tabs{
		Items: []TabItem{
			{Header: Str("one"), Content: hiddenStop},
			{Header: Str("two"), Content: visibleStop},
		},
		Selected: sel,
	}
	c := gooey.NewComposer(tabs, 24, 4)
	c.Frame()
	if !c.Focus().SetFocus(tabs) {
		t.Fatal("could not focus the strip")
	}
	c.Focus().FocusNext()
	if got := c.Focus().Focused(); got != gooey.Component(visibleStop) {
		t.Fatalf("tab from the strip landed on %T (%v), want the active page's stop", got, got)
	}
}

func TestTabsClickAndWheelOnTheStrip(t *testing.T) {
	sel := prop.NewSource(0)
	tabs := twoTabs(sel)
	c := gooey.NewComposer(tabs, 24, 4)
	c.Frame()

	// The strip is ` one │ two `: header two starts at x=6.
	if !tabs.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 7, Y: 0}) || sel.Get() != 1 {
		t.Fatalf("clicking the second header did not select it: sel=%d", sel.Get())
	}
	// The separator column belongs to neither header.
	if tabs.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 5, Y: 0}) {
		t.Fatal("a click on the separator was consumed")
	}
	// A click below the strip is the page's business.
	if tabs.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 2, Y: 1}) {
		t.Fatal("a click on the content area was consumed by the strip")
	}

	// The wheel steps without wrapping; end-of-travel is not consumed.
	if !tabs.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 2, Y: 0}) || sel.Get() != 0 {
		t.Fatalf("wheel up did not step back: sel=%d", sel.Get())
	}
	if tabs.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 2, Y: 0}) {
		t.Fatal("wheel up at the first tab was consumed")
	}
	if !tabs.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 2, Y: 0}) || sel.Get() != 1 {
		t.Fatalf("wheel down did not step forward: sel=%d", sel.Get())
	}
	// Off the strip the wheel is not the Tabs' business.
	if tabs.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 2, Y: 2}) {
		t.Fatal("a wheel over the content area was consumed by the strip")
	}
}

// The wave-1 disabled contract: a Changed whose condition says no paints
// the strip dim and refuses every gesture; the flip repaints exactly
// the strip.
func TestTabsDisabledRefusesAndRepaintsOnce(t *testing.T) {
	sel := prop.NewSource(0)
	can := prop.NewSource(true)
	tabs := twoTabs(sel)
	tabs.Changed = gooey.NewCommand(func() {}).When(can)
	c := gooey.NewComposer(tabs, 24, 4)
	c.Frame()

	can.Set(false)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("disabling painted %d components, want 1 (the strip)", painted)
	}
	tabs.SetFocused(true)
	if tabs.HandleKey(input.Named(input.KeyRight)) || sel.Get() != 0 {
		t.Fatal("a disabled strip moved its selection from a key")
	}
	if tabs.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 7, Y: 0}) || sel.Get() != 0 {
		t.Fatal("a disabled strip moved its selection from a click")
	}
	if tabs.Select(1) {
		t.Fatal("a disabled strip moved its selection from code")
	}
}

func TestTabsChangedRunsAfterTheSelectionMoved(t *testing.T) {
	sel := prop.NewSource(0)
	tabs := twoTabs(sel)
	got := -1
	tabs.Changed = gooey.Command(func() { got = sel.Get() })
	c := gooey.NewComposer(tabs, 24, 4)
	c.Frame()

	tabs.Select(1)
	if got != 1 {
		t.Fatalf("Changed saw sel=%d, want 1 (the property is Set before it runs)", got)
	}
	got = -1
	tabs.Select(1) // no move, no event
	if got != -1 {
		t.Fatal("Changed ran for a selection that did not move")
	}
}

// A Tabs with no Selected handle owns its selection: it starts at 0 and
// switches on its own source.
func TestTabsNilSelectedIsSelfContained(t *testing.T) {
	tabs := &Tabs{Items: []TabItem{
		{Header: Str("a"), Content: &Text{Content: Str("first")}},
		{Header: Str("b"), Content: &Text{Content: Str("second")}},
	}}
	c := gooey.NewComposer(tabs, 20, 4)
	c.Frame()
	if got := row(c.Cells(), 1); !strings.Contains(got, "first") {
		t.Fatalf("row 1 = %q, want the first page by default", got)
	}
	tabs.Select(1)
	c.Frame()
	if got := row(c.Cells(), 1); !strings.Contains(got, "second") {
		t.Fatalf("row 1 after Select = %q, want the second page", got)
	}
}

// An out-of-range bound selection clamps: the viewmodel that has not
// caught up with a shorter tab list still shows the last page.
func TestTabsOutOfRangeSelectionClamps(t *testing.T) {
	sel := prop.NewSource(7)
	tabs := twoTabs(sel)
	c := gooey.NewComposer(tabs, 20, 4)
	c.Frame()
	if got := row(c.Cells(), 1); !strings.Contains(got, "page two") {
		t.Fatalf("row 1 = %q, want the clamped last page", got)
	}
}
