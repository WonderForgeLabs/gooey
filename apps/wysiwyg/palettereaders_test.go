package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/preview"
	"github.com/WonderForgeLabs/gooey/markup"
)

// Two readers asked a DECLARATION question through a PLACEMENT filter,
// and these are the arms that tell the two apart.
//
// ed.palette is what loadPalette offers the toolbox — every Nested and
// NonVisual element dropped. ed.specs is what the catalog DECLARES. A
// document holds elements the palette never offered (a <Tab> comes with
// its <Tabs>, a <Menu> with its <MenuBar>), so anything asked about a
// node already on the canvas has to read the second.
//
// The class was corrected five times before these two — target(),
// specOrBare, grantOf, bodySpec, and then trackAttr and retype's scrub in
// review round 10. All of them were LATENT: the elements that declare
// tracks or a body happen not to be filtered out today. So a fixture
// element is registered in ed.specs and deliberately NOT in ed.palette,
// which is the state a real Nested container is in, and each reader is
// asked about it. Against the palette version both go silent; against
// these they do not.

// registerUnpalettable puts spec in the catalog the editor consults and
// nowhere else, and fails if the palette picked it up — the whole
// discrimination rests on it being absent there.
func registerUnpalettable(t *testing.T, ed *editor, spec markup.ElementSpec) {
	t.Helper()
	if ed.specs == nil {
		t.Fatal("the editor has no specs map")
	}
	ed.specs[spec.Name] = spec
	for _, e := range ed.palette {
		if e.Name == spec.Name {
			t.Fatalf("<%s> is in the palette, so reading either source gives the "+
				"same answer and this test cannot see the difference", spec.Name)
		}
	}
}

func TestTrackAttrReadsWhatAnElementDECLARES(t *testing.T) {
	ed, _ := buildPage(t)
	registerUnpalettable(t, ed, markup.ElementSpec{
		Name:       "Lattice",
		AttrsKnown: true,
		Attrs: []markup.AttrSpec{
			{Name: "Down", Role: markup.RoleRowTracks},
			{Name: "Across", Role: markup.RoleColTracks},
		},
	})

	for _, c := range []struct {
		axis preview.Axis
		want string
	}{
		{preview.AxisRow, "Down"},
		{preview.AxisCol, "Across"},
	} {
		if got := ed.trackAttr("Lattice", c.axis); got != c.want {
			t.Errorf("trackAttr(Lattice, %v) = %q, want %q — the element DECLARES the "+
				"attribute and the guides are drawn over a node already in the "+
				"document, so the palette's placement filter is the wrong source",
				c.axis, got, c.want)
		}
	}
	// NOT VACUOUS in the other direction: an element declaring no tracks
	// still reports nothing, so the arms above are not passing because
	// trackAttr answers everything.
	if got := ed.trackAttr("Text", preview.AxisRow); got != "" {
		t.Errorf("trackAttr(Text, row) = %q, want \"\"", got)
	}
}

func TestRetypeScrubsGrantsFromEveryDeclaredContainer(t *testing.T) {
	ed, _ := buildPage(t)
	registerUnpalettable(t, ed, markup.ElementSpec{
		Name:       "Lattice",
		AttrsKnown: true,
		Grants: markup.Grant{
			Kind:     markup.GrantOffset,
			Attached: []markup.AttrSpec{{Name: "Lattice.Left"}, {Name: "Lattice.Top"}},
		},
	})

	// A child carrying the unpalettable container's attached attributes,
	// as it would after being cut out of one.
	ed.doc().Kids = []*node{{Elem: "Text", Attrs: map[string]string{
		"Name": "T", "Text": "hi", "Lattice.Left": "3", "Lattice.Top": "4",
	}}}
	ed.rebuild()

	ed.retype("VStack")

	kid := ed.doc().Kids[0]
	for _, a := range []string{"Lattice.Left", "Lattice.Top"} {
		if _, still := kid.Attrs[a]; still {
			t.Errorf("%s survived a retype into <VStack>, which does not contribute "+
				"it. The new parent discards it in silence, which is the defect the "+
				"scrub exists to delete — and it survived because the scrub read the "+
				"palette, where a Nested or NonVisual container never appears", a)
		}
	}
	// The child's OWN attributes are not the scrub's business.
	if got := kid.Attrs["Text"]; got != "hi" {
		t.Errorf("the child's own Text is %q after the retype, want %q — the scrub "+
			"removed something no container granted", got, "hi")
	}
}
