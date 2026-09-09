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

	// The parent as the real gesture supplies it: a <MenuBar> that
	// already holds the seed's <Menu>. Passing an EMPTY MenuBar would
	// pass whatever unshadowMnemonic did, because there is nothing to
	// collide with.
	bar := &node{Elem: "MenuBar", Kids: []*node{
		{Elem: "Menu", Attrs: map[string]string{"Title": "File"}},
	}}
	w := ed.wrapperNode(bar, "Menu")
	if w.Elem != "Menu" {
		t.Fatalf("wrapper is <%s>, want <Menu>", w.Elem)
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
	w := ed.wrapperNode(&node{Elem: "MenuBar"}, "Menu")
	if got := w.Attrs["Title"]; got != "File" {
		t.Errorf("the wrapper's Title is %q, want the seed's %q unchanged — nothing "+
			"in the bar claims an accelerator, so there is nothing to avoid",
			got, "File")
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
