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
	bar, err := menuPage(t, `Icon="assets/open.png"`)
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
