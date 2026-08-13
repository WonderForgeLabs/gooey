package main

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The editor has an undeclared minimum size, and it used to fail at that
// size in the worst available way: silently, off-screen.
//
// # There are TWO minimums, and conflating them is how this got written
// # wrong the first time
//
// The shell is a <Grid Rows="1,1*,12,1" Cols="2*,46">.
//
// The HARD minimum is the sum of the FIXED tracks — 14 rows here.
// Grid.Arrange sizes star tracks with distributeStars
// (components/grid.go), which hands them max(0, extent-used); once the
// fixed tracks alone exceed the extent the star track goes to zero and
// the fixed tracks keep their full declared size. offsets() then
// accumulates them unclamped and the tail is arranged past b.Y+b.H.
// Measured, not reasoned: at 49x13 the StatusBar and the help Text are
// both arranged at Y=13 on a 13-row screen, with no error anywhere.
//
// The USABLE minimum adds starMin per star track — 17 rows. Between the
// two nothing overflows; the preview and palette simply have a <Border>
// with no interior left to draw in.
//
// The first version of this file had only one number, the usable one,
// and described it with the hard one's mechanism. The red test skipped
// (nothing overflowed one row below 17) which is what caught it — a skip
// is not a pass, and reading it as one would have shipped a confident
// wrong explanation next to working code.
//
// # Why the numbers are derived, not written down
//
// A constant here would be a second copy of a fact the markup already
// states, and the second copy is the one that goes stale — the argument
// that put the element vocabulary beside the code that reads it. Both
// minimums come from the live <Grid>'s own track definitions, so editing
// Rows= or Cols= moves them and they cannot drift.
//
// The only judgement is starMin.

// starMin is the smallest useful extent for a star track. A <Border>
// spends two cells per axis on its own frame, leaving one for content;
// below that the pane is chrome with nothing in it.
const starMin = 3

// fitSize is a terminal size the shell needs.
type fitSize struct{ Cols, Rows int }

func (f fitSize) String() string { return fmt.Sprintf("%d\u00d7%d", f.Cols, f.Rows) }

// minimumFor returns both minimums: hard, below which the shell is laid
// out off the screen, and usable, below which it fits but has no room
// left for the panes the star tracks hold.
//
// An AUTO track is a hard error rather than a guess. Its extent is
// whatever its content measured, which lives in the Grid's unexported
// measure cache — so this cannot compute it, and every way of guessing
// makes the minimum too SMALL, which means the check passes at a size
// that does not fit. That is the exact silent misfit this file removes,
// reintroduced by the fix. The shell has no Auto track today; the day one
// appears, this says so instead of quietly being wrong.
func minimumFor(g *components.Grid) (hard, usable fitSize, err error) {
	hc, uc, err := trackMinimum("Cols", g.Cols)
	if err != nil {
		return fitSize{}, fitSize{}, err
	}
	hr, ur, err := trackMinimum("Rows", g.Rows)
	if err != nil {
		return fitSize{}, fitSize{}, err
	}
	return fitSize{hc, hr}, fitSize{uc, ur}, nil
}

func trackMinimum(axis string, defs []components.GridLen) (hard, usable int, err error) {
	for i, d := range defs {
		switch {
		case d.Auto:
			return 0, 0, fmt.Errorf(
				"%s track %d is Auto: its extent is the measure cache, which this "+
					"cannot read, and every guess makes the minimum too small — "+
					"which is the silent misfit the check exists to catch", axis, i)
		case d.Star > 0:
			// A star track contributes nothing to the hard minimum: it is
			// what absorbs the shortfall, all the way down to zero.
			usable += starMin
		default:
			hard += d.Fixed
			usable += d.Fixed
		}
	}
	return hard, usable, nil
}

// watchFit swaps the shell for a legible message whenever the terminal is
// too small to lay it out, and back when it grows again.
//
// It runs from BeforeFrame — UI goroutine, before layout — because
// App.Size() is only updated by resized() on a SIGWINCH, so there is no
// property to observe and a per-frame read is the honest way to notice.
//
// Both Sets are GUARDED. prop.Set does not compare values, so setting
// them unconditionally every frame would invalidate every dependent on
// every frame and turn a cheap size check into a permanent full repaint.
func (ed *editor) watchFit(app *gooey.App) {
	app.BeforeFrame(func() {
		shell, err := markup.Find[*components.Grid](ed.ctx, "Shell")
		if err != nil {
			return // not built yet; the first frame settles it
		}
		_, usable, err := minimumFor(shell)
		if err != nil {
			// Say it once, in the place the user is already looking.
			if msg := "\u2717 " + err.Error(); msg != ed.status.Get() {
				ed.status.Set(msg)
			}
			return
		}
		cols, rows := app.Size()
		// The swap fires at the USABLE minimum, not the hard one. Between
		// them the layout is technically valid and shows two empty
		// borders, which is not a thing worth showing anybody.
		fits := cols >= usable.Cols && rows >= usable.Rows
		if fits != ed.fits.Get() {
			ed.fits.Set(fits)
		}
		if !fits {
			if want := cramMsg(fitSize{cols, rows}, usable); want != ed.fitMsg.Get() {
				ed.fitMsg.Set(want)
			}
		}
	})
}

// cramMsg is what the user sees instead of a broken layout. Both sizes,
// because "too small" without the numbers leaves them guessing how far to
// drag.
func cramMsg(have, want fitSize) string {
	return fmt.Sprintf(
		"This terminal is %s.\nThe editor needs %s.\n\nResize, or press q to quit.",
		have, want)
}
