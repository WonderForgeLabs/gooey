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
