package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestALiveHostCostsAnAllocationPerMotionEvent is the arm the root
// package cannot write: gooey.TestTheHitWalkAllocatesNothing measures
// the walk against fixtures in its own package, and components imports
// gooey, so the two shipped hosts that actually pay this are only
// reachable from here.
//
// The walk allocates nothing of its own. ToastHost.ChildComponents and
// AdornmentLayer.ChildComponents each build a fresh slice per call, and
// HitTest runs on every UNCAPTURED motion report — ?1003h sends one per
// cell crossed — so while either host holds anything the user pays one
// allocation per cell of UNCAPTURED pointer travel. In cmd/toolkit,
// apps/wysiwyg and docs/learn/examples/07-app-chrome the host spans
// every row, so that is the whole screen.
//
// A DRAG PAYS NONE OF IT, which is the scope the unqualified sentence
// here got wrong: under ANY capture DispatchMouse walks for no move at
// all, so dragging on the wysiwyg canvas past a live tip costs zero.
// The condition is m.captor == nil, not !m.held — and "under a held
// capture", which is what this said until review of #458, is a claim
// NARROWER than the code in a file whose subject is cost claims
// drifting from the code. Its own example falls outside it: the canvas
// drag named in the same sentence takes an IMPLICIT capture from the
// press (apps/wysiwyg/components/preview/preview.go — "THE PRESS
// ALREADY CAPTURED THIS PANE … No CaptureMouse call is needed"), which
// is the commonest drag in the framework and the one "held" excludes.
// The reads that remain are the uncaptured move, the unheld press and
// the release — the last two not motion events.
//
// EMPTY IS GENUINELY ZERO, and that is not a courtesy of the walk:
// make([]T, 0) returns the zero base and allocates nothing. It is the
// reason the cost is invisible until a toast is up, and the reason a
// test that never showed one would have measured 0 and confirmed the
// wrong sentence. Raised in review of #458; the fix — a stored slice
// maintained on Show/Dismiss and Add/Remove — is
// https://github.com/WonderForgeLabs/gooey/issues/513, and it moves the
// numbers below along with mouse.go, docs/architecture.md and CLAUDE.md.
func TestALiveHostCostsAnAllocationPerMotionEvent(t *testing.T) {
	t.Run("toast", func(t *testing.T) {
		host, page, content := toastPage(20, 5)
		c := gooey.NewComposer(page, 20, 5)
		t.Cleanup(c.Close)
		c.Frame()
		m := gooey.NewFocusManager(page)

		if hit := m.HitTest(0, 0); hit != gooey.Component(content) {
			t.Fatalf("HitTest(0,0) = %T before any toast; want the content under the "+
				"host, so the counts below are a walk that descended through it", hit)
		}
		if n := testing.AllocsPerRun(100, func() { m.HitTest(0, 0) }); n != 0 {
			t.Errorf("an empty ToastHost cost %v allocations per HitTest, want 0 — "+
				"make([]T, 0) allocates nothing, which is why this cost stays "+
				"invisible until a toast is shown", n)
		}

		host.Show("saved")
		c.Frame()
		if n := testing.AllocsPerRun(100, func() { m.HitTest(0, 0) }); n != 1 {
			t.Errorf("a ToastHost with one live toast cost %v allocations per HitTest, "+
				"want 1 — the fresh slice ChildComponents builds. If this is now 0, "+
				"#513 is fixed and mouse.go, docs/architecture.md and CLAUDE.md say "+
				"otherwise; if it is more, something on the walk grew a second one", n)
		}
	})

	t.Run("adornment", func(t *testing.T) {
		_, _, marker, _, page := formPage(30)
		c := gooey.NewComposer(page, 30, 4)
		t.Cleanup(c.Close)
		c.Frame()
		if !marker.IsShown() {
			t.Fatal("the form's marker is not shown, so the layer is empty and the " +
				"count below is measuring the empty case the toast arm already covers")
		}
		m := gooey.NewFocusManager(page)
		if n := testing.AllocsPerRun(100, func() { m.HitTest(0, 0) }); n != 1 {
			t.Errorf("an AdornmentLayer holding one marker cost %v allocations per "+
				"HitTest, want 1 — the same fresh slice, at the top rank and for as "+
				"long as the anchor stays invalid (#513)", n)
		}
	})
}
