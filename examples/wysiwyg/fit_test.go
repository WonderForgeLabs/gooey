package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/components"
)

// The editor's minimum size, and the failure it used to produce.
//
// # What used to be here, and why it is not
//
// Four of this file's five tests read the SHIPPED page: they found
// <Grid Name="Shell">, took both minimums from its declared tracks, and
// asserted the off-screen overflow one row below the hard one, the
// visibility swap, and the numbers in the cramped message. That was the
// right design — the numbers came from the markup, so editing Rows= moved
// them and they could not drift.
//
// The page is now empty, pending the canvas-first rebuild, so there are no
// tracks to read. Those four tests are in git at b41aa2a and they are not
// obsolete; they are waiting for a shell. Rewriting them against a Grid
// built in the test would have kept the file green while deleting the
// only thing they asserted — that the SHIPPED layout has a minimum and
// says so — and a test that cannot fail for the reason it names is worse
// than an absent one.
//
// WHAT THE NEW SHELL OWES THIS FILE: the same four assertions, pointed at
// whatever it declares. fit.go's machinery is untouched and still derives
// both minimums from live track definitions; watchFit already no-ops when
// no element is named Shell, which is why the empty page costs nothing at
// runtime.
//
// The one test below survives because it never read the page.

// TestAnAutoTrackIsRefusedRatherThanGuessed. Every way of guessing an
// Auto track's extent makes the minimum too SMALL, which means the fit
// check passes at a size that does not fit — the silent misfit this
// whole mechanism removes, reintroduced by the fix.
func TestAnAutoTrackIsRefusedRatherThanGuessed(t *testing.T) {
	g := &components.Grid{
		Rows: []components.GridLen{components.Fixed(1), components.Auto()},
		Cols: []components.GridLen{components.Star(1)},
	}
	if _, _, err := minimumFor(g); err == nil {
		t.Fatal("an Auto track must be refused, not guessed")
	} else if !strings.Contains(err.Error(), "Auto") {
		t.Errorf("the error must name the problem, got: %v", err)
	}
	// Control: without the Auto track the same shape computes fine.
	g.Rows = []components.GridLen{components.Fixed(1), components.Star(1)}
	if _, _, err := minimumFor(g); err != nil {
		t.Fatalf("the control failed too, so the refusal proved nothing: %v", err)
	}
}

// TestTheTwoMinimumsDifferByTheStarTracks keeps the distinction the old
// shell tests were built on, since it is a property of trackMinimum
// rather than of any particular page: a star track contributes NOTHING to
// the hard minimum — it absorbs the shortfall down to zero — and starMin
// to the usable one.
//
// This is the part that was written wrong first, with one number and the
// other one's mechanism, so it is worth holding onto while the shell is
// away.
func TestTheTwoMinimumsDifferByTheStarTracks(t *testing.T) {
	g := &components.Grid{
		Rows: []components.GridLen{components.Fixed(1), components.Star(1), components.Fixed(12)},
		Cols: []components.GridLen{components.Star(2), components.Fixed(46)},
	}
	hard, usable, err := minimumFor(g)
	if err != nil {
		t.Fatal(err)
	}
	if hard.Rows != 13 || hard.Cols != 46 {
		t.Errorf("hard = %s, want 46×13: the fixed tracks alone, star tracks contributing nothing", hard)
	}
	if usable.Rows != 13+starMin || usable.Cols != 46+starMin {
		t.Errorf("usable = %s, want the fixed tracks plus %d per star track", usable, starMin)
	}
	if hard == usable {
		t.Error("the two minimums coincide, so nothing here distinguishes them")
	}
}

// TestTheCrampedMessageSaysBothSizes — "too small" without the numbers
// leaves the user guessing how much to drag. The message is still what a
// future shell will show, so its content stays pinned even with no shell
// to trigger it.
func TestTheCrampedMessageSaysBothSizes(t *testing.T) {
	have, want := fitSize{Cols: 40, Rows: 11}, fitSize{Cols: 49, Rows: 17}
	msg := cramMsg(have, want)
	for _, s := range []string{"40", "11", "49", "17"} {
		if !strings.Contains(msg, s) {
			t.Errorf("the message must name %q; got:\n%s", s, msg)
		}
	}
}
