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

// accelsIn reports what every <elem> in parent claims through attr, by
// the function that owns the rule. A rune appearing twice is something
// the user cannot reach: MenuBar.titleWithAccel and Menu.itemWithAccel
// are both first-match-wins, and which one wins is a fact about tree
// order no reader of the markup can see.
//
// THE PAIR IS PASSED IN rather than read from mnemonicAttr, which is the
// table under test. A helper that derived it would go blind in exactly
// the mutation that empties the table, and report no collisions because
// it looked at nothing. Each caller says which level it is asking about.
func accelsIn(t *testing.T, parent *node, elem, attr string) map[rune]int {
	t.Helper()
	got := map[rune]int{}
	for _, k := range parent.Kids {
		if k.Elem != elem {
			continue
		}
		if r, ok := components.MenuMnemonic(k.Attrs[attr]); ok {
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
	for r, n := range accelsIn(t, ed.doc(), "Menu", "Title") {
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
	for r, n := range accelsIn(t, ed.doc(), "Menu", "Title") {
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

	w := ed.wrapperNode("MenuBar", "Menu")
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

// TestDuplicatingAMenuItemDoesNotStealItsAccelerator is round 11's
// finding at the level the round-10 fix could not see.
//
// Two filters excluded it and each was sufficient on its own: the guard
// asked into.Elem != "MenuBar", and a <MenuItem>'s parent is a <Menu>;
// and it selected the attribute with Required && KindString, which
// <MenuItem Text> is neither — KindText, and optional because Separator
// makes it so.
//
// The consequence is the one docs/markup-reference.md states for the
// user: "while the menu is open, typing the letter activates the item".
// Two items claiming "o" leaves one unreachable, and components/menu.go's
// itemWithAccel takes the first.
func TestDuplicatingAMenuItemDoesNotStealItsAccelerator(t *testing.T) {
	ed, _ := buildPage(t)
	menu := menuBarPage(t, ed)

	item := menu.Kids[0]
	if item.Elem != "MenuItem" {
		t.Fatalf("the fixture's first child is <%s>, want <MenuItem>", item.Elem)
	}
	ed.setSelection(item)
	if !ed.duplicateSelected() {
		t.Fatalf("ctrl+d on the <MenuItem> was refused: %s", ed.status.Get())
	}
	if n := len(menu.Kids); n != 2 {
		t.Fatalf("the menu holds %d children after a duplicate, want 2", n)
	}
	for r, n := range accelsIn(t, menu, "MenuItem", "Text") {
		if n > 1 {
			t.Errorf("%d menu items claim %q after ctrl+d — itemWithAccel takes the "+
				"first, so one of the two is unreachable by letter", n, string(r))
		}
	}
	if ed.docRoot == nil {
		t.Errorf("the duplicate does not build: %s", ed.status.Get())
	}
}

// TestPastingAMenuItemDoesNotStealItsAccelerator is the second gesture,
// and it is a different route through the code: pasting a <MenuItem>
// into a <Menu> needs no wrapper, so insertSubtree lands it verbatim.
func TestPastingAMenuItemDoesNotStealItsAccelerator(t *testing.T) {
	ed, _ := buildPage(t)
	menu := menuBarPage(t, ed)

	ed.setSelection(menu.Kids[0])
	ed.copySelected()
	if ed.clip.node == nil || ed.clip.node.Elem != "MenuItem" {
		t.Fatalf("y did not copy the <MenuItem>: %s", ed.status.Get())
	}
	ed.setSelection(menu)
	ed.pasteClip()
	if n := len(menu.Kids); n != 2 {
		t.Fatalf("the menu holds %d children after a paste, want 2: %s",
			n, ed.status.Get())
	}
	for r, n := range accelsIn(t, menu, "MenuItem", "Text") {
		if n > 1 {
			t.Errorf("%d menu items claim %q after y then p — the pasted item shadows "+
				"the original and one of the two is unreachable", n, string(r))
		}
	}
	if ed.docRoot == nil {
		t.Errorf("the paste does not build: %s", ed.status.Get())
	}
}

// TestABoundLabelIsLeftAloneByTheAcceleratorGuard is finding 1 of round
// 12, and it is a CORRUPTION rather than a missed collision.
//
// <MenuItem Text="{{.Label}}"> is a template the markup resolves at
// build time. components.MenuMnemonic read the literal and answered "L";
// markUnclaimed then wrote the marker into the template — "{{._Label}}"
// or "_{{.Label}}" — which either fails to build or silently binds a
// path nobody declared. ctrl+d and paste both reach it.
//
// TWO ARMS, AND THE SECOND IS THE ONE THAT WOULD HAVE BEEN MISSED. The
// first is the obvious one: the duplicate's own bound Text must come out
// byte-for-byte. The second is the phantom claim — a bound SIBLING must
// not be counted as claiming a letter, or an unbound item that collides
// with nothing gets marked anyway, which is the same wrong answer
// wearing a fix. A guard that only refused to WRITE into a template
// would pass the first arm and fail the second.
func TestABoundLabelIsLeftAloneByTheAcceleratorGuard(t *testing.T) {
	t.Run("the duplicate's own binding survives", func(t *testing.T) {
		ed, _ := buildPage(t)
		menu := menuBarPage(t, ed)
		item := menu.Kids[0]
		// A REAL BINDING, registered the way the editor registers one —
		// ctx.Values is where a viewmodel entry lives. Without it the
		// document does not build at all and the arm would pass on a
		// refusal rather than on the guard.
		//
		// A PLAIN STRING, not a property handle: <MenuItem Text> is
		// resolved once at load, and markup refuses a *prop.Property
		// there with a diagnostic saying exactly that. Which is itself
		// worth knowing here — the value being static does not make the
		// EXPRESSION literal, and the expression is what the guard was
		// writing into.
		ed.ctx.Values["Label"] = "Open"
		item.Attrs["Text"] = "{{.Label}}"

		ed.setSelection(item)
		if !ed.duplicateSelected() {
			t.Fatalf("ctrl+d on the bound <MenuItem> was refused: %s", ed.status.Get())
		}
		if n := len(menu.Kids); n != 2 {
			t.Fatalf("the menu holds %d children after a duplicate, want 2", n)
		}
		for i, k := range menu.Kids {
			if got := k.Attrs["Text"]; got != "{{.Label}}" {
				t.Errorf("item %d reads Text=%q after ctrl+d, want %q unchanged. A "+
					"marker written into a binding expression either fails to "+
					"build or binds a path nobody declared — and the letter it "+
					"was placed on came from the template's own source text",
					i, got, "{{.Label}}")
			}
		}
	})

	t.Run("a bound insertion is never marked", func(t *testing.T) {
		// THE ARM THE FIRST TWO LEAVE OPEN, and it was measured: with
		// both items bound, no sibling claims anything, so the write
		// never runs and reading the inserted node's own template
		// literally is SILENT. What separates them is a real collision
		// — an UNBOUND sibling holding the letter the template's source
		// text starts with, which is the state where the marker
		// actually gets written into "{{.Label}}".
		ed, _ := buildPage(t)
		menu := menuBarPage(t, ed)
		menu.Kids[0].Attrs["Text"] = "Label"
		bound := &node{Elem: "MenuItem", Attrs: map[string]string{"Text": "{{.Label}}"}}

		unshadowMnemonic(menu, bound)

		if got := bound.Attrs["Text"]; got != "{{.Label}}" {
			t.Errorf("the inserted item reads Text=%q, want %q unchanged. Its "+
				"sibling claims \"L\", and \"L\" is the first letter of the "+
				"TEMPLATE'S SOURCE rather than of anything the user will see — "+
				"so the marker lands inside the binding expression and the "+
				"document either fails to build or binds a path nobody declared",
				got, "{{.Label}}")
		}
	})

	t.Run("a bound sibling claims nothing", func(t *testing.T) {
		ed, _ := buildPage(t)
		menu := menuBarPage(t, ed)
		// The existing item is bound; the one being inserted is not, and
		// its literal first letter is the one the BINDING's source text
		// would have claimed. Nothing here actually collides — what the
		// template resolves to is not knowable from this tree.
		menu.Kids[0].Attrs["Text"] = "{{.Label}}"
		plain := &node{Elem: "MenuItem", Attrs: map[string]string{"Text": "Label"}}

		unshadowMnemonic(menu, plain)

		if got := plain.Attrs["Text"]; got != "Label" {
			t.Errorf("the inserted item reads Text=%q, want %q. Its only sibling's "+
				"Text is a binding, so no letter is claimed here — marking one "+
				"puts a stray underscore in front of the user for a collision "+
				"that does not exist", got, "Label")
		}
	})
}

// TestAnItemInAMenuThatCollidesWithNothingKeepsItsText is the other side,
// matching TestAWrapperInAnEmptyBarKeepsTheSeedsTitle one level down.
// "Make the accelerator unique" is satisfied by marking every item, which
// would put a stray underscore in the first item a user ever duplicates
// into an otherwise clear menu.
func TestAnItemInAMenuThatCollidesWithNothingKeepsItsText(t *testing.T) {
	ed, _ := buildPage(t)
	menuBarPage(t, ed)
	menu := ed.doc().Kids[0]
	// A LIVE SIBLING claiming something else, not an empty menu: an
	// empty one also passes for a guard that never runs.
	menu.Kids = []*node{
		{Elem: "MenuItem", Attrs: map[string]string{"Text": "Zoom"}},
		{Elem: "MenuItem", Attrs: map[string]string{"Text": "Open"}},
	}
	ed.rebuild()

	// BY POINTER, not by index. duplicateSelected inserts the copy at
	// i+1, so every index after the original shifts — and a test that
	// hardcodes them asserts the insertion position while claiming to
	// assert the rewrite.
	src, bystander := menu.Kids[0], menu.Kids[1]
	ed.setSelection(src)
	if !ed.duplicateSelected() {
		t.Fatalf("ctrl+d was refused: %s", ed.status.Get())
	}
	if got := bystander.Attrs["Text"]; got != "Open" {
		t.Errorf("the untouched sibling's Text became %q; it collides with nothing "+
			"and must not be rewritten", got)
	}
	if got := src.Attrs["Text"]; got != "Zoom" {
		t.Errorf("the ORIGINAL's Text became %q. The node being inserted is the one "+
			"that gives way; rewriting the document the user already had is a "+
			"different feature", got)
	}
	for r, n := range accelsIn(t, menu, "MenuItem", "Text") {
		if n > 1 {
			t.Errorf("%d items claim %q; the copy still shadows something", n, string(r))
		}
	}
}

// TestTheMnemonicTableCoversBothMenuLevels is the derivation floor. The
// rule is a table of two element names, which is a thing that can be
// half-deleted without breaking a build — and each half has its own
// gesture tests above, so a missing row shows up as two failures with no
// obvious common cause. This says the cause.
func TestTheMnemonicTableCoversBothMenuLevels(t *testing.T) {
	for parent, attr := range map[string]string{"MenuBar": "Title", "Menu": "Text"} {
		if got := mnemonicAttr[parent]; got != attr {
			t.Errorf("mnemonicAttr[%q] = %q, want %q — a menu level with no row is a "+
				"level where two children may claim the same letter and one of them "+
				"is unreachable by keyboard", parent, got, attr)
		}
	}
	if len(mnemonicAttr) != 2 {
		t.Errorf("mnemonicAttr has %d rows. Adding one is a claim that some other "+
			"container makes its children's letters compete — mnemonic.go says this "+
			"rule is menu-flavoured and a guard about buttons must not reach for it",
			len(mnemonicAttr))
	}
}
