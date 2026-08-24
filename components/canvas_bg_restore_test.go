package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The other half of #361's exemption, and the half a type-level marker
// gets wrong. A Canvas implements CellPassthrough because its Render
// owns nothing — but a Canvas that DECLARES a Background is filled by
// the framework (composer.go's backgroundProp branch, the same one that
// marks it `covered`), so that instance owns every cell in its bounds
// and restoreUnder must still sweep it.
//
// The shape that catches it: the overlay is a SIBLING of the coloured
// canvas, not its child, so the vacated rect clears to the OUTER
// canvas's background — the terminal default — and only a repaint of
// the coloured canvas can put the colour back. Exempt it and nothing
// repaints at all: zero components painted and a default-coloured hole
// left on screen for good.
//
// Written red against the type-level check: it reported "hiding painted
// 0 components" and a bg of {0 0 0 false} over a red canvas.
func TestCellPassthroughStopsAtADeclaredBackground(t *testing.T) {
	inner := &Canvas{Background: prop.NewSource(render.RGB(200, 0, 0))}
	over := &Text{Content: Str("XXXX")}
	page := &Canvas{Children: []gooey.Component{inner, over}}
	c := gooey.NewComposer(page, 10, 2)
	c.Frame()

	untouched := c.Cells().At(6, 0).Style.Bg
	if !untouched.Set {
		t.Fatalf("the canvas background never filled: cell(6,0) bg = %v", untouched)
	}

	over.LayoutProps().Visibility = gooey.Hidden
	if _, painted := c.Frame(); painted == 0 {
		t.Fatal("hiding the overlay painted nothing — the filled canvas was skipped by restoreUnder")
	}
	if got := c.Cells().At(0, 0).Style.Bg; got != untouched {
		t.Errorf("vacated cell bg = %v, want %v — the canvas fill did not restore", got, untouched)
	}
}

// The control: the same page with no colour on the inner canvas. That
// canvas owns no cells, so it IS exempt and the only repaint is the leaf
// beneath — which is what the counts in menu_test and tooltip_test pin.
func TestCellPassthroughHoldsForAnUncolouredCanvas(t *testing.T) {
	under := &Text{Content: Str("UNDER")}
	inner := &Canvas{Children: []gooey.Component{under}}
	over := &Text{Content: Str("XXXX")}
	page := &Canvas{Children: []gooey.Component{inner, over}}
	c := gooey.NewComposer(page, 10, 2)
	c.Frame()

	over.LayoutProps().Visibility = gooey.Hidden
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("hiding the overlay painted %d components, want 1 (the leaf alone; neither canvas owns cells)", painted)
	}
	if got := row(c.Cells(), 0); got != "UNDER" {
		t.Fatalf("row 0 after hiding = %q, want the occluded text restored", got)
	}
}
