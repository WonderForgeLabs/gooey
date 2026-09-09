package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/components"
)

// TestWrappingAMenuItemRepeatsTheSeedsTitle.
//
// This PR narrowed <MenuBar>'s Only list from {"Menu", "MenuItem"} to
// {"Menu"}, because the two-entry version declared a child the builder
// refuses. A consequence nobody looked at: wrapperFor takes the
// SINGLE-CANDIDATE branch for MenuBar now, so MenuBar moved from being the
// example of the decline path to a live user of the wrap path — and that
// path was untested for it. Reported in review of #454.
//
// The behaviour is the wrapper's documented KNOWN LIMIT: the clone carries
// the seed's attributes verbatim, so a second <Menu> repeats the first's
// Title TEXT. That half stays — the fix needs a notion of "the attribute
// that labels this element" the catalog does not have.
//
// WHAT DOES NOT STAY IS THE ACCELERATOR, and the first version of this
// test asked the wrong question about it. It read
// strings.Contains(title, "_") and concluded the clone claimed nothing —
// a fourth local re-derivation of a rule components/mnemonic.go owns and
// says must not be re-derived, and wrong in the same direction this
// module was already wrong once (review of #428): menus fall back to the
// FIRST LETTER, so <Menu Title="File"> claims alt+f with no marker at
// all, MenuBar.titleWithAccel takes the first match, and the new menu
// never opens. It asks components.MenuMnemonic now, which is the function
// exported for exactly this. Raised in review of #454.
func TestWrappingAMenuItemRepeatsTheSeedsTitle(t *testing.T) {
	ed, _ := buildPage(t)

	// The premise. If MenuBar ever names two permitted children again,
	// wrapperFor declines and everything below is testing nothing.
	if got := ed.wrapperFor("MenuBar", "MenuItem"); got != "Menu" {
		t.Fatalf("wrapperFor(MenuBar, MenuItem) = %q, want \"Menu\" — MenuBar is no "+
			"longer on the single-candidate branch and this test cannot see its case", got)
	}

	// THROUGH THE GESTURE, not through wrapperNode. The de-shadowing
	// moved to the insertion seam in review round 10, so a test that
	// calls wrapperNode is testing the half that no longer holds the rule.
	//
	// AND THE GESTURE IS PASTE, not the palette, which is the finding
	// stated from the test's side: loadPalette drops <MenuItem> as
	// Nested, so addSelected can never produce wrap == "Menu" and this
	// route is unreachable from the toolbox. Pasting a <MenuItem> with
	// the <MenuBar> selected is the one way to reach it — the route the
	// old guard covered, and the only one it covered.
	menuBarPage(t, ed)
	ed.setSelection(ed.doc().Kids[0].Kids[0])
	ed.copySelected()
	if ed.clip.node == nil || ed.clip.node.Elem != "MenuItem" {
		t.Fatalf("y did not copy the <MenuItem>: %s", ed.status.Get())
	}
	ed.setSelection(ed.doc())
	ed.pasteClip()
	if n := len(ed.doc().Kids); n != 2 {
		t.Fatalf("the bar holds %d children after pasting a <MenuItem>, want 2: %s",
			n, ed.status.Get())
	}
	w := ed.doc().Kids[1]
	if w.Elem != "Menu" {
		t.Fatalf("the pasted <MenuItem> landed as <%s>, want a <Menu> wrapper", w.Elem)
	}
	title, ok := w.Attrs["Title"]
	if !ok {
		t.Fatal("the wrapper carries no Title, but <Menu> requires one — " +
			"a wrapper that does not build is worse than a repeated label")
	}

	// THE TEXT REPEAT IS THE DOCUMENTED LIMIT and stays: strip the marker
	// and the clone still reads "File". Pinned so the fix below cannot be
	// mistaken for a rename.
	if strings.ReplaceAll(title, "_", "") != "File" {
		t.Errorf("the cloned wrapper's Title is %q; the documented limit is that it "+
			"repeats the seed's text, and something has changed that without "+
			"changing the paragraph that says so", title)
	}

	// THE SHADOWING LINE, asked of the owner of the rule. Two menus
	// claiming alt+f means one of them never opens, and which one is a
	// fact about tree order no reader of the markup can see.
	got, has := components.MenuMnemonic(title)
	if !has {
		t.Fatalf("components.MenuMnemonic(%q) claims nothing at all; menus fall back "+
			"to the first letter, so this fixture no longer exercises the rule", title)
	}
	seed, _ := components.MenuMnemonic("File")
	if got == seed {
		t.Errorf("the cloned wrapper's Title is %q, which claims accelerator %q — the "+
			"same one the <Menu> already in the bar claims. MenuBar.titleWithAccel "+
			"takes the first match, so the menu the user just created never opens.",
			title, string(got))
	}
}

// TestAWrapperInAnEmptyBarKeepsTheSeedsTitle is the other side of the
// de-shadowing: it must not rewrite a title that collides with nothing.
// Without this, "make the accelerator unique" is satisfied by marking
// every wrapper, which would put a stray underscore in the first menu a
// user ever adds. Raised in review of #454.
func TestAWrapperInAnEmptyBarKeepsTheSeedsTitle(t *testing.T) {
	ed, _ := buildPage(t)
	// A bar whose one menu claims something ELSE. "Nothing to avoid" is
	// the property, and a bar holding a <Menu Title="_Zebra"> states it
	// with a live sibling rather than with an empty container — which
	// would also pass for a guard that never runs because there is no
	// collision to find and no sibling to look at either.
	ed.doc().Elem = "MenuBar"
	ed.doc().Attrs = map[string]string{}
	ed.doc().Kids = []*node{
		{Elem: "Menu", Attrs: map[string]string{"Title": "_Zebra"}, Kids: []*node{
			{Elem: "MenuItem", Attrs: map[string]string{"Text": "Open"}},
		}},
	}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}

	ed.setSelection(ed.doc().Kids[0].Kids[0])
	ed.copySelected()
	ed.setSelection(ed.doc())
	ed.pasteClip()
	if n := len(ed.doc().Kids); n != 2 {
		t.Fatalf("the bar holds %d children after the paste, want 2: %s",
			n, ed.status.Get())
	}
	if got := ed.doc().Kids[1].Attrs["Title"]; got != "File" {
		t.Errorf("the wrapper's Title is %q, want the seed's %q unchanged — the only "+
			"menu in the bar claims \"z\", so there is nothing to avoid",
			got, "File")
	}
}

// menuBarPage retypes the fixture's root into a <MenuBar> holding one
// <Menu Title="_File">, which is the shape all three insertion routes
// have to be safe in. It is a helper rather than three copies because the
// three tests below differ only in the GESTURE.
func menuBarPage(t *testing.T, ed *editor) *node {
	t.Helper()
	ed.doc().Elem = "MenuBar"
	ed.doc().Attrs = map[string]string{}
	ed.doc().Kids = []*node{
		{Elem: "Menu", Attrs: map[string]string{"Title": "_File"}, Kids: []*node{
			{Elem: "MenuItem", Attrs: map[string]string{"Text": "Open"}},
		}},
	}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}
	return ed.doc().Kids[0]
}

// accelsIn reports what every <Menu> in bar claims, through the function
// that owns the rule. A rune appearing twice is a menu the user cannot
// open: MenuBar.titleWithAccel takes the first match, and which <Menu>
// that is is a fact about tree order no reader of the markup can see.
func accelsIn(t *testing.T, bar *node) map[rune]int {
	t.Helper()
	got := map[rune]int{}
	for _, k := range bar.Kids {
		if k.Elem != "Menu" {
			continue
		}
		if r, ok := components.MenuMnemonic(k.Attrs["Title"]); ok {
			got[r]++
		}
	}
	return got
}

// TestDuplicatingAMenuDoesNotStealItsAccelerator is arm A of the round-10
// finding, and it is the gesture THIS PR made reachable: <Menu> became
// selectable through alt+enter, so ctrl+d on one is a thing a user can
// now do. clone renames Name and copies Title verbatim, and movable()
// only refuses a surface parent — so before the de-shadowing moved to the
// insertion seam, two menus both claimed alt+f and the duplicate was
// unreachable by keyboard.
func TestDuplicatingAMenuDoesNotStealItsAccelerator(t *testing.T) {
	ed, _ := buildPage(t)
	menu := menuBarPage(t, ed)

	ed.setSelection(menu)
	if !ed.duplicateSelected() {
		t.Fatalf("ctrl+d on the <Menu> was refused: %s", ed.status.Get())
	}
	if n := len(ed.doc().Kids); n != 2 {
		t.Fatalf("the bar holds %d children after a duplicate, want 2", n)
	}
	for r, n := range accelsIn(t, ed.doc()) {
		if n > 1 {
			t.Errorf("%d menus claim %q after ctrl+d — the copy shadows the original "+
				"and one of the two never opens", n, string(r))
		}
	}
	if ed.docRoot == nil {
		t.Errorf("the duplicate does not build: %s", ed.status.Get())
	}
}

// TestPastingAMenuDoesNotStealItsAccelerator is arm B. Pasting a <Menu>
// into a <MenuBar> needs no wrapper — canHold says yes directly — so
// insertSubtree lands it verbatim and never reaches wrapperNode at all.
// That is why the guard could not live there.
func TestPastingAMenuDoesNotStealItsAccelerator(t *testing.T) {
	ed, _ := buildPage(t)
	menu := menuBarPage(t, ed)

	ed.setSelection(menu)
	ed.copySelected()
	if ed.clip.node == nil {
		t.Fatalf("y did not copy the <Menu>: %s", ed.status.Get())
	}
	ed.setSelection(ed.doc())
	ed.pasteClip()
	if n := len(ed.doc().Kids); n != 2 {
		t.Fatalf("the bar holds %d children after a paste, want 2: %s",
			n, ed.status.Get())
	}
	for r, n := range accelsIn(t, ed.doc()) {
		if n > 1 {
			t.Errorf("%d menus claim %q after y then p — the pasted menu shadows the "+
				"original and one of the two never opens", n, string(r))
		}
	}
	if ed.docRoot == nil {
		t.Errorf("the paste does not build: %s", ed.status.Get())
	}
}

// TestMarkingAnUnclaimedLetterKeepsALiteralUnderscore is the escape half.
//
// `__` is a literal underscore in this convention — components/mnemonic.go
// and the <Menu Title> row in docs/markup-reference.md — and markUnclaimed
// used to strip every underscore before scanning, so the label a user
// wrote to read `Sa_ve` came back reading `Save`. Round 10.
func TestMarkingAnUnclaimedLetterKeepsALiteralUnderscore(t *testing.T) {
	for _, c := range []struct {
		title   string
		claimed []rune
		want    string
		display string
	}{
		{"Sa__ve", []rune{'s'}, "S_a__ve", "Sa_ve"},
		{"File", []rune{'f'}, "F_ile", "File"},
		{"_File", []rune{'f'}, "F_ile", "File"},
		{"__x", []rune{}, "___x", "_x"},
	} {
		claimed := map[rune]bool{}
		for _, r := range c.claimed {
			claimed[r] = true
		}
		got, ok := markUnclaimed(c.title, claimed)
		if !ok {
			t.Errorf("markUnclaimed(%q) found no unclaimed letter", c.title)
			continue
		}
		if got != c.want {
			t.Errorf("markUnclaimed(%q) = %q, want %q, which is the encoding of the "+
				"label %q with one letter marked", c.title, got, c.want, c.display)
		}
		// THE DISPLAY TEXT IS THE POINT and `want` encodes it: the
		// comment column below each case is what the user reads, which
		// must be what they typed with one letter now underlined. It is
		// spelled out rather than computed because components does not
		// export the text half of the split — only MenuMnemonic, which
		// answers the other question this asks next.
		if r, has := components.MenuMnemonic(got); !has {
			t.Errorf("markUnclaimed(%q) = %q, which claims nothing", c.title, got)
		} else if claimed[r] {
			t.Errorf("markUnclaimed(%q) = %q, which claims %q — already taken",
				c.title, got, string(r))
		}
	}
}

// TestTheWrapperStillBuildsWhatItWraps. The repeat is only acceptable
// because the document still LOADS; a wrapper missing a required
// attribute would be a different and much worse defect wearing the same
// "known limit" label.
func TestTheWrapperStillBuildsWhatItWraps(t *testing.T) {
	ed, _ := buildPage(t)
	ed.doc().Elem = "MenuBar"
	ed.doc().Attrs = map[string]string{}
	ed.doc().Kids = []*node{
		{Elem: "Menu", Attrs: map[string]string{"Title": "_File"}, Kids: []*node{
			{Elem: "MenuItem", Attrs: map[string]string{"Text": "Open"}},
		}},
	}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}

	// Add a MenuItem with the BAR selected: it has to be wrapped in a
	// <Menu>, and the result has to load.
	ed.sel = ed.doc()
	plan := ed.planAdd("MenuItem")
	if plan.wrap != "Menu" {
		t.Fatalf("planAdd(MenuItem) wrap = %q, want \"Menu\"", plan.wrap)
	}
	if plan.into != ed.doc() {
		t.Fatal("planAdd climbed past the selected <MenuBar>")
	}

	w := ed.wrapperNode(&node{Elem: "MenuBar"}, "Menu")
	w.Kids = []*node{{Elem: "MenuItem", Attrs: map[string]string{"Text": "New"}}}
	plan.into.Kids = append(plan.into.Kids, w)
	ed.rebuild()

	if ed.docRoot == nil {
		t.Fatalf("the wrapped insert does not build: %s", ed.status.Get())
	}
}

// TestThePaletteAddSeamIsUnreachableForAMenuWrapper is the honest half of
// the fix, and it exists so the next reader does not have to re-derive it.
//
// unshadowMnemonic is called at all THREE insertion seams, and only two
// of them can be driven: loadPalette drops <MenuItem> as Nested, so
// wrapperFor("MenuBar", …) never fires from the toolbox and the call in
// addSelected is unfalsifiable — removing it is SILENT against this whole
// suite, measured. That is the same shape as the defect round 10 found,
// pointed the other way, and the call stays because a seam that is
// consistent at two of three is how the first version came to guard the
// one route nobody could take.
//
// This test is the EXPIRY. The day <MenuItem> becomes placeable it goes
// red, and the fix is to drive TestWrappingAMenuItemRepeatsTheSeedsTitle
// through the palette as well as through paste — at which point the
// silent arm is no longer silent.
func TestThePaletteAddSeamIsUnreachableForAMenuWrapper(t *testing.T) {
	ed, _ := buildPage(t)
	for _, e := range ed.palette {
		if e.Name == "MenuItem" {
			t.Fatal("<MenuItem> is now in the palette, so addSelected CAN produce a " +
				"<Menu> wrapper and that seam's unshadowMnemonic call is reachable. " +
				"Drive the wrap route through the palette too — until now it was " +
				"only reachable by pasting a <MenuItem> into a <MenuBar>")
		}
	}
	// NOT VACUOUS: the palette is non-empty and does hold the element the
	// wrapper itself is made of, so "no MenuItem" is a fact about the
	// filter and not about an empty list.
	found := false
	for _, e := range ed.palette {
		if e.Name == "Menu" {
			found = true
		}
	}
	if found {
		t.Error("<Menu> is in the palette, which contradicts loadPalette dropping " +
			"Nested elements and makes the assertion above meaningless")
	}
	if len(ed.palette) == 0 {
		t.Fatal("the palette is empty; the assertion above holds for the wrong reason")
	}
}
