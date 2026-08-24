package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// What the palette actually produces, measured by WALKING IT.
//
// This is the number that says whether the seeding rewire fixed the
// user-visible bug, and it is deliberately derived rather than written
// down: a list of which elements are broken is stale the first time the
// vocabulary changes, and stale in the direction that reports green.
//
// The measurement before markup.Seeded, on a palette of 28: two
// elements that would NOT LOAD (<Image>, <MenuBar>) and four that
// measured 0x0 (<ActivityBar>, <ButtonBar>, <HStack>, <VStack>).
//
// Zero-size is the worse half of the two and the reason this test
// measures at all rather than only building. hitTest never returns a
// zero-size component, so an element that measures 0x0 is invisible on
// the canvas AND unselectable — the user adds it, nothing appears, and
// there is no way to click the thing in order to give it the content
// that would make it appear. "It builds" would have reported all four
// of those as fine.

// findNamed is the component the palette just added, looked up by the
// Name the editor gave it.
func findNamed(ed *editor) gooey.Component { return ed.docCtx.Named[ed.sel.Attrs["Name"]] }

func TestEveryPaletteEntryLoadsAndOccupiesSpace(t *testing.T) {
	base := newEditor(editorFS())
	if len(base.palette) == 0 {
		t.Fatal("the palette is empty, so this test can no longer discriminate")
	}

	var broken, zero []string
	for i, spec := range base.palette {
		ed := newEditor(editorFS())
		ed.rebuild()
		ed.paletteSel.Set(i)
		ed.addSelected()

		if s := ed.status.Get(); strings.HasPrefix(s, "✗") {
			broken = append(broken, spec.Name+": "+s)
			continue
		}
		if _, err := markup.Build([]byte(ed.source.Get()), ed.docCtx); err != nil {
			broken = append(broken, spec.Name+": "+err.Error())
			continue
		}
		// A non-visual element has no bounds to occupy, so the
		// zero-size question does not apply to it. Deriving that from
		// the catalog rather than from a list of names is the point.
		if spec.NonVisual {
			continue
		}
		c := findNamed(ed)
		if c == nil {
			broken = append(broken, spec.Name+": built, but nothing was registered under its Name")
			continue
		}
		// gooey.MeasureChild, NOT c.Measure. Measure is the component's
		// intrinsic size; MeasureChild is what a PARENT calls, and it
		// is the only thing that applies the margin/size/align sandwich
		// — so a declared Width or Height is invisible to c.Measure.
		// Calling the bare Measure here reported <ActivityBar> as 0x0
		// when its seed's Height="8" was doing its job perfectly: the
		// harness was skipping the very step under test. Which one to
		// call is the question the framework already answers, and the
		// answer for "how big is this on a canvas" is the parent's.
		sz := gooey.MeasureChild(c, gooey.Size{W: 60, H: 20})
		if sz.W == 0 || sz.H == 0 {
			zero = append(zero, spec.Name)
		}
	}

	t.Logf("palette of %d: %d would not load, %d measure 0x0", len(base.palette), len(broken), len(zero))
	for _, b := range broken {
		t.Errorf("adding <%s> does not load; the user clicks and gets an error", b)
	}
	for _, z := range zero {
		t.Errorf("adding <%s> measures 0x0: invisible on the canvas and, because hitTest "+
			"never returns a zero-size component, unselectable in order to be fixed", z)
	}
}

// The palette's entries must each be seedable in the first place. This
// separates "the seed is wrong" from "there is no seed", which the test
// above reports identically — the fail-open shape markup's own
// TestEveryElementDeclaresASeed exists to close, applied here to the
// REGISTERED half of the vocabulary that one cannot see.
func TestEveryPaletteEntryIsSeedableOrKnowinglyOpaque(t *testing.T) {
	ed := newEditor(editorFS())
	for _, spec := range ed.palette {
		if strings.TrimSpace(spec.Seed) != "" {
			continue
		}
		// No Seed is legal only where the catalog admits it knows
		// nothing: a Components-registered Builder is a func, so its
		// attributes and its shape are both unknowable and the bare
		// element is the honest seed. A DECLARED element with no Seed
		// is an omission.
		if spec.AttrsKnown {
			t.Errorf("<%s> declares its attributes but no Seed, so the palette has to guess "+
				"what to insert for it; see markup.ElementDef.Seed", spec.Name)
		}
	}
}
