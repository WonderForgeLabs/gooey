package main

import (
	"testing"
)

// Seeding replaced the per-instance body the old path produced.
//
// addSelected used to set n.Body = n.Attrs["Name"], so the first three
// <Text>s on a canvas read Text1, Text2, Text3 and could be told apart
// while you were looking at them. <Text>'s seed is "<Text>Text</Text>",
// so every palette-inserted copy now reads the same word.
//
// This is not cosmetic and it is not the Name attribute's job. Name is
// the ADDRESS — the outline and hitTest resolve by it and the user never
// sees it on the canvas. The BODY is the only thing rendered, so two
// elements with identical bodies are indistinguishable in the one place
// the user is actually working.

// TestTwoPaletteInsertsOfABodyElementAreDistinguishable is the
// regression. It goes through body_test.go's addFromPalette, which adds
// the way a user does — moving the palette selection and activating it —
// so the seeding path is the one under test rather than a constructed
// node.
func TestTwoPaletteInsertsOfABodyElementAreDistinguishable(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	ed.doc().Elem = "VStack"
	ed.doc().Attrs = map[string]string{"Name": "Root"}
	ed.doc().Kids = nil
	ed.rebuild()
	c.Frame()

	ed.sel = ed.doc()
	a, _ := addFromPalette(t, ed, "Text")
	ed.sel = ed.doc()
	b, _ := addFromPalette(t, ed, "Text")

	if a.Body == "" || b.Body == "" {
		t.Fatalf("a seeded <Text> has an empty body (%q, %q): it measures zero, "+
			"so it is invisible on the canvas AND unhittable, which means the "+
			"user cannot select it in order to give it one", a.Body, b.Body)
	}
	if a.Body == b.Body {
		t.Errorf("two palette-inserted <Text>s both read %q. The body is the only "+
			"thing drawn, so they are indistinguishable in the one place the user "+
			"is working. Name is the address, not the label — it never appears on "+
			"the canvas.", a.Body)
	}
}

// TestAContainerSeedsChildrenKeepTheirOwnBodies is the other side, and
// it is what stops the fix from being "overwrite every body".
//
// A container's seed names its children deliberately — <VStack>'s are
// One and Two, <Grid>'s are A and B — and those are taken VERBATIM. Only
// the inserted element's OWN body is the editor's to make distinct.
func TestAContainerSeedsChildrenKeepTheirOwnBodies(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	ed.doc().Elem = "Canvas"
	ed.doc().Attrs = map[string]string{"Name": "Root"}
	ed.doc().Kids = nil
	ed.rebuild()
	c.Frame()

	ed.sel = ed.doc()
	v, _ := addFromPalette(t, ed, "VStack")
	if len(v.Kids) != 2 {
		t.Fatalf("<VStack>'s seed produced %d children, want 2", len(v.Kids))
	}
	if v.Kids[0].Body != "One" || v.Kids[1].Body != "Two" {
		t.Errorf("the seed's own children read %q/%q, want \"One\"/\"Two\": a "+
			"container's seed names its children and they are taken verbatim",
			v.Kids[0].Body, v.Kids[1].Body)
	}
}
