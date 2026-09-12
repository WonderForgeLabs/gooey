package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// The LIVE-DISPATCH regression for issue #125: menu clicks must survive
// the real event pipeline — input.Event through Composer.Handle, hit
// testing, focus-follows-click, capture — on a page shaped like
// cmd/toolkit, where a full-page ToastHost is declared AFTER the
// MenuBar (the toast layer outranks the popup layer, so it is topmost
// wherever it is declared — see gooey.OverlayRanker and #439).
//
// Wave 2's tests synthesized events directly on the bar's handlers and
// missed this: hit-testing used to treat any Bounded container as
// opaque, so the whole-page toast layer swallowed every press before
// the bar — an earlier sibling, never on the bubble path — could see
// it. The fix is the HitTestTransparent seam (#129): ToastHost opts
// out of hit-testing while Toast leaves stay hittable. This test pins
// that fix from the menu's side, through the pipeline the bug actually
// lived in.
func TestMenuClicksThroughLiveDispatchUnderToastLayer(t *testing.T) {
	saved := 0
	bar := &MenuBar{Menus: []Menu{
		{Title: "File", Items: []MenuItem{
			{Text: "Save", Action: gooey.Command(func() { saved++ })},
			{Text: "Quit", Action: gooey.Command(func() {})},
		}},
		{Title: "Edit", Items: []MenuItem{
			{Text: "Copy", Action: gooey.Command(func() {})},
		}},
	}}
	btn := &Button{Content: Str("elsewhere"), Click: gooey.Command(func() {})}
	page := &Canvas{Children: []gooey.Component{
		gooey.L(&Text{Content: Str(strings.Repeat("#", 30))}, gooey.Layout{Top: 1}),
		gooey.L(btn, gooey.Layout{Top: 6, Left: 25}),
		bar,          // anywhere: the dropdown's surface is an Overlay and is lifted
		&ToastHost{}, // full page — the demo's notification layer
	}}
	c := gooey.NewComposer(page, 40, 10)
	c.Frame()

	// Real events, real routing: a click arrives as a press/release pair
	// through Composer.Handle, exactly as App.handle delivers it.
	click := func(x, y int) {
		c.Handle(input.MouseOf(input.MouseEvent{Kind: input.MousePress, X: x, Y: y}))
		c.Handle(input.MouseOf(input.MouseEvent{Kind: input.MouseRelease, X: x, Y: y}))
	}

	click(2, 0) // the File title
	if !bar.IsOpen() {
		t.Fatal("clicking the File title through the live pipeline did not open the menu (#125)")
	}
	c.Frame()

	click(2, 1+1) // the Save row inside the dropdown (border at y=1, first item at y=2)
	if saved != 1 {
		t.Fatalf("clicking the Save item ran it %d times, want 1", saved)
	}
	if bar.IsOpen() {
		t.Fatal("activating an item did not close the menu")
	}
}
