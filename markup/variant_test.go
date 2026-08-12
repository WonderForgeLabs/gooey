package markup

import (
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
