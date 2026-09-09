package main

import (
	"strings"
	"testing"
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
// Title. This pins it rather than fixing it, because the fix needs a
// notion of "the attribute that labels this element" the catalog does not
// have — but it pins the part that makes it merely cosmetic, which is the
// half that could stop being true.
func TestWrappingAMenuItemRepeatsTheSeedsTitle(t *testing.T) {
	ed, _ := buildPage(t)

	// The premise. If MenuBar ever names two permitted children again,
	// wrapperFor declines and everything below is testing nothing.
	if got := ed.wrapperFor("MenuBar", "MenuItem"); got != "Menu" {
		t.Fatalf("wrapperFor(MenuBar, MenuItem) = %q, want \"Menu\" — MenuBar is no "+
			"longer on the single-candidate branch and this test cannot see its case", got)
	}

	w := ed.wrapperNode("MenuBar", "Menu")
	if w.Elem != "Menu" {
		t.Fatalf("wrapper is <%s>, want <Menu>", w.Elem)
	}
	title, ok := w.Attrs["Title"]
	if !ok {
		t.Fatal("the wrapper carries no Title, but <Menu> requires one — " +
			"a wrapper that does not build is worse than a repeated label")
	}

	// THE COSMETIC/SHADOWING LINE, and it is the whole reason this test
	// exists. A repeated LABEL is cosmetic. A repeated MNEMONIC is not: two
	// menus claiming alt+F means one of them never opens, and which one is
	// a fact about tree order no reader of the markup can see. MenuBar's
	// seed carries no "_" today, so the clone claims no accelerator. If a
	// seed grows one, this fails and the limit has to be fixed rather than
	// documented.
	if strings.Contains(title, "_") {
		t.Errorf("the cloned wrapper's Title is %q, which carries a mnemonic marker — "+
			"every wrapper built this way would claim the same accelerator, and all "+
			"but one would silently never open", title)
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
