package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey/components"
)

// The markup surface for menu item icons (#400).
//
// <MenuItem> is a PSEUDO-ELEMENT — buildMenuBar reads it off e.Children
// as data and it never reaches build(), so none of the ordinary
// machinery applies to it. Every one of these tests exists because the
// generic path that would cover the equivalent attribute on a real
// element does not run here.

// menuPage loads one <MenuItem> with the given attributes and returns
// the built item. The fixture carries a real PNG so a literal Icon has
// something to resolve to.
func menuPage(t *testing.T, attrs string) (*components.MenuBar, error) {
	t.Helper()
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey>
  <VStack>
    <MenuBar Name="bar">
      <Menu Title="_File">
        <MenuItem Text="_Open" ` + attrs + ` Command="{{.Open}}"/>
      </Menu>
    </MenuBar>
  </VStack>
</Gooey>`)},
		"assets/open.png": {Data: pngBytes(t, 4, 4)},
	}
	ctx := &Context{Values: map[string]any{"Open": func() {}}}
	if _, err := Load(fsys, "page.gooey", ctx); err != nil {
		return nil, err
	}
	return Find[*components.MenuBar](ctx, "bar")
}

// TestAMenuItemIconLoadsFromThePageFS is the literal form, and it
// resolves through the SAME fs.FS <Image Src> does — assets ship the
// way markup does.
func TestAMenuItemIconLoadsFromThePageFS(t *testing.T) {
	bar, err := menuPage(t, `Icon="assets/open.png" IconRune="O"`)
	if err != nil {
		t.Fatal(err)
	}
	it := bar.Menus[0].Items[0]
	if it.Icon == nil {
		t.Fatal("Icon= named a file in the page's FS and the built item has no image")
	}
	if got := it.Icon.Bounds(); got.Dx() != 4 || got.Dy() != 4 {
		t.Fatalf("decoded icon is %v, want 4×4", got)
	}
}

// TestAMissingIconAssetIsALoadError — resolvable at load, so it fails
// at load. The path has to be in the message or the error is a hunt.
func TestAMissingIconAssetIsALoadError(t *testing.T) {
	_, err := menuPage(t, `Icon="assets/nope.png"`)
	if err == nil {
		t.Fatal("a MenuItem naming an asset that does not exist loaded clean")
	}
	if !strings.Contains(err.Error(), "assets/nope.png") {
		t.Errorf("the error does not name the path it could not find: %v", err)
	}
	// NAME THE WINNER. Before Icon was declared this test passed on
	// "no such attribute", which names the path too and says nothing
	// about the asset — a negative assertion passes for any reason, so
	// it has to exclude the reason it is not testing.
	if strings.Contains(err.Error(), "no such attribute") {
		t.Errorf("this is the undeclared-attribute error, not a missing asset: %v", err)
	}
}

// TestABoundMenuItemIconIsRefused is the same contract Text already
// states, and for the same mechanical reason: MenuItem.Icon is a plain
// field read while painting, so a handle resolved here would be sampled
// once and silently stop tracking. Refusing says so at load.
//
// This is the clause that decides the attribute is BindsLiteral rather
// than BindsEither, and a consumer reading the catalog must see the
// same answer the loader enforces.
func TestABoundMenuItemIconIsRefused(t *testing.T) {
	_, err := menuPage(t, `Icon="{{.Logo}}"`)
	if err == nil {
		t.Fatal("<MenuItem Icon=\"{{.Logo}}\"> loaded clean; a bound icon would freeze at its first value")
	}
	if !strings.Contains(err.Error(), "Icon") {
		t.Errorf("the error does not name the attribute: %v", err)
	}
	// Same reason as above, and one more: the message must explain the
	// freeze rather than merely refuse, because "Icon takes a file
	// path" is the whole difference from <Image Src>, which does take
	// a binding.
	if strings.Contains(err.Error(), "no such attribute") {
		t.Errorf("this is the undeclared-attribute error, not a refused binding: %v", err)
	}
	if !strings.Contains(err.Error(), "sampled once") {
		t.Errorf("the error refuses the binding without saying why it cannot work: %v", err)
	}
}

// TestAMenuItemIconRuneIsExactlyOneRune. The cell-plane tier is a
// single glyph in a fixed gutter — two runes would be clipped to one
// and a half, which is not drawable, so the second one is refused where
// it can be explained rather than lost where it cannot.
func TestAMenuItemIconRuneIsExactlyOneRune(t *testing.T) {
	bar, err := menuPage(t, `IconRune="○"`)
	if err != nil {
		t.Fatal(err)
	}
	if got := bar.Menus[0].Items[0].IconRune; got != '○' {
		t.Fatalf("IconRune = %q, want ○", got)
	}
	if _, err := menuPage(t, `IconRune="ab"`); err == nil {
		t.Error("IconRune=\"ab\" loaded clean; the gutter holds one glyph")
	}
}

// TestAWideIconRuneIsAccepted — the gutter is three cells and measured
// in columns, so an emoji fits. This is the arm that would go red if
// the loader validated with a rune-vs-cell mix-up.
func TestAWideIconRuneIsAccepted(t *testing.T) {
	bar, err := menuPage(t, `IconRune="📁"`)
	if err != nil {
		t.Fatalf("a two-cell icon rune was refused: %v", err)
	}
	if got := bar.Menus[0].Items[0].IconRune; got != '📁' {
		t.Fatalf("IconRune = %q, want 📁", got)
	}
}

// TestTheIconAttributesAreDeclaredOnMenuItem. The catalog is what a
// designer's property inspector reads, and #429's whole point was that
// an element consumed as data still has a declared surface. An
// implementation that read the attributes without declaring them would
// work at runtime and be invisible in the tool.
func TestTheIconAttributesAreDeclaredOnMenuItem(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	var item *ElementSpec
	for i, e := range ctx.Catalog() {
		if e.Name == "MenuItem" {
			item = &ctx.Catalog()[i]
			break
		}
	}
	if item == nil {
		t.Fatal("MenuItem is not in the catalog")
	}
	want := map[string]Binds{"Icon": BindsLiteral, "IconRune": BindsLiteral}
	for _, a := range item.Attrs {
		if b, ok := want[a.Name]; ok {
			if a.Binds != b {
				t.Errorf("%s declares Binds=%q, want %q — the loader refuses a binding", a.Name, a.Binds, b)
			}
			delete(want, a.Name)
		}
	}
	for n := range want {
		t.Errorf("MenuItem does not declare %s, so no property inspector can offer it", n)
	}
}

// A BOUND IconRune is refused by a message about BINDING, not about
// counting glyphs.
//
// Icon had a purpose-built refusal naming the freeze; IconRune had none,
// so a binding fell through to the glyph count and came back "IconRune
// is one glyph — the icon gutter holds exactly one, and 10 were given".
// The author's mistake is "I tried to bind this"; the message counted
// the characters of the template. Nothing in checkAttrs enforces
// BindsLiteral — it validates attribute NAMES — so menuItemIcon is the
// only place the declared Binds is enforced, and for IconRune it was
// enforced by accident. Found in review of #455.
//
// The assertion is on the message's SUBJECT rather than on err != nil,
// because the bug was never a missing error. It was the wrong one.
func TestABoundMenuItemIconRuneIsRefusedForBeingBound(t *testing.T) {
	const page = `<Gooey><MenuBar><Menu Title="_File">` +
		`<MenuItem Text="_Open" IconRune="{{.Glyph}}"/></Menu></MenuBar></Gooey>`
	fsys := fstest.MapFS{"p.gooey": &fstest.MapFile{Data: []byte(page)}}
	_, err := Load(fsys, "p.gooey", &Context{Values: map[string]any{"Glyph": "x"}})
	if err == nil {
		t.Fatal("a bound IconRune loaded clean; it would be sampled once and frozen")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not a binding") {
		t.Errorf("the refusal does not name BINDING as the mistake:\n\t%s", msg)
	}
	if strings.Contains(msg, "were given") {
		t.Errorf("the refusal counted glyphs, which describes a different mistake:\n\t%s", msg)
	}
}

// A separator carries nothing else, and an unresolvable asset on one is
// a LOAD ERROR like it is anywhere else.
//
// The Separator short-circuit ran before every other attribute was read,
// so this exact markup loaded clean while the same Icon on a
// non-separator item was a load error naming the path
// (TestAMissingIconAssetIsALoadError). One spelling kept the
// "everything resolvable fails at load" posture and the other dropped
// it. Found in review of #455.
func TestASeparatorRefusesTheAttributesItWouldIgnore(t *testing.T) {
	for _, tc := range []struct{ attr, val string }{
		{"Icon", "assets/nope.png"},
		{"IconRune", "x"},
		{"Text", "not shown"},
		{"Gesture", "ctrl+q"},
	} {
		page := `<Gooey><MenuBar><Menu Title="_File">` +
			`<MenuItem Separator="true" ` + tc.attr + `="` + tc.val + `"/>` +
			`</Menu></MenuBar></Gooey>`
		fsys := fstest.MapFS{"p.gooey": &fstest.MapFile{Data: []byte(page)}}
		_, err := Load(fsys, "p.gooey", &Context{})
		if err == nil {
			t.Errorf("<MenuItem Separator=%q %s=%q> loaded clean — it is accepted and silently ignored",
				"true", tc.attr, tc.val)
			continue
		}
		if !strings.Contains(err.Error(), tc.attr) {
			t.Errorf("the refusal for %s does not name the attribute:\n\t%s", tc.attr, err)
		}
	}
}

// The bare separator every page in this repo actually writes still
// loads. Without this the test above is satisfiable by refusing all
// separators.
func TestABareSeparatorStillLoads(t *testing.T) {
	const page = `<Gooey><MenuBar><Menu Title="_File">` +
		`<MenuItem Text="_Open"/><MenuItem Separator="true"/><MenuItem Text="_Quit"/>` +
		`</Menu></MenuBar></Gooey>`
	fsys := fstest.MapFS{"p.gooey": &fstest.MapFile{Data: []byte(page)}}
	if _, err := Load(fsys, "p.gooey", &Context{}); err != nil {
		t.Fatalf("a bare separator is refused: %v", err)
	}
}

// TestAZeroWidthIconRuneIsRefused is the load-time half of the gutter
// overrun. One rune is not one column: a combining mark passes the count
// check beside this one and measures zero, and Buffer.SetString still
// spends a cell on it — so the gutter measures three and paints four.
//
// The assertion names the measurement, not merely that an error exists,
// because the count check next door already refuses plenty and "err !=
// nil" would pass against the bug. Found in review of #455.
func TestAZeroWidthIconRuneIsRefused(t *testing.T) {
	const page = `<Gooey><MenuBar><Menu Title="_File">` +
		`<MenuItem Text="_Open" IconRune="&#x308;"/></Menu></MenuBar></Gooey>`
	fsys := fstest.MapFS{"p.gooey": &fstest.MapFile{Data: []byte(page)}}
	_, err := Load(fsys, "p.gooey", &Context{})
	if err == nil {
		t.Fatal("a zero-width IconRune loaded clean; it would steal a cell the gutter did not reserve")
	}
	if !strings.Contains(err.Error(), "measures zero columns") {
		t.Errorf("the refusal does not name the measurement, so it is a different mistake:\n\t%v", err)
	}
}

// TestAOneCellIconRuneStillLoads keeps the refusal above from being
// satisfiable by rejecting every IconRune.
func TestAOneCellIconRuneStillLoads(t *testing.T) {
	const page = `<Gooey><MenuBar><Menu Title="_File">` +
		`<MenuItem Text="_Open" IconRune="○"/></Menu></MenuBar></Gooey>`
	fsys := fstest.MapFS{"p.gooey": &fstest.MapFile{Data: []byte(page)}}
	if _, err := Load(fsys, "p.gooey", &Context{}); err != nil {
		t.Fatalf("a one-cell IconRune is refused: %v", err)
	}
}

// TestASeparatorTreatsAnEmptyAttributeTheWayEveryOtherReadDoes.
//
// The refusal above gated on PRESENCE (`_, ok := ic.Attrs[a]`) while
// every other read in the same builder gates on a non-empty VALUE
// (`if raw := strings.TrimSpace(ic.Attrs["Icon"]); raw != ""`). So
// `Icon=""` was fatal on a separator and a no-op three lines later on
// anything else, which is one attribute spelling meaning two things.
//
// The error text is the tell: it says the attribute "would be accepted
// and silently ignored", and for an empty value nothing would be — there
// is nothing to ignore. A diagnostic that describes a consequence that
// cannot happen is the same defect the refusal was added to remove, one
// level up. Found in review of #455.
// separatorRejects is every attribute a separator refuses, taken from
// the declaration the loader ranges over. One spelling of the set, for
// the guard and for the thing guarded.
func separatorRejects() []string {
	var out []string
	for _, a := range defMenuItem.Attrs {
		if a.Name != "Separator" {
			out = append(out, a.Name)
		}
	}
	return out
}

func TestASeparatorTreatsAnEmptyAttributeTheWayEveryOtherReadDoes(t *testing.T) {
	// DERIVED, for the reason the refusal itself now is: a hand-written
	// copy of defMenuItem.Attrs minus Separator goes stale silently, and
	// this arm would then pass while saying nothing about the new
	// attribute. Raised in review of #455.
	for _, attr := range separatorRejects() {
		page := `<Gooey><MenuBar><Menu Title="_File">` +
			`<MenuItem Separator="true" ` + attr + `=""/>` +
			`</Menu></MenuBar></Gooey>`
		fsys := fstest.MapFS{"p.gooey": &fstest.MapFile{Data: []byte(page)}}
		if _, err := Load(fsys, "p.gooey", &Context{}); err != nil {
			t.Errorf(`<MenuItem Separator="true" %s=""> is refused, but the same `+
				`empty value is a no-op on a non-separator item:%s%v`, attr, "\n\t", err)
		}
	}
}

// TestAnIconWithoutAnIconRuneIsRefused.
//
// The gutter is reserved UNCONDITIONALLY — three cells whenever any item
// in the menu carries either field, protocol or no protocol. That is the
// spec's decision and it is the right one: the capability probe answers
// after the first frame, so reserving conditionally would visibly reflow
// the dropdown on a terminal that turns out to support pixels.
//
// What it leaves uncovered is an item carrying ONLY an Icon on a
// terminal that never gets a protocol. iconGutter falls through to
// spaces(w), so those three columns stay blank for the life of the
// program — not for one frame, forever — and nothing anywhere says why.
// It is the separator case's shape exactly: markup accepted, then
// silently drawing nothing.
//
// The refusal is not "IconRune is a fallback for Icon". The spec is
// explicit that neither field degrades to the other and that the tiers
// draw DIFFERENT THINGS; requiring both is what authoring two tiers
// means, and it is resolvable at load. Found in review of #455.
func TestAnIconWithoutAnIconRuneIsRefused(t *testing.T) {
	_, err := menuPage(t, `Icon="assets/open.png"`)
	if err == nil {
		t.Fatal(`<MenuItem Icon="assets/open.png"> with no IconRune loaded clean — ` +
			`on a terminal with no graphics protocol it reserves three blank columns forever`)
	}
	if !strings.Contains(err.Error(), "IconRune") {
		t.Errorf("the refusal does not name the missing field:\n\t%s", err)
	}
}

// TestTheIconHelpSaysWhatTheLoaderEnforces ties the inline help to the
// rule it describes, in ONE test, because the pair is the claim.
//
// The Doc string read "set IconRune for everywhere else", which is
// advice. menuItemIcon makes it a hard load error. That Doc is what the
// wysiwyg property grid renders inline at the moment of the edit, and
// the grid offers Icon and IconRune as independent rows — so an author
// filled in Icon, read that the other field was optional polish, and
// produced a document the loader refuses. The reference row and the
// Separator Doc two lines down had both been corrected in the same diff;
// only the place a designer actually reads it stayed advisory.
//
// Asserted together with the refusal so the sentence expires with the
// behaviour: if the pairing check is ever dropped, this fails on the
// FIRST half and the help does not quietly become the wrong kind of
// wrong. Raised in review of #455.
func TestTheIconHelpSaysWhatTheLoaderEnforces(t *testing.T) {
	if _, err := menuPage(t, `Icon="assets/open.png"`); err == nil {
		t.Fatal("an Icon without an IconRune loads, so the help below would be " +
			"describing a load error that no longer exists")
	}

	ctx := &Context{Values: map[string]any{}}
	var doc string
	var found bool
	for _, e := range ctx.Catalog() {
		if e.Name != "MenuItem" {
			continue
		}
		for _, a := range e.Attrs {
			if a.Name == "Icon" {
				doc, found = a.Doc, true
			}
		}
	}
	if !found {
		t.Fatal("MenuItem does not declare Icon, so the property grid shows no help " +
			"for it at all")
	}
	if !strings.Contains(doc, "IconRune") || !strings.Contains(doc, "LOAD ERROR") {
		t.Errorf("the Icon help a designer reads while editing does not say the "+
			"pairing is a load error:\n\t%s\n"+
			"The grid offers Icon and IconRune as independent rows, so help that "+
			"reads as advice walks the author into a document that will not open", doc)
	}
}

// TestAnIconRuneAloneStillLoads — the cell tier on its own is a complete
// menu item, and only the pixel tier needs a partner. Without this the
// refusal above is satisfiable by demanding both fields always.
func TestAnIconRuneAloneStillLoads(t *testing.T) {
	if _, err := menuPage(t, `IconRune="O"`); err != nil {
		t.Fatalf("an item carrying only IconRune is refused: %v", err)
	}
}

// TestTheIconPairingCheckRunsLast. A missing asset and a bound Icon each
// describe a DIFFERENT mistake, and both markups also lack an IconRune —
// so a pairing check hoisted above them would answer every one of these
// with "an Icon needs an IconRune beside it" and bury the real cause.
// This is the ordering assertion, and nothing else makes it.
func TestTheIconPairingCheckRunsLast(t *testing.T) {
	for _, tc := range []struct{ attrs, want string }{
		{`Icon="assets/nope.png"`, "nope.png"},
		{`Icon="{{.Logo}}"`, "not a binding"},
	} {
		_, err := menuPage(t, tc.attrs)
		if err == nil {
			t.Errorf("%s loaded clean", tc.attrs)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s was refused as %q, want the refusal to mention %q — "+
				"the pairing check is running before the one that knows the real cause",
				tc.attrs, err, tc.want)
		}
	}
}
