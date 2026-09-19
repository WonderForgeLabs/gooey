package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey/components"
)

// The two documents differ in a way a test can SEE — different text — so
// "the variant was picked" is distinguishable from "the base was picked".
// A pair of files that built the same tree would let every assertion here
// pass with the resolution code deleted.
func variantFS() fstest.MapFS {
	return fstest.MapFS{
		"page.gooey":       {Data: []byte(`<Gooey><Text>base</Text></Gooey>`)},
		"page.sixel.gooey": {Data: []byte(`<Gooey><Text>sixel</Text></Gooey>`)},
		"lone.gooey":       {Data: []byte(`<Gooey><Text>lone</Text></Gooey>`)},
	}
}

func loadedText(t *testing.T, variant, name string) string {
	t.Helper()
	ctx := &Context{Variant: variant}
	root, err := Load(variantFS(), name, ctx)
	if err != nil {
		t.Fatalf("Load(%q, variant=%q): %v", name, variant, err)
	}
	txt, ok := root.(*components.Text)
	if !ok {
		t.Fatalf("root is %T, want *components.Text", root)
	}
	return txt.Content.Get()
}

func TestAVariantFileWinsWhenItExists(t *testing.T) {
	if got := loadedText(t, "sixel", "page.gooey"); got != "sixel" {
		t.Errorf("Variant %q loaded %q; page.sixel.gooey exists and must win", "sixel", got)
	}
}

// The fallback is the ordinary case, not an error case: most documents
// will never have a variant, and asking for one must not break them.
func TestAMissingVariantFallsBackToTheBaseDocument(t *testing.T) {
	if got := loadedText(t, "kitty", "page.gooey"); got != "base" {
		t.Errorf("Variant %q loaded %q; there is no page.kitty.gooey, so the base document must load", "kitty", got)
	}
	if got := loadedText(t, "sixel", "lone.gooey"); got != "lone" {
		t.Errorf("loaded %q; lone.gooey has no variants at all", got)
	}
}

// No variant set is what every app that predates this gets, and it must
// mean "load exactly the name I asked for" — even when a variant file is
// sitting right there.
func TestNoVariantIgnoresVariantFilesEntirely(t *testing.T) {
	if got := loadedText(t, "", "page.gooey"); got != "base" {
		t.Errorf("Variant \"\" loaded %q; an unset variant must not consult page.sixel.gooey", got)
	}
}

// The suffix goes BEFORE the extension. This is the naming contract, and
// it is worth pinning separately from resolution: "page.gooey.sixel" would
// resolve identically through Stat while being the wrong file name and no
// longer a .gooey document to any editor or tool.
func TestTheVariantSuffixGoesBeforeTheExtension(t *testing.T) {
	cases := []struct{ name, variant, want string }{
		{"page.gooey", "sixel", "page.sixel.gooey"},
		{"a/b/page.gooey", "kitty", "a/b/page.kitty.gooey"},
		{"noext", "sixel", "noext.sixel"},
		{"page.gooey", "", "page.gooey"},
	}
	for _, c := range cases {
		if got := variantName(c.name, c.variant); got != c.want {
			t.Errorf("variantName(%q, %q) = %q, want %q", c.name, c.variant, got, c.want)
		}
	}
}

// TestASetupsVariantGovernsItsDescendantsNotItself pins the one
// asymmetry in Variant inheritance, and it is the depth surprise
// control() fixes pointing the other way.
//
// resolveVariant runs on the PARENT's Variant when control() loads the
// document, and that is before runSetup exists to return a Context at
// all. So a setup asking for "sixel" cannot retroactively choose the
// file it was itself loaded from — it can only redirect what it
// instantiates. Both halves are asserted here, because only the pair
// distinguishes "the setup's Variant is ignored" from "the setup's
// Variant is authoritative"; either alone is satisfied by one of the two
// wrong readings. Raised in review of #490.
func TestASetupsVariantGovernsItsDescendantsNotItself(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey":        {Data: []byte(`<Gooey><Card/></Gooey>`)},
		"card.gooey":        {Data: []byte(`<Gooey><VStack><Text>card base</Text><Panel/></VStack></Gooey>`)},
		"card.sixel.gooey":  {Data: []byte(`<Gooey><VStack><Text>card sixel</Text><Panel/></VStack></Gooey>`)},
		"panel.gooey":       {Data: []byte(`<Gooey><Text>panel base</Text></Gooey>`)},
		"panel.sixel.gooey": {Data: []byte(`<Gooey><Text>panel sixel</Text></Gooey>`)},
	}
	ctx := &Context{
		Includes: fsys, // <Panel/> resolves by convention, off the child context
		Components: map[string]Builder{
			"Card": UserControl(fsys, "card.gooey", func(e Element, parent *Context) (*Context, error) {
				return &Context{Variant: "sixel"}, nil
			}),
		},
	}
	// THE PAGE ASKS FOR NO VARIANT, which is what makes the setup the
	// only thing that could have chosen one.
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := renderToString(t, w, 30, 6)
	if !strings.Contains(out, "card base") {
		t.Errorf("the control's own document was resolved against its SETUP's "+
			"Variant. resolveVariant runs before runSetup, so <Card/> on a "+
			"plain page must load card.gooey however the setup answers:\n%s", out)
	}
	if !strings.Contains(out, "panel sixel") {
		t.Errorf("the setup's Variant did not reach the control's descendants, "+
			"which is the half it does govern — <Panel/> inside a control "+
			"whose setup asked for sixel must load panel.sixel.gooey:\n%s", out)
	}
}
